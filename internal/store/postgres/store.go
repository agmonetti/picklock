package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver

	"github.com/agmonetti/picklock/internal/conn"
	"github.com/agmonetti/picklock/internal/store"
)

// Store is the PostgreSQL implementation of store.Store.
type Store struct {
	db       *sql.DB
	keyTypes sync.Map // table+"\x00"+column -> information_schema data_type
}

// New opens a connection to PostgreSQL.
func New(cfg conn.ConnectionConfig) (*Store, error) {
	if cfg.Host == "" {
		return nil, fmt.Errorf("%w: missing host", store.ErrConnection)
	}
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(cfg.User, cfg.Password),
		Host:   fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Path:   "/" + url.PathEscape(cfg.Database),
	}
	q := u.Query()
	sslmode := cfg.SSLMode
	if sslmode == "" {
		sslmode = "prefer"
	}
	q.Set("sslmode", sslmode)
	if cfg.ReadOnly {
		// every transaction in the session starts read-only, so any write
		// fails at the server ("cannot execute ... in a read-only transaction")
		q.Set("options", "-cdefault_transaction_read_only=on")
	}
	u.RawQuery = q.Encode()

	db, err := sql.Open("pgx", u.String())
	if err != nil {
		return nil, fmt.Errorf("%w: %v", store.ErrConnection, err)
	}
	db.SetConnMaxLifetime(5 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("%w: %v", store.ErrConnection, err)
	}
	return &Store{db: db}, nil
}

func init() {
	store.RegisterLegacy(conn.DriverPostgres, func(cfg conn.ConnectionConfig) (store.Store, error) {
		return New(cfg)
	})
}

func (s *Store) Driver() string { return "postgres" }

// Version returns the server version.
func (s *Store) Version() (string, error) {
	var v string
	if err := s.db.QueryRow("SELECT version()").Scan(&v); err != nil {
		return "", fmt.Errorf("store.Version: %w", err)
	}
	return v, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Tables returns the tables of the current schema (search_path).
func (s *Store) Tables() ([]string, error) {
	rows, err := s.db.Query(`
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = current_schema() AND table_type = 'BASE TABLE'
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

// Columns returns the columns of a table in the current schema.
func (s *Store) Columns(table string) ([]store.Column, error) {
	rows, err := s.db.Query(`
		SELECT c.column_name, c.data_type,
		       (c.is_nullable = 'NO'),
		       COALESCE(c.column_default, ''),
		       CASE WHEN pk.column_name IS NOT NULL THEN true ELSE false END
		FROM information_schema.columns c
		LEFT JOIN (
			SELECT ku.table_name, ku.column_name
			FROM information_schema.table_constraints tc
			JOIN information_schema.key_column_usage ku
			  ON tc.constraint_name = ku.constraint_name
			 AND tc.table_schema = ku.table_schema
			WHERE tc.constraint_type = 'PRIMARY KEY' AND tc.table_schema = current_schema()
		) pk ON c.table_name = pk.table_name AND c.column_name = pk.column_name
		WHERE c.table_schema = current_schema() AND c.table_name = $1
		ORDER BY c.ordinal_position`, table)
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

// Indexes returns the indexes of a table in the current schema.
func (s *Store) Indexes(table string) ([]store.Index, error) {
	rows, err := s.db.Query(`
		SELECT i.relname, a.attname, ix.indisunique
		FROM pg_class t
		JOIN pg_index ix ON t.oid = ix.indrelid
		JOIN pg_class i ON i.oid = ix.indexrelid
		JOIN pg_namespace n ON t.relnamespace = n.oid
		JOIN unnest(ix.indkey) WITH ORDINALITY AS k(attnum, ord) ON true
		JOIN pg_attribute a ON t.oid = a.attrelid AND a.attnum = k.attnum
		WHERE n.nspname = current_schema() AND t.relname = $1
		ORDER BY i.relname, k.ord`, table)
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
		SELECT kcu.constraint_name, kcu.constraint_schema, kcu.table_name,
		       kcu.column_name, kcu.ordinal_position,
		       kcu.referenced_table_schema, kcu.referenced_table_name,
		       kcu.referenced_column_name,
		       rc.update_rule, rc.delete_rule
		FROM information_schema.key_column_usage kcu
		JOIN information_schema.referential_constraints rc
		  ON rc.constraint_schema = kcu.constraint_schema
		 AND rc.constraint_name = kcu.constraint_name
		 AND rc.table_name = kcu.table_name
		WHERE kcu.constraint_schema = current_schema()
		  AND kcu.referenced_table_name IS NOT NULL
		ORDER BY kcu.table_name, kcu.constraint_name, kcu.ordinal_position`)
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

// Exec runs SQL without a result.
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

// SelectTableKeysetPage returns the page after cursor ordered by the key. The
// cursor is compared with an explicit cast ($1::<type>) so the primary key
// index is still usable regardless of the column type.
func (s *Store) SelectTableKeysetPage(table, key string, limit int, cursor string) (*store.Result, error) {
	return s.SelectTableKeysetPageContext(context.Background(), table, key, limit, cursor)
}

// SelectTableKeysetPageContext returns the page after cursor ordered by the
// key, bounded by ctx.
func (s *Store) SelectTableKeysetPageContext(ctx context.Context, table, key string, limit int, cursor string) (*store.Result, error) {
	typ, err := s.keyType(table, key)
	if err != nil {
		return nil, err
	}
	q := fmt.Sprintf("SELECT * FROM %s", QuoteIdent(table))
	if cursor != "" {
		q += fmt.Sprintf(" WHERE %s > $1::%s", QuoteIdent(key), typ)
	}
	q += fmt.Sprintf(" ORDER BY %s LIMIT %d", QuoteIdent(key), limit)
	rows, err := s.db.QueryContext(ctx, q, queryArgs(cursor)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.ScanResult(rows)
}

// keyType returns the PostgreSQL data type of a column formatted for casting
// (e.g. "integer", "uuid", "text", "public.my_enum", "integer[]"). The result
// is cached per table+column.
func (s *Store) keyType(table, key string) (string, error) {
	cacheKey := table + "\x00" + key
	if v, ok := s.keyTypes.Load(cacheKey); ok {
		return v.(string), nil
	}
	var dt string
	if err := s.db.QueryRow(`
		SELECT format_type(a.atttypid, a.atttypmod)
		FROM pg_attribute a
		JOIN pg_class t ON a.attrelid = t.oid
		JOIN pg_namespace n ON t.relnamespace = n.oid
		WHERE n.nspname = current_schema() AND t.relname = $1 AND a.attname = $2`,
		table, key).Scan(&dt); err != nil {
		return "", fmt.Errorf("store.keyType(%s.%s): %w", table, key, err)
	}
	s.keyTypes.Store(cacheKey, dt)
	return dt, nil
}

// queryArgs returns nil for an empty cursor (first page) and a single-value
// slice otherwise, so the WHERE clause placeholder always matches the args.
func queryArgs(cursor string) []any {
	if cursor == "" {
		return nil
	}
	return []any{cursor}
}
