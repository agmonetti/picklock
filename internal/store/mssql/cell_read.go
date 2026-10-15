package mssql

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
	var typ string
	for _, c := range columns {
		known[c.Name] = true
		if c.Name == req.Column {
			typ = strings.ToLower(c.Type)
		}
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
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSnapshot})
	if err != nil {
		return nil, fmt.Errorf("begin SQL Server SNAPSHOT cell read (database SNAPSHOT isolation must be enabled): %w", err)
	}
	where, args := mssqlCellPredicate(parts, 1)
	col, table := QuoteIdent(req.Column), QuoteIdent(req.Row.ObjectName())
	binary := strings.Contains(typ, "binary") || strings.Contains(typ, "image") || strings.Contains(typ, "timestamp")
	expr := "CONVERT(varbinary(max), " + col + ")"
	if !binary {
		expr = "CONVERT(varbinary(max), CONVERT(varchar(max), CONVERT(nvarchar(max), " + col + ") COLLATE Latin1_General_100_BIN2_UTF8))"
	}
	var size sql.NullInt64
	if err := tx.QueryRowContext(ctx, fmt.Sprintf("SELECT DATALENGTH(%s) FROM %s WHERE %s", expr, table, where), args...).Scan(&size); err != nil {
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
	blockWhere, _ := mssqlCellPredicate(parts, 3)
	query := fmt.Sprintf("SELECT SUBSTRING(%s, @p1, @p2) FROM %s WHERE %s", expr, table, blockWhere)
	return &store.CellRead{Reader: &mssqlCellReader{ctx: ctx, tx: tx, query: query, args: args, block: req.BlockBytes, size: n}, SizeBytes: &n, Binary: binary}, nil
}
func mssqlCellPredicate(parts []store.KeyPart, first int) (string, []any) {
	clauses := make([]string, len(parts))
	args := make([]any, len(parts))
	for i, p := range parts {
		clauses[i] = fmt.Sprintf("%s = @p%d", QuoteIdent(p.Column), first+i)
		args[i] = p.Value.Value
	}
	return strings.Join(clauses, " AND "), args
}

type mssqlCellReader struct {
	ctx          context.Context
	tx           *sql.Tx
	query        string
	args         []any
	block        int
	size, offset int64
	closed       bool
}

func (r *mssqlCellReader) Read(p []byte) (int, error) {
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
func (r *mssqlCellReader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	return r.tx.Rollback()
}
