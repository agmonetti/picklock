package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"time"

	_ "github.com/microsoft/go-mssqldb" // registers the "sqlserver" driver

	"github.com/agmonetti/picklock/internal/conn"
	"github.com/agmonetti/picklock/internal/store"
)

// Store is the SQL Server implementation of store.Store.
type Store struct {
	db       *sql.DB
	readOnly bool
}

// New opens a connection to SQL Server.
func New(cfg conn.ConnectionConfig) (*Store, error) {
	if cfg.Host == "" {
		return nil, fmt.Errorf("%w: missing host", store.ErrConnection)
	}
	u := url.URL{
		Scheme: "sqlserver",
		User:   url.UserPassword(cfg.User, cfg.Password),
		Host:   fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
	}
	q := u.Query()
	if cfg.Database != "" {
		q.Set("database", cfg.Database)
	}
	q.Set("connection+timeout", "5")
	// TLS: "require" mandates encryption with certificate verification,
	// "disable" sends nothing. "prefer" (and empty) keep the driver default,
	// which encrypts when the server supports it without demanding a trusted
	// certificate — so local/dev servers with self-signed certs still connect.
	switch cfg.SSLMode {
	case "require":
		q.Set("encrypt", "mandatory")
	case "disable":
		q.Set("encrypt", "disable")
	}
	u.RawQuery = q.Encode()

	db, err := sql.Open("sqlserver", u.String())
	if err != nil {
		return nil, fmt.Errorf("%w: %v", store.ErrConnection, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("%w: %v", store.ErrConnection, err)
	}
	return &Store{db: db, readOnly: cfg.ReadOnly}, nil
}

func init() {
	store.RegisterLegacy(conn.DriverMSSQL, func(cfg conn.ConnectionConfig) (store.Store, error) {
		return New(cfg)
	})
}

func (s *Store) Driver() string { return "mssql" }

// Version returns the server version.
func (s *Store) Version() (string, error) {
	var v string
	if err := s.db.QueryRow("SELECT @@VERSION").Scan(&v); err != nil {
		return "", fmt.Errorf("store.Version: %w", err)
	}
	return v, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Tables returns the tables of the user's default schema in the current database.
func (s *Store) Tables() ([]string, error) {
	rows, err := s.db.Query(`
		SELECT TABLE_NAME FROM INFORMATION_SCHEMA.TABLES
		WHERE TABLE_TYPE = 'BASE TABLE' AND TABLE_SCHEMA = SCHEMA_NAME()
		ORDER BY TABLE_NAME`)
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
	rows, err := s.db.Query(`
		SELECT c.COLUMN_NAME, c.DATA_TYPE,
		       CASE WHEN c.IS_NULLABLE = 'NO' THEN 1 ELSE 0 END,
		       ISNULL(c.COLUMN_DEFAULT, ''),
		       CASE WHEN pk.COLUMN_NAME IS NOT NULL THEN 1 ELSE 0 END
		FROM INFORMATION_SCHEMA.COLUMNS c
		LEFT JOIN (
			SELECT ku.TABLE_NAME, ku.COLUMN_NAME
			FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS tc
			JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE ku
			  ON tc.CONSTRAINT_NAME = ku.CONSTRAINT_NAME
			WHERE tc.CONSTRAINT_TYPE = 'PRIMARY KEY'
			  AND tc.CONSTRAINT_SCHEMA = SCHEMA_NAME()
		) pk ON c.TABLE_NAME = pk.TABLE_NAME AND c.COLUMN_NAME = pk.COLUMN_NAME
		WHERE c.TABLE_SCHEMA = SCHEMA_NAME() AND c.TABLE_NAME = @p1
		ORDER BY c.ORDINAL_POSITION`, table)
	if err != nil {
		return nil, fmt.Errorf("store.Columns(%s): %w", table, err)
	}
	defer rows.Close()

	var cols []store.Column
	for rows.Next() {
		var (
			c             store.Column
			notNull, isPK bool
		)
		if err := rows.Scan(&c.Name, &c.Type, &notNull, &c.Default, &isPK); err != nil {
			return nil, fmt.Errorf("store.Columns(%s): %w", table, err)
		}
		c.NotNull = notNull
		c.PK = isPK
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

// Indexes returns the indexes of a table.
func (s *Store) Indexes(table string) ([]store.Index, error) {
	rows, err := s.db.Query(`
		SELECT i.name, col.name, i.is_unique
		FROM sys.indexes i
		JOIN sys.index_columns ic
		  ON i.object_id = ic.object_id AND i.index_id = ic.index_id
		JOIN sys.columns col
		  ON ic.object_id = col.object_id AND ic.column_id = col.column_id
		JOIN sys.tables t ON i.object_id = t.object_id
		JOIN sys.schemas sch ON t.schema_id = sch.schema_id
		WHERE sch.name = SCHEMA_NAME() AND t.name = @p1 AND i.name IS NOT NULL
		ORDER BY i.name, ic.key_ordinal`, table)
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

// ForeignKeysContext returns all foreign-key constraints in the current schema.
func (s *Store) ForeignKeysContext(ctx context.Context) ([]store.ForeignKey, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT fk.name, sch_parent.name, t_parent.name, c_parent.name,
		       fkc.constraint_column_id, sch_ref.name, t_ref.name,
		       c_ref.name, fk.update_referential_action_desc,
		       fk.delete_referential_action_desc
		FROM sys.foreign_keys fk
		JOIN sys.foreign_key_columns fkc
		  ON fkc.constraint_object_id = fk.object_id
		JOIN sys.tables t_parent ON t_parent.object_id = fk.parent_object_id
		JOIN sys.schemas sch_parent ON sch_parent.schema_id = t_parent.schema_id
		JOIN sys.columns c_parent
		  ON c_parent.object_id = t_parent.object_id
		 AND c_parent.column_id = fkc.parent_column_id
		JOIN sys.tables t_ref ON t_ref.object_id = fk.referenced_object_id
		JOIN sys.schemas sch_ref ON sch_ref.schema_id = t_ref.schema_id
		JOIN sys.columns c_ref
		  ON c_ref.object_id = t_ref.object_id
		 AND c_ref.column_id = fkc.referenced_column_id
		WHERE sch_parent.name = SCHEMA_NAME()
		ORDER BY t_parent.name, fk.name, fkc.constraint_column_id`)
	if err != nil {
		return nil, fmt.Errorf("store.ForeignKeys: %w", err)
	}
	defer rows.Close()

	var out []store.ForeignKey
	byName := make(map[string]*store.ForeignKey)
	var order []string
	for rows.Next() {
		var name, schema, table, column string
		var ordinal int
		var refSchema, refTable, refCol, updateRule, deleteRule string
		if err := rows.Scan(&name, &schema, &table, &column, &ordinal, &refSchema, &refTable, &refCol, &updateRule, &deleteRule); err != nil {
			return nil, fmt.Errorf("store.ForeignKeys: %w", err)
		}
		key := schema + "\x00" + table + "\x00" + name
		fk, ok := byName[key]
		if !ok {
			fk = &store.ForeignKey{
				Name: name, TableSchema: schema, Table: table,
				ReferencedSchema: refSchema, ReferencedTable: refTable,
				UpdateRule: updateRule, DeleteRule: deleteRule,
			}
			byName[key] = fk
			order = append(order, key)
		}
		fk.Columns = append(fk.Columns, column)
		fk.ReferencedColumns = append(fk.ReferencedColumns, refCol)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.ForeignKeys: %w", err)
	}
	for _, key := range order {
		out = append(out, *byName[key])
	}
	return out, nil
}

// Query runs arbitrary SQL.
func (s *Store) Query(sql string) (*store.Result, error) {
	return s.QueryContext(context.Background(), sql)
}

// QueryContext runs arbitrary SQL with a context, so it can be cancelled or
// bounded by a timeout.
func (s *Store) QueryContext(ctx context.Context, sql string) (*store.Result, error) {
	if s.readOnly && store.IsSQLWrite(sql) {
		return nil, fmt.Errorf("mutations are blocked in read-only mode")
	}
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
	if s.readOnly && store.IsSQLWrite(sql) {
		return nil, fmt.Errorf("mutations are blocked in read-only mode")
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

// ExecContext runs SQL without a result with a context.
func (s *Store) ExecContext(ctx context.Context, sql string) (int64, error) {
	if s.readOnly {
		return 0, fmt.Errorf("mutations are blocked in read-only mode")
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
		q += fmt.Sprintf(" WHERE %s > @p1", QuoteIdent(key))
	}
	q += fmt.Sprintf(" ORDER BY %s OFFSET 0 ROWS FETCH NEXT %d ROWS ONLY", QuoteIdent(key), limit)
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
