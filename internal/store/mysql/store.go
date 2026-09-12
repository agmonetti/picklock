package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"

	"github.com/agmonetti/picklock/internal/conn"
	"github.com/agmonetti/picklock/internal/store"
)

// Store is the MySQL/MariaDB implementation of store.Store.
type Store struct {
	db       *sql.DB
	driver   conn.Driver
	readOnly bool
}

// NewMySQL opens a MySQL connection.
func NewMySQL(cfg conn.ConnectionConfig) (*Store, error) {
	return newStore(cfg, conn.DriverMySQL)
}

// NewMariaDB opens a MariaDB connection.
func NewMariaDB(cfg conn.ConnectionConfig) (*Store, error) {
	return newStore(cfg, conn.DriverMariaDB)
}

func newStore(cfg conn.ConnectionConfig, drv conn.Driver) (*Store, error) {
	if cfg.Host == "" {
		return nil, fmt.Errorf("%w: missing host", store.ErrConnection)
	}
	mc := mysqlDriver.NewConfig()
	mc.User = cfg.User
	mc.Passwd = cfg.Password
	mc.Net = "tcp"
	mc.Addr = fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	mc.DBName = cfg.Database
	mc.ParseTime = true
	mc.Timeout = 5 * time.Second
	// TLS: "prefer" negotiates when the server supports it, "require" demands
	// it with hostname+CA verification (driver "true"), "disable" forbids it.
	// An empty value keeps the driver default (no TLS unless the server asks).
	switch cfg.SSLMode {
	case "prefer":
		mc.TLSConfig = "preferred"
	case "require":
		mc.TLSConfig = "true"
	case "disable":
		mc.TLSConfig = "false"
	}

	db, err := sql.Open("mysql", mc.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("%w: %v", store.ErrConnection, err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("%w: %v", store.ErrConnection, err)
	}
	return &Store{db: db, driver: drv, readOnly: cfg.ReadOnly}, nil
}

func init() {
	store.RegisterLegacy(conn.DriverMySQL, func(cfg conn.ConnectionConfig) (store.Store, error) {
		return NewMySQL(cfg)
	})
	store.RegisterLegacy(conn.DriverMariaDB, func(cfg conn.ConnectionConfig) (store.Store, error) {
		return NewMariaDB(cfg)
	})
}

func (s *Store) Driver() string { return string(s.driver) }

// Version returns the server version.
func (s *Store) Version() (string, error) {
	var v string
	if err := s.db.QueryRow("SELECT VERSION()").Scan(&v); err != nil {
		return "", fmt.Errorf("store.Version: %w", err)
	}
	return v, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Tables returns the tables of the current database.
func (s *Store) Tables() ([]string, error) {
	rows, err := s.db.Query(`
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = DATABASE() AND table_type = 'BASE TABLE'
		ORDER BY table_name`)
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

// Columns returns the columns of a table in the current database.
func (s *Store) Columns(table string) ([]store.Column, error) {
	rows, err := s.db.Query(`
		SELECT column_name, column_type, (is_nullable = 'NO'),
		       COALESCE(column_default, ''), (column_key = 'PRI')
		FROM information_schema.columns
		WHERE table_schema = DATABASE() AND table_name = ?
		ORDER BY ordinal_position`, table)
	if err != nil {
		return nil, fmt.Errorf("store.Columns(%s): %w", table, err)
	}
	defer rows.Close()

	var cols []store.Column
	for rows.Next() {
		var c store.Column
		if err := rows.Scan(&c.Name, &c.Type, &c.NotNull, &c.Default, &c.PK); err != nil {
			return nil, fmt.Errorf("store.Columns(%s): %w", table, err)
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

// Indexes returns the indexes of a table in the current database.
func (s *Store) Indexes(table string) ([]store.Index, error) {
	rows, err := s.db.Query(`
		SELECT index_name, column_name, (non_unique = 0)
		FROM information_schema.statistics
		WHERE table_schema = DATABASE() AND table_name = ?
		ORDER BY index_name, seq_in_index`, table)
	if err != nil {
		return nil, fmt.Errorf("store.Indexes(%s): %w", table, err)
	}
	defer rows.Close()

	var indexes []store.Index
	byName := map[string]*store.Index{}
	var order []string
	for rows.Next() {
		var name, col string
		var unique bool
		if err := rows.Scan(&name, &col, &unique); err != nil {
			return nil, fmt.Errorf("store.Indexes(%s): %w", table, err)
		}
		ix, ok := byName[name]
		if !ok {
			ix = &store.Index{Name: name, Unique: unique}
			byName[name] = ix
			order = append(order, name)
		}
		ix.Columns = append(ix.Columns, col)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, name := range order {
		indexes = append(indexes, *byName[name])
	}
	return indexes, nil
}

// Query runs arbitrary SQL.
func (s *Store) Query(sql string) (*store.Result, error) {
	return s.QueryContext(context.Background(), sql)
}

// QueryContext runs arbitrary SQL with a context, so it can be cancelled or
// bounded by a timeout.
func (s *Store) QueryContext(ctx context.Context, sql string) (*store.Result, error) {
	return s.QueryContextMax(ctx, sql, 0)
}

// QueryContextMax runs a query that returns rows, stopping after max rows
// (0 = unlimited) and marking Result.Truncated when the result is longer. In
// read-only mode it pins a connection and enforces READ ONLY so stored procedures
// or expressions cannot write.
func (s *Store) QueryContextMax(ctx context.Context, sql string, max int) (*store.Result, error) {
	if s.readOnly {
		conn, err := s.db.Conn(ctx)
		if err != nil {
			return nil, err
		}
		defer conn.Close()
		if _, err := conn.ExecContext(ctx, "SET SESSION TRANSACTION READ ONLY"); err != nil {
			return nil, err
		}
		rows, err := conn.QueryContext(ctx, sql)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return store.ScanResultMax(rows, max)
	}
	rows, err := s.db.QueryContext(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.ScanResultMax(rows, max)
}

// Exec runs SQL without a result.
func (s *Store) Exec(sql string) (int64, error) {
	return s.ExecContext(context.Background(), sql)
}

// ExecContext runs SQL without a result with a context. In read-only mode the
// statement runs on a pinned connection after SET SESSION TRANSACTION READ
// ONLY, so a write fails at the server instead of silently succeeding. The
// driver has no DSN parameter for this, and the pool would otherwise recycle
// connections with the setting lost.
func (s *Store) ExecContext(ctx context.Context, sql string) (int64, error) {
	if s.readOnly {
		conn, err := s.db.Conn(ctx)
		if err != nil {
			return 0, err
		}
		defer conn.Close()
		if _, err := conn.ExecContext(ctx, "SET SESSION TRANSACTION READ ONLY"); err != nil {
			return 0, err
		}
		res, err := conn.ExecContext(ctx, sql)
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	}
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
