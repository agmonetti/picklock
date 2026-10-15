package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"strings"

	"github.com/agmonetti/picklock/internal/store"
)

func (s *Store) ReadCell(ctx context.Context, req store.CellReadRequest) (*store.CellRead, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	columns, err := s.Columns(req.Row.ObjectName())
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, c := range columns {
		known[c.Name] = true
	}
	if !known[req.Column] {
		return nil, fmt.Errorf("unknown cell column %q", req.Column)
	}
	parts := req.Row.KeyParts()
	for _, p := range parts {
		if !known[p.Column] {
			return nil, fmt.Errorf("unknown identity column %q", p.Column)
		}
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, fmt.Errorf("begin cell read snapshot: %w", err)
	}
	where, args := postgresCellPredicate(parts, 1)
	table, col := QuoteIdent(req.Row.ObjectName()), QuoteIdent(req.Column)
	binary := false
	for _, c := range columns {
		if c.Name == req.Column {
			binary = strings.Contains(strings.ToLower(c.Type), "bytea")
		}
	}
	expr := fmt.Sprintf("convert_to(%s::text, 'UTF8')", col)
	if binary {
		expr = col
	}
	var size sql.NullInt64
	if err := tx.QueryRowContext(ctx, fmt.Sprintf("SELECT octet_length(%s) FROM %s WHERE %s", expr, table, where), args...).Scan(&size); err != nil {
		_ = tx.Rollback()
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("row no longer exists")
		}
		return nil, fmt.Errorf("inspect cell: %w", err)
	}
	if !size.Valid {
		_ = tx.Rollback()
		return &store.CellRead{IsNull: true}, nil
	}
	n := size.Int64
	blockWhere, _ := postgresCellPredicate(parts, 3)
	query := fmt.Sprintf("SELECT substring(%s FROM $1 FOR $2) FROM %s WHERE %s", expr, table, blockWhere)
	return &store.CellRead{Reader: &postgresCellReader{ctx: ctx, tx: tx, query: query, args: args, block: req.BlockBytes, size: n}, SizeBytes: &n, Binary: binary}, nil
}

func postgresCellPredicate(parts []store.KeyPart, first int) (string, []any) {
	clauses := make([]string, len(parts))
	args := make([]any, len(parts))
	for i, p := range parts {
		clauses[i] = fmt.Sprintf("%s = $%d", QuoteIdent(p.Column), first+i)
		args[i] = p.Value.Value
	}
	return strings.Join(clauses, " AND "), args
}

type postgresCellReader struct {
	ctx          context.Context
	tx           *sql.Tx
	query        string
	args         []any
	block        int
	size, offset int64
	closed       bool
}

func (r *postgresCellReader) Read(p []byte) (int, error) {
	if r.closed {
		return 0, io.ErrClosedPipe
	}
	if err := r.ctx.Err(); err != nil {
		_ = r.Close()
		return 0, err
	}
	if r.offset >= r.size {
		_ = r.Close()
		return 0, io.EOF
	}
	want := min(int64(r.block), r.size-r.offset)
	if int64(len(p)) < want {
		want = int64(len(p))
	}
	if want == 0 {
		return 0, nil
	}
	args := make([]any, 0, len(r.args)+2)
	args = append(args, r.offset+1, want)
	args = append(args, r.args...)
	var data []byte
	if err := r.tx.QueryRowContext(r.ctx, r.query, args...).Scan(&data); err != nil {
		_ = r.Close()
		return 0, err
	}
	if len(data) == 0 {
		_ = r.Close()
		return 0, fmt.Errorf("cell block ended before reported size")
	}
	n := copy(p, data)
	r.offset += int64(n)
	return n, nil
}
func (r *postgresCellReader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	return r.tx.Rollback()
}
