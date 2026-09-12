// Package demo generates the example schema and data that picklock shows when you
// first open it (users, orders, products, payments, logs, ...). It seeds any of
// the five engines from the same deterministic dataset, so every engine can be
// tried with a real amount of data: pagination, keyset browsing, auto-refresh
// and the SQL editor all work against it.
package demo

import (
	"database/sql"
	"fmt"
	"math/rand"
	"net/url"
	"strings"
	"time"
)

// Config holds the connection settings for a demo target.
type Config struct {
	// SQLite
	Path string
	// Network engines
	Host     string
	Port     int
	User     string
	Password string
	Database string
	// PostgreSQL TLS mode (prefer/require/disable/...); empty = disable for the
	// local demo servers.
	SSLMode string
}

// DriverName maps a picklock engine to the database/sql driver name to open.
func DriverName(driver string) string {
	switch driver {
	case "postgres":
		return "pgx"
	case "mssql":
		return "sqlserver"
	case "mysql", "mariadb":
		// go-sql-driver/mysql registers a single "mysql" driver.
		return "mysql"
	default:
		// sqlite (modernc)
		return driver
	}
}

// DSN builds the connection string for a driver and config.
func DSN(driver string, cfg Config) string {
	switch driver {
	case "sqlite":
		return cfg.Path
	case "postgres":
		u := url.URL{
			Scheme: "postgres",
			User:   url.UserPassword(cfg.User, cfg.Password),
			Host:   fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
			Path:   "/" + url.PathEscape(cfg.Database),
		}
		q := u.Query()
		mode := cfg.SSLMode
		if mode == "" {
			mode = "disable"
		}
		q.Set("sslmode", mode)
		u.RawQuery = q.Encode()
		return u.String()
	case "mysql", "mariadb":
		return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database)
	case "mssql":
		u := url.URL{
			Scheme: "sqlserver",
			User:   url.UserPassword(cfg.User, cfg.Password),
			Host:   fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		}
		q := u.Query()
		q.Set("database", cfg.Database)
		u.RawQuery = q.Encode()
		return u.String()
	}
	return ""
}

// Schema returns the CREATE TABLE statements for a driver.
func Schema(driver string) []string {
	out := make([]string, 0, len(schema))
	for _, t := range schema {
		out = append(out, createTable(driver, t))
	}
	return out
}

// Seed rebuilds the demo schema and data in db. It drops any existing demo
// tables first, so running it again yields the same clean dataset (the data is
// deterministic: same random seed, same rows).
func Seed(db *sql.DB, driver string) error {
	for _, t := range schema {
		if _, err := db.Exec("DROP TABLE IF EXISTS " + q(driver, t.name)); err != nil {
			return fmt.Errorf("drop %s: %w", t.name, err)
		}
	}
	for _, q2 := range Schema(driver) {
		if _, err := db.Exec(q2); err != nil {
			return fmt.Errorf("%s: %w", firstLine(q2), err)
		}
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			tx.Rollback()
		}
	}()

	s := seeder{
		tx:     tx,
		driver: driver,
		rng:    rand.New(rand.NewSource(42)),
		stmts:  make(map[string]*sql.Stmt),
	}
	defer s.closeStmts()
	if err := s.data(); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	ok = true
	return nil
}

// ---------------------------------------------------------------- schema ---

// colSpec is a portable column definition written in SQLite types; createTable
// maps it to each engine.
type colSpec struct {
	name string
	typ  string // SQLite type string: "INTEGER PRIMARY KEY", "TEXT NOT NULL", ...
}

type tableSpec struct {
	name string
	cols []colSpec
}

var schema = []tableSpec{
	{`users`, []colSpec{
		{"id", "INTEGER PRIMARY KEY"}, {"name", "TEXT NOT NULL"}, {"email", "TEXT UNIQUE"},
		{"password_hash", "TEXT"}, {"role", "TEXT"}, {"active", "INTEGER NOT NULL DEFAULT 1"},
		{"created_at", "TEXT"}, {"updated_at", "TEXT"},
	}},
	{`customers`, []colSpec{
		{"id", "INTEGER PRIMARY KEY"}, {"first_name", "TEXT"}, {"last_name", "TEXT"}, {"email", "TEXT"},
		{"phone", "TEXT"}, {"country", "TEXT"}, {"city", "TEXT"}, {"vip", "INTEGER NOT NULL DEFAULT 0"},
		{"loyalty_points", "INTEGER NOT NULL DEFAULT 0"}, {"created_at", "TEXT"},
	}},
	{`addresses`, []colSpec{
		{"id", "INTEGER PRIMARY KEY"}, {"customer_id", "INTEGER"}, {"street", "TEXT"}, {"city", "TEXT"},
		{"state", "TEXT"}, {"zip", "TEXT"}, {"country", "TEXT"}, {"is_primary", "INTEGER NOT NULL DEFAULT 0"},
	}},
	{`categories`, []colSpec{
		{"id", "INTEGER PRIMARY KEY"}, {"name", "TEXT"}, {"description", "TEXT"}, {"parent_id", "INTEGER"},
	}},
	{`products`, []colSpec{
		{"id", "INTEGER PRIMARY KEY"}, {"name", "TEXT"}, {"sku", "TEXT"}, {"category_id", "INTEGER"},
		{"price", "REAL"}, {"cost", "REAL"}, {"stock", "INTEGER NOT NULL DEFAULT 0"},
		{"discontinued", "INTEGER NOT NULL DEFAULT 0"}, {"description", "TEXT"}, {"weight", "REAL"},
	}},
	{`suppliers`, []colSpec{
		{"id", "INTEGER PRIMARY KEY"}, {"name", "TEXT"}, {"contact", "TEXT"}, {"email", "TEXT"},
		{"phone", "TEXT"}, {"country", "TEXT"},
	}},
	{`orders`, []colSpec{
		{"id", "INTEGER PRIMARY KEY"}, {"customer_id", "INTEGER"}, {"status", "TEXT"},
		{"total", "REAL"}, {"shipping", "REAL"}, {"tax", "REAL"}, {"currency", "TEXT"},
		{"created_at", "TEXT"}, {"shipped_at", "TEXT"},
	}},
	{`order_items`, []colSpec{
		{"id", "INTEGER PRIMARY KEY"}, {"order_id", "INTEGER"}, {"product_id", "INTEGER"},
		{"quantity", "INTEGER"}, {"unit_price", "REAL"}, {"discount", "REAL"},
	}},
	{`invoices`, []colSpec{
		{"id", "INTEGER PRIMARY KEY"}, {"order_id", "INTEGER"}, {"number", "TEXT"},
		{"issued_at", "TEXT"}, {"due_at", "TEXT"}, {"paid_at", "TEXT"}, {"amount", "REAL"}, {"status", "TEXT"},
	}},
	{`payments`, []colSpec{
		{"id", "INTEGER PRIMARY KEY"}, {"invoice_id", "INTEGER"}, {"method", "TEXT"},
		{"amount", "REAL"}, {"reference", "TEXT"}, {"paid_at", "TEXT"},
	}},
	{`departments`, []colSpec{
		{"id", "INTEGER PRIMARY KEY"}, {"name", "TEXT"}, {"location", "TEXT"},
	}},
	{`employees`, []colSpec{
		{"id", "INTEGER PRIMARY KEY"}, {"name", "TEXT"}, {"email", "TEXT"}, {"department_id", "INTEGER"},
		{"salary", "REAL"}, {"hired_at", "TEXT"}, {"manager_id", "INTEGER"},
	}},
	{`projects`, []colSpec{
		{"id", "INTEGER PRIMARY KEY"}, {"name", "TEXT"}, {"department_id", "INTEGER"},
		{"budget", "REAL"}, {"start_at", "TEXT"}, {"end_at", "TEXT"}, {"status", "TEXT"},
	}},
	{`tasks`, []colSpec{
		{"id", "INTEGER PRIMARY KEY"}, {"project_id", "INTEGER"}, {"assignee_id", "INTEGER"},
		{"title", "TEXT"}, {"status", "TEXT"}, {"priority", "INTEGER"}, {"estimate_hours", "REAL"}, {"due_at", "TEXT"},
	}},
	{`comments`, []colSpec{
		{"id", "INTEGER PRIMARY KEY"}, {"task_id", "INTEGER"}, {"author_id", "INTEGER"},
		{"body", "TEXT"}, {"created_at", "TEXT"},
	}},
	{`events`, []colSpec{
		{"id", "INTEGER PRIMARY KEY"}, {"name", "TEXT"}, {"payload", "TEXT"}, {"occurred_at", "TEXT"},
	}},
	{`sessions`, []colSpec{
		{"id", "INTEGER PRIMARY KEY"}, {"user_id", "INTEGER"}, {"token", "TEXT"}, {"ip", "TEXT"},
		{"user_agent", "TEXT"}, {"started_at", "TEXT"}, {"expires_at", "TEXT"},
	}},
	{`notifications`, []colSpec{
		{"id", "INTEGER PRIMARY KEY"}, {"user_id", "INTEGER"}, {"kind", "TEXT"}, {"message", "TEXT"},
		{"read", "INTEGER NOT NULL DEFAULT 0"}, {"created_at", "TEXT"},
	}},
	{`logs`, []colSpec{
		{"id", "INTEGER PRIMARY KEY"}, {"level", "TEXT"}, {"source", "TEXT"}, {"message", "TEXT"}, {"created_at", "TEXT"},
	}},
	{`configs`, []colSpec{
		{"id", "INTEGER PRIMARY KEY"}, {"key", "TEXT UNIQUE"}, {"value", "TEXT"},
	}},
}

// q returns an engine-quoted identifier, mirroring picklock's QuoteIdent dialects.
// MySQL backticks and SQL Server brackets also protect reserved words like
// `key` and `read`; the other engines accept the plain lowercase names.
func q(driver, name string) string {
	switch driver {
	case "mysql", "mariadb":
		return "`" + strings.ReplaceAll(name, "`", "``") + "`"
	case "mssql":
		return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
	}
	return name
}

// mapType maps a SQLite column type string to the target engine.
func mapType(driver, t string) string {
	if t == "INTEGER PRIMARY KEY" {
		switch driver {
		case "postgres":
			return "BIGSERIAL PRIMARY KEY"
		case "mysql", "mariadb":
			return "INT AUTO_INCREMENT PRIMARY KEY"
		case "mssql":
			return "INT IDENTITY(1,1) PRIMARY KEY"
		}
		return t
	}
	switch t {
	case "TEXT":
		return textType(driver)
	case "TEXT NOT NULL":
		return textType(driver) + " NOT NULL"
	case "TEXT UNIQUE":
		return textUniqueType(driver)
	case "REAL":
		return realType(driver)
	default:
		// INTEGER, INTEGER NOT NULL DEFAULT n
		return t
	}
}

func textType(driver string) string {
	if driver == "mssql" {
		return "NVARCHAR(MAX)"
	}
	return "TEXT"
}

func textUniqueType(driver string) string {
	switch driver {
	case "mysql", "mariadb", "mssql":
		// TEXT / NVARCHAR(MAX) cannot be a UNIQUE key column.
		if driver == "mssql" {
			return "NVARCHAR(255) UNIQUE"
		}
		return "VARCHAR(255) UNIQUE"
	default:
		return "TEXT UNIQUE"
	}
}

func realType(driver string) string {
	switch driver {
	case "postgres":
		return "DOUBLE PRECISION"
	case "mysql", "mariadb":
		return "DOUBLE"
	case "mssql":
		return "FLOAT"
	}
	return "REAL"
}

func createTable(driver string, t tableSpec) string {
	cols := make([]string, len(t.cols))
	for i, c := range t.cols {
		cols[i] = q(driver, c.name) + " " + mapType(driver, c.typ)
	}
	// SQL Server has no "IF NOT EXISTS" on CREATE TABLE; Seed drops the tables
	// first, so a plain CREATE is equivalent everywhere.
	create := "CREATE TABLE"
	if driver != "mssql" {
		create += " IF NOT EXISTS"
	}
	return fmt.Sprintf("%s %s (%s)", create, q(driver, t.name), strings.Join(cols, ", "))
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '('); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// ----------------------------------------------------------------- data ----

// seeder inserts the deterministic demo dataset. It builds every INSERT with
// the engine's placeholder syntax (?, $N, @pN) and quotes identifiers.
type seeder struct {
	tx     *sql.Tx
	driver string
	rng    *rand.Rand
	stmts  map[string]*sql.Stmt
}

func (s *seeder) closeStmts() {
	for _, stmt := range s.stmts {
		stmt.Close()
	}
}

func (s *seeder) placeholder(i int) string {
	switch s.driver {
	case "postgres":
		return fmt.Sprintf("$%d", i+1)
	case "mssql":
		return fmt.Sprintf("@p%d", i+1)
	default:
		return "?"
	}
}

// exec runs an INSERT with positional values using the engine's placeholder
// syntax and quoted identifiers, reusing prepared statements per table.
func (s *seeder) exec(table string, cols []string, vals ...any) error {
	stmt, ok := s.stmts[table]
	if !ok {
		names := make([]string, len(cols))
		for i, c := range cols {
			names[i] = q(s.driver, c)
		}
		phs := make([]string, len(cols))
		for i := range cols {
			phs[i] = s.placeholder(i)
		}
		query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
			q(s.driver, table), strings.Join(names, ", "), strings.Join(phs, ", "))
		var err error
		stmt, err = s.tx.Prepare(query)
		if err != nil {
			return err
		}
		s.stmts[table] = stmt
	}
	_, err := stmt.Exec(vals...)
	return err
}

func (s *seeder) data() error {
	ts := time.Now().Add(-365 * 24 * time.Hour)
	rfc := func(d time.Time) string { return d.Format(time.RFC3339) }
	date := func() time.Time { return ts.Add(time.Duration(s.rng.Intn(365*24)) * time.Hour) }
	randStr := func(prefix string, n int) string {
		b := make([]byte, n)
		const letters = "abcdefghijklmnopqrstuvwxyz"
		for i := range b {
			b[i] = letters[s.rng.Intn(len(letters))]
		}
		return prefix + string(b)
	}
	email := func(name string) string {
		return fmt.Sprintf("%s%d@example.com", name, s.rng.Intn(9999))
	}

	names := make([]string, 4000)
	for i := range names {
		names[i] = randStr("user", 6)
	}

	users := []string{"name", "email", "password_hash", "role", "active", "created_at", "updated_at"}
	for i := 1; i <= 2000; i++ {
		n := names[i%len(names)]
		if err := s.exec("users", users,
			n, email(n), randStr("$2a$", 12), []string{"admin", "staff", "viewer"}[s.rng.Intn(3)],
			s.rng.Intn(2), rfc(date()), rfc(date())); err != nil {
			return err
		}
	}

	customers := []string{"first_name", "last_name", "email", "phone", "country", "city", "vip", "loyalty_points", "created_at"}
	for i := 1; i <= 2500; i++ {
		fn := randStr("f", 5)
		ln := randStr("l", 7)
		if err := s.exec("customers", customers,
			fn, ln, email(fn), fmt.Sprintf("+1%09d", s.rng.Intn(1e9)),
			[]string{"US", "UK", "ES", "AR", "BR", "DE"}[s.rng.Intn(6)],
			randStr("C", 6), s.rng.Intn(2), s.rng.Intn(5000), rfc(date())); err != nil {
			return err
		}
	}

	addresses := []string{"customer_id", "street", "city", "state", "zip", "country", "is_primary"}
	for i := 1; i <= 3000; i++ {
		if err := s.exec("addresses", addresses,
			s.rng.Intn(2500)+1, fmt.Sprintf("%d %s St", s.rng.Intn(9999), randStr("S", 6)),
			randStr("City", 5), randStr("ST", 2), fmt.Sprintf("%05d", s.rng.Intn(99999)),
			[]string{"US", "UK", "ES", "AR"}[s.rng.Intn(4)], s.rng.Intn(2)); err != nil {
			return err
		}
	}

	categories := []string{"name", "description", "parent_id"}
	for i := 1; i <= 60; i++ {
		parent := 0
		if i > 10 {
			parent = s.rng.Intn(10) + 1
		}
		if err := s.exec("categories", categories, randStr("cat_", 6), randStr("desc ", 20), parent); err != nil {
			return err
		}
	}

	products := []string{"name", "sku", "category_id", "price", "cost", "stock", "discontinued", "description", "weight"}
	for i := 1; i <= 1500; i++ {
		if err := s.exec("products", products,
			randStr("prod_", 8), randStr("SKU-", 8), s.rng.Intn(60)+1,
			float64(s.rng.Intn(20000))/100, float64(s.rng.Intn(15000))/100,
			s.rng.Intn(500), s.rng.Intn(2), randStr("desc ", 30), float64(s.rng.Intn(5000))/100); err != nil {
			return err
		}
	}

	suppliers := []string{"name", "contact", "email", "phone", "country"}
	for i := 1; i <= 300; i++ {
		if err := s.exec("suppliers", suppliers,
			randStr("supp_", 7), randStr("contact_", 8), email(randStr("s", 5)),
			fmt.Sprintf("+1%09d", s.rng.Intn(1e9)), []string{"CN", "US", "DE", "BR"}[s.rng.Intn(4)]); err != nil {
			return err
		}
	}

	statuses := []string{"pending", "paid", "shipped", "delivered", "cancelled", "refunded"}
	orders := []string{"customer_id", "status", "total", "shipping", "tax", "currency", "created_at", "shipped_at"}
	for i := 1; i <= 6000; i++ {
		if err := s.exec("orders", orders,
			s.rng.Intn(2500)+1, statuses[s.rng.Intn(len(statuses))],
			float64(s.rng.Intn(50000))/100, float64(s.rng.Intn(3000))/100, float64(s.rng.Intn(1500))/100,
			[]string{"USD", "EUR", "GBP", "ARS"}[s.rng.Intn(4)], rfc(date()), rfc(date())); err != nil {
			return err
		}
		items := s.rng.Intn(6) + 1
		for j := 0; j < items; j++ {
			if err := s.exec("order_items", []string{"order_id", "product_id", "quantity", "unit_price", "discount"},
				i, s.rng.Intn(1500)+1, s.rng.Intn(10)+1, float64(s.rng.Intn(20000))/100, float64(s.rng.Intn(50))/100); err != nil {
				return err
			}
		}
	}

	invoices := []string{"order_id", "number", "issued_at", "due_at", "paid_at", "amount", "status"}
	for i := 1; i <= 6000; i++ {
		var paid any
		if s.rng.Intn(2) == 1 {
			paid = rfc(date())
		}
		if err := s.exec("invoices", invoices,
			s.rng.Intn(6000)+1, fmt.Sprintf("INV-%05d", i), rfc(date()), rfc(date()),
			paid, float64(s.rng.Intn(50000))/100, statuses[s.rng.Intn(4)]); err != nil {
			return err
		}
	}

	payments := []string{"invoice_id", "method", "amount", "reference", "paid_at"}
	for i := 1; i <= 8000; i++ {
		if err := s.exec("payments", payments,
			s.rng.Intn(6000)+1, []string{"card", "cash", "transfer", "paypal"}[s.rng.Intn(4)],
			float64(s.rng.Intn(20000))/100, randStr("PAY-", 10), rfc(date())); err != nil {
			return err
		}
	}

	departments := []string{"name", "location"}
	for i := 1; i <= 25; i++ {
		if err := s.exec("departments", departments, randStr("dept_", 6), randStr("loc_", 5)); err != nil {
			return err
		}
	}

	employees := []string{"name", "email", "department_id", "salary", "hired_at", "manager_id"}
	for i := 1; i <= 800; i++ {
		if err := s.exec("employees", employees,
			randStr("emp_", 7), email(randStr("e", 5)), s.rng.Intn(25)+1,
			float64(s.rng.Intn(150000)+30000)/100, rfc(date()), s.rng.Intn(800)+1); err != nil {
			return err
		}
	}

	projects := []string{"name", "department_id", "budget", "start_at", "end_at", "status"}
	for i := 1; i <= 400; i++ {
		if err := s.exec("projects", projects,
			randStr("proj_", 7), s.rng.Intn(25)+1, float64(s.rng.Intn(1000000))/100,
			rfc(date()), rfc(date()), statuses[s.rng.Intn(3)]); err != nil {
			return err
		}
	}

	taskStatus := []string{"todo", "doing", "review", "done"}
	tasks := []string{"project_id", "assignee_id", "title", "status", "priority", "estimate_hours", "due_at"}
	for i := 1; i <= 4000; i++ {
		if err := s.exec("tasks", tasks,
			s.rng.Intn(400)+1, s.rng.Intn(800)+1, randStr("task_", 10),
			taskStatus[s.rng.Intn(len(taskStatus))], s.rng.Intn(5)+1,
			float64(s.rng.Intn(80))/10, rfc(date())); err != nil {
			return err
		}
	}

	comments := []string{"task_id", "author_id", "body", "created_at"}
	for i := 1; i <= 8000; i++ {
		if err := s.exec("comments", comments,
			s.rng.Intn(4000)+1, s.rng.Intn(800)+1, randStr("comment ", 60), rfc(date())); err != nil {
			return err
		}
	}

	events := []string{"name", "payload", "occurred_at"}
	for i := 1; i <= 10000; i++ {
		if err := s.exec("events", events,
			[]string{"login", "logout", "click", "purchase", "export"}[s.rng.Intn(5)],
			randStr("{} \"payload\": \"", 30), rfc(date())); err != nil {
			return err
		}
	}

	sessions := []string{"user_id", "token", "ip", "user_agent", "started_at", "expires_at"}
	for i := 1; i <= 12000; i++ {
		if err := s.exec("sessions", sessions,
			s.rng.Intn(2000)+1, randStr("tok_", 24),
			fmt.Sprintf("%d.%d.%d.%d", s.rng.Intn(256), s.rng.Intn(256), s.rng.Intn(256), s.rng.Intn(256)),
			randStr("ua/", 20), rfc(date()), rfc(date())); err != nil {
			return err
		}
	}

	notifications := []string{"user_id", "kind", "message", "read", "created_at"}
	for i := 1; i <= 15000; i++ {
		if err := s.exec("notifications", notifications,
			s.rng.Intn(2000)+1, []string{"info", "alert", "billing", "security"}[s.rng.Intn(4)],
			randStr("msg ", 40), s.rng.Intn(2), rfc(date())); err != nil {
			return err
		}
	}

	logs := []string{"level", "source", "message", "created_at"}
	for i := 1; i <= 30000; i++ {
		if err := s.exec("logs", logs,
			[]string{"debug", "info", "warn", "error"}[s.rng.Intn(4)],
			[]string{"api", "db", "auth", "worker", "cron"}[s.rng.Intn(5)],
			randStr("log ", 50), rfc(date())); err != nil {
			return err
		}
	}

	configs := []string{"key", "value"}
	for i := 1; i <= 200; i++ {
		if err := s.exec("configs", configs, randStr("cfg.", 12), randStr("v", 16)); err != nil {
			return err
		}
	}

	return nil
}
