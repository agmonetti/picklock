package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/agmonetti/picklock/internal/conn"
)

// envCfg builds the config from env vars, or skips if not set.
func envCfg(t *testing.T, prefix string) conn.ConnectionConfig {
	t.Helper()
	host := os.Getenv(prefix + "_HOST")
	if host == "" {
		t.Skipf("env %s_HOST not set; skipping integration test", prefix)
	}
	return conn.ConnectionConfig{
		Driver:   conn.DriverPostgres,
		Host:     host,
		Port:     5432,
		User:     os.Getenv(prefix + "_USER"),
		Password: os.Getenv(prefix + "_PASSWORD"),
		Database: os.Getenv(prefix + "_DATABASE"),
	}
}

// TestIntegration exercises the Store interface against a real server.
func TestIntegration(t *testing.T) {
	cfg := envCfg(t, "PICKLOCK_TEST_POSTGRES")

	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	if _, err := s.Exec("DROP TABLE IF EXISTS picklock_test"); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := s.Exec("CREATE TABLE picklock_test (id SERIAL PRIMARY KEY, name TEXT NOT NULL, email TEXT)"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.Exec("INSERT INTO picklock_test (name, email) VALUES ('Alice','a@t.com'), ('Bob','b@t.com')"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	t.Cleanup(func() {
		s.Exec("DROP TABLE IF EXISTS picklock_test")
	})

	tables, err := s.Tables()
	if err != nil {
		t.Fatalf("Tables: %v", err)
	}
	if !contains(tables, "picklock_test") {
		t.Errorf("Tables does not include picklock_test: %v", tables)
	}

	cols, err := s.Columns("picklock_test")
	if err != nil {
		t.Fatalf("Columns: %v", err)
	}
	if len(cols) != 3 {
		t.Fatalf("len(Columns) = %d, want 3", len(cols))
	}
	if !cols[0].PK || !cols[1].NotNull {
		t.Errorf("constraints: %+v", cols)
	}

	n, err := s.CountTable("picklock_test")
	if err != nil {
		t.Fatalf("CountTable: %v", err)
	}
	if n != 2 {
		t.Errorf("CountTable = %d, want 2", n)
	}

	page, err := s.SelectTablePage("picklock_test", 10, 0)
	if err != nil {
		t.Fatalf("SelectTablePage: %v", err)
	}
	if len(page.Rows) != 2 || page.Rows[0][1] != "Alice" {
		t.Errorf("page.Rows = %v", page.Rows)
	}

	// keyset pagination over the primary key
	first, err := s.SelectTableKeysetPage("picklock_test", "id", 10, "")
	if err != nil {
		t.Fatalf("SelectTableKeysetPage first: %v", err)
	}
	if len(first.Rows) != 2 || first.Rows[0][1] != "Alice" {
		t.Errorf("first keyset page = %v", first.Rows)
	}
	last := first.Rows[len(first.Rows)-1][0]
	second, err := s.SelectTableKeysetPage("picklock_test", "id", 10, last)
	if err != nil {
		t.Fatalf("SelectTableKeysetPage second: %v", err)
	}
	if len(second.Rows) != 0 {
		t.Errorf("second keyset page = %v, want empty", second.Rows)
	}

	if v, err := s.Version(); err != nil || v == "" {
		t.Errorf("Version = %q, err=%v", v, err)
	}

	fks, err := s.ForeignKeysContext(context.Background())
	if err != nil {
		t.Fatalf("ForeignKeysContext: %v", err)
	}
	for _, fk := range fks {
		if fk.Table == "" || fk.ReferencedTable == "" {
			t.Errorf("ForeignKeysContext returned incomplete key: %+v", fk)
		}
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// TestIntegrationReadOnly verifies that a read-only connection rejects writes
// (via default_transaction_read_only) while reads keep working.
func TestIntegrationReadOnly(t *testing.T) {
	cfg := envCfg(t, "PICKLOCK_TEST_POSTGRES")

	rc := cfg
	rc.ReadOnly = true
	ro, err := New(rc)
	if err != nil {
		t.Fatalf("New read-only: %v", err)
	}
	defer ro.Close()

	if _, err := ro.Exec("CREATE TABLE picklock_ro_test (id INT)"); err == nil {
		t.Error("write must fail in read-only mode")
	}
	if _, err := ro.Query("SELECT 1"); err != nil {
		t.Errorf("read in read-only mode failed: %v", err)
	}
}
