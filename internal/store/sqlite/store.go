// Package sqlite implements the store.Store interface for SQLite.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"

	_ "modernc.org/sqlite" // pure-Go driver, no CGO required

	"github.com/agmonetti/picklock/internal/conn"
	"github.com/agmonetti/picklock/internal/store"
)

// Store is the SQLite implementation of store.Store.
type Store struct {
	db     *sql.DB
	driver conn.Driver
}

// New opens a SQLite database according to the config.
func New(cfg conn.ConnectionConfig) (*Store, error) {
	if cfg.Path == "" {
		return nil, fmt.Errorf("%w: missing file path", store.ErrConnection)
	}
	if _, err := os.Stat(cfg.Path); err != nil && !isMemory(cfg.Path) {
		return nil, fmt.Errorf("%w: %s: no such file", store.ErrConnection, cfg.Path)
	}
	dsn := "file:" + dsnPath(cfg.Path)
	if cfg.ReadOnly {
		dsn += "?mode=ro&_pragma=foreign_keys(1)"
	} else {
		dsn += "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", store.ErrConnection, err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("%w: %v", store.ErrConnection, err)
	}
	return &Store{db: db, driver: conn.DriverSQLite}, nil
}

func init() {
	store.RegisterLegacy(conn.DriverSQLite, func(cfg conn.ConnectionConfig) (store.Store, error) {
		return New(cfg)
	})
}

// isMemory reports whether the path is an in-memory SQLite database.
func isMemory(path string) bool {
	return path == ":memory:" || path == "file::memory:"
}

// dsnPath percent-encodes the characters that the DSN parser would read as
// part of the query or a URI escape (?, #, %), plus spaces, so a file named
// "a?b.db" or "a b.db" connects correctly. Memory paths need no escaping.
func dsnPath(path string) string {
	if isMemory(path) {
		return path
	}
	var b strings.Builder
	for i := 0; i < len(path); i++ {
		c := path[i]
		switch c {
		case '?', '#', '%', ' ':
			b.WriteString(url.PathEscape(string(c)))
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func (s *Store) Driver() string { return string(s.driver) }

// Version returns the SQLite version.
func (s *Store) Version() (string, error) {
	var v string
	if err := s.db.QueryRow("SELECT sqlite_version()").Scan(&v); err != nil {
		return "", fmt.Errorf("store.Version: %w", err)
	}
	return v, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Tables returns the tables of the database, sorted alphabetically.
func (s *Store) Tables() ([]string, error) {
	rows, err := s.db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("store.Tables: %w", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("store.Tables: %w", err)
		}
		tables = append(tables, name)
	}
	return tables, rows.Err()
}

// Columns returns the columns of a table.
func (s *Store) Columns(table string) ([]store.Column, error) {
	rows, err := s.db.Query("PRAGMA table_info(" + QuoteIdent(table) + ")")
	if err != nil {
		return nil, fmt.Errorf("store.Columns(%s): %w", table, err)
	}
	defer rows.Close()

	var cols []store.Column
	for rows.Next() {
		var (
			cid     int
			name    string
			typ     string
			notNull int
			def     sql.NullString
			pk      int
		)
		// PRAGMA table_info returns: cid, name, type, notnull, dflt_value, pk
		if err := rows.Scan(&cid, &name, &typ, &notNull, &def, &pk); err != nil {
			return nil, fmt.Errorf("store.Columns(%s): %w", table, err)
		}
		cols = append(cols, store.Column{
			Name:    name,
			Type:    typ,
			NotNull: notNull == 1,
			Default: def.String,
			PK:      pk > 0,
		})
	}
	return cols, rows.Err()
}

// Indexes returns the indexes of a table.
func (s *Store) Indexes(table string) ([]store.Index, error) {
	rows, err := s.db.Query("PRAGMA index_list(" + QuoteIdent(table) + ")")
	if err != nil {
		return nil, fmt.Errorf("store.Indexes(%s): %w", table, err)
	}
	defer rows.Close()

	type idxMeta struct {
		name   string
		unique int
	}
	var metas []idxMeta
	for rows.Next() {
		var (
			seq     int
			name    string
			unique  int
			origin  string
			partial int
		)
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			return nil, fmt.Errorf("store.Indexes(%s): %w", table, err)
		}
		metas = append(metas, idxMeta{name: name, unique: unique})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var indexes []store.Index
	for _, m := range metas {
		cols, err := s.indexColumns(m.name)
		if err != nil {
			return nil, err
		}
		indexes = append(indexes, store.Index{
			Name:    m.name,
			Columns: cols,
			Unique:  m.unique == 1,
		})
	}
	return indexes, nil
}

func (s *Store) indexColumns(index string) ([]string, error) {
	rows, err := s.db.Query("PRAGMA index_info(" + QuoteIdent(index) + ")")
	if err != nil {
		return nil, fmt.Errorf("store.indexColumns(%s): %w", index, err)
	}
	defer rows.Close()

	var cols []string
	for rows.Next() {
		var (
			seqno int
			cid   int
			name  string
		)
		if err := rows.Scan(&seqno, &cid, &name); err != nil {
			return nil, fmt.Errorf("store.indexColumns(%s): %w", index, err)
		}
		cols = append(cols, name)
	}
	return cols, rows.Err()
}

// Query runs arbitrary SQL and returns columns and rows.
func (s *Store) Query(sql string) (*store.Result, error) {
	return s.QueryContext(context.Background(), sql)
}

// QueryContext runs arbitrary SQL with a context, so it can be cancelled or
// bounded by a timeout.
func (s *Store) QueryContext(ctx context.Context, sql string) (*store.Result, error) {
	rows, err := s.db.QueryContext(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.ScanResult(rows)
}

// QueryContextMax runs a query that returns rows, stopping after max rows
// (0 = unlimited) and marking Result.Truncated when the result is longer.
func (s *Store) QueryContextMax(ctx context.Context, sql string, max int) (*store.Result, error) {
	rows, err := s.db.QueryContext(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.ScanResultMax(rows, max)
}

// Exec runs SQL without a result (INSERT/UPDATE/DELETE) and returns affected rows.
func (s *Store) Exec(sql string) (int64, error) {
	return s.ExecContext(context.Background(), sql)
}

// ExecContext runs SQL without a result with a context.
func (s *Store) ExecContext(ctx context.Context, sql string) (int64, error) {
	res, err := s.db.ExecContext(ctx, sql)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return n, nil
}

// CountTable returns the number of rows in a table.
func (s *Store) CountTable(table string) (int, error) {
	return s.CountTableContext(context.Background(), table)
}

// CountTableContext returns the number of rows in a table, bounded by ctx.
func (s *Store) CountTableContext(ctx context.Context, table string) (int, error) {
	q := fmt.Sprintf("SELECT COUNT(*) FROM %s", QuoteIdent(table))
	var n int
	if err := s.db.QueryRowContext(ctx, q).Scan(&n); err != nil {
		return 0, fmt.Errorf("store.CountTable(%s): %w", table, err)
	}
	return n, nil
}

// SelectTablePage returns a page of rows from a table.
func (s *Store) SelectTablePage(table string, limit, offset int) (*store.Result, error) {
	return s.SelectTablePageContext(context.Background(), table, limit, offset)
}

// SelectTablePageContext returns a page of rows from a table, bounded by ctx.
func (s *Store) SelectTablePageContext(ctx context.Context, table string, limit, offset int) (*store.Result, error) {
	q := fmt.Sprintf("SELECT * FROM %s %s", QuoteIdent(table), Limit(limit, offset))
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.ScanResult(rows)
}

// SelectTableKeysetPage returns the page after cursor ordered by the key.
func (s *Store) SelectTableKeysetPage(table, key string, limit int, cursor string) (*store.Result, error) {
	return s.SelectTableKeysetPageContext(context.Background(), table, key, limit, cursor)
}

// SelectTableKeysetPageContext returns the page after cursor ordered by the
// key, bounded by ctx.
func (s *Store) SelectTableKeysetPageContext(ctx context.Context, table, key string, limit int, cursor string) (*store.Result, error) {
	q := fmt.Sprintf("SELECT * FROM %s", QuoteIdent(table))
	if cursor != "" {
		q += fmt.Sprintf(" WHERE %s > ?", QuoteIdent(key))
	}
	q += fmt.Sprintf(" ORDER BY %s LIMIT %d", QuoteIdent(key), limit)
	rows, err := s.db.QueryContext(ctx, q, queryArgs(cursor)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.ScanResult(rows)
}

// queryArgs returns nil for an empty cursor (first page) and a single-value
// slice otherwise, so the WHERE clause placeholder always matches the args.
func queryArgs(cursor string) []any {
	if cursor == "" {
		return nil
	}
	return []any{cursor}
}
