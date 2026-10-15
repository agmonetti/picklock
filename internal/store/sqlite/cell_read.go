package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"strings"

	"github.com/agmonetti/picklock/internal/store"
)

// ReadCell opens a SQLite read transaction and returns a byte-bounded reader.
// Each block is fetched with a separate SUBSTR query inside the same snapshot.
func (s *Store) ReadCell(ctx context.Context, req store.CellReadRequest) (*store.CellRead, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if req.Row.ObjectName() == "" {
		return nil, fmt.Errorf("cell read requires a table")
	}
	columns, err := s.Columns(req.Row.ObjectName())
	if err != nil {
		return nil, err
	}
	valid := false
	for _, column := range columns {
		if column.Name == req.Column {
			valid = true
			break
		}
	}
	if !valid {
		return nil, fmt.Errorf("unknown cell column %q", req.Column)
	}

	parts := req.Row.KeyParts()
	keyColumns := make(map[string]bool, len(parts))
	for _, part := range parts {
		keyColumns[part.Column] = true
	}
	for name := range keyColumns {
		found := false
		for _, column := range columns {
			if column.Name == name {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("unknown identity column %q", name)
		}
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin cell read snapshot: %w", err)
	}
	where, args := cellReadPredicate(parts)
	tableName := QuoteIdent(req.Row.ObjectName())
	columnName := QuoteIdent(req.Column)
	var sqliteType sql.NullString
	var size sql.NullInt64
	query := fmt.Sprintf("SELECT typeof(%s), length(CAST(%s AS BLOB)) FROM %s WHERE %s", columnName, columnName, tableName, where)
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&sqliteType, &size); err != nil {
		_ = tx.Rollback()
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("row no longer exists")
		}
		return nil, fmt.Errorf("inspect cell: %w", err)
	}
	if !sqliteType.Valid || sqliteType.String == "null" {
		_ = tx.Rollback()
		return &store.CellRead{IsNull: true}, nil
	}
	n := size.Int64
	return &store.CellRead{
		Reader:    &sqliteCellReader{ctx: ctx, tx: tx, query: fmt.Sprintf("SELECT substr(CAST(%s AS BLOB), ?, ?) FROM %s WHERE %s", columnName, tableName, where), args: args, blockBytes: req.BlockBytes, size: n},
		SizeBytes: &n,
		Binary:    strings.EqualFold(sqliteType.String, "blob"),
	}, nil
}

func cellReadPredicate(parts []store.KeyPart) (string, []any) {
	clauses := make([]string, len(parts))
	args := make([]any, len(parts))
	for i, part := range parts {
		clauses[i] = QuoteIdent(part.Column) + " = ?"
		args[i] = part.Value.Value
	}
	return strings.Join(clauses, " AND "), args
}

type sqliteCellReader struct {
	ctx        context.Context
	tx         *sql.Tx
	query      string
	args       []any
	blockBytes int
	size       int64
	offset     int64
	closed     bool
}

func (r *sqliteCellReader) Read(p []byte) (int, error) {
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
	want := min(int64(r.blockBytes), r.size-r.offset)
	if int64(len(p)) < want {
		want = int64(len(p))
	}
	if want == 0 {
		return 0, nil
	}
	args := make([]any, 0, len(r.args)+2)
	args = append(args, r.offset+1, want)
	args = append(args, r.args...)
	var block []byte
	if err := r.tx.QueryRowContext(r.ctx, r.query, args...).Scan(&block); err != nil {
		_ = r.Close()
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("row changed during cell read")
		}
		return 0, err
	}
	if len(block) == 0 {
		_ = r.Close()
		return 0, fmt.Errorf("cell block ended before the reported size")
	}
	n := copy(p, block)
	r.offset += int64(n)
	return n, nil
}

func (r *sqliteCellReader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	return r.tx.Rollback()
}
