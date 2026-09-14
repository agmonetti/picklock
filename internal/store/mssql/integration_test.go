package mssql

import (
	"context"
	"os"
	"testing"

	"github.com/agmonetti/picklock/internal/conn"
)

func envCfg(t *testing.T) conn.ConnectionConfig {
	t.Helper()
	host := os.Getenv("PICKLOCK_TEST_MSSQL_HOST")
	if host == "" {
		t.Skip("env PICKLOCK_TEST_MSSQL_HOST not set; skipping integration test")
	}
	return conn.ConnectionConfig{
		Driver:   conn.DriverMSSQL,
		Host:     host,
		Port:     1433,
		User:     os.Getenv("PICKLOCK_TEST_MSSQL_USER"),
		Password: os.Getenv("PICKLOCK_TEST_MSSQL_PASSWORD"),
		Database: os.Getenv("PICKLOCK_TEST_MSSQL_DATABASE"),
	}
}

// TestIntegration exercises the engine against a real SQL Server.
func TestIntegration(t *testing.T) {
	cfg := envCfg(t)

	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	if _, err := s.Exec("IF OBJECT_ID('picklock_test') IS NOT NULL DROP TABLE picklock_test"); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := s.Exec("CREATE TABLE picklock_test (id INT IDENTITY PRIMARY KEY, name NVARCHAR(255) NOT NULL, email NVARCHAR(255))"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.Exec("INSERT INTO picklock_test (name, email) VALUES ('Alice','a@t.com'), ('Bob','b@t.com')"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	t.Cleanup(func() {
		s.Exec("IF OBJECT_ID('picklock_test') IS NOT NULL DROP TABLE picklock_test")
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
	if len(fks) != 0 {
		t.Errorf("ForeignKeysContext = %v, want no foreign keys", fks)
	}
}

// TestIntegrationTLSOptions verifies the TLS field values that open a
// connection against the compose container (prefer keeps the driver default,
// disable sends plaintext; "require" needs a trusted certificate).
func TestIntegrationTLSOptions(t *testing.T) {
	for _, ssl := range []string{"prefer", "disable"} {
		cfg := envCfg(t)
		cfg.SSLMode = ssl
		s, err := New(cfg)
		if err != nil {
			t.Fatalf("New(tls=%s): %v", ssl, err)
		}
		if _, err := s.Query("SELECT @@VERSION"); err != nil {
			t.Errorf("query tls=%s: %v", ssl, err)
		}
		s.Close()
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
