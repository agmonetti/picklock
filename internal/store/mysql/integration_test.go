package mysql

import (
	"os"
	"testing"

	"github.com/agmonetti/picklock/internal/conn"
)

func envCfg(t *testing.T, prefix string) conn.ConnectionConfig {
	t.Helper()
	host := os.Getenv(prefix + "_HOST")
	if host == "" {
		t.Skipf("env %s_HOST not set; skipping integration test", prefix)
	}
	port := 3306
	return conn.ConnectionConfig{
		Driver:   conn.DriverMySQL,
		Host:     host,
		Port:     port,
		User:     os.Getenv(prefix + "_USER"),
		Password: os.Getenv(prefix + "_PASSWORD"),
		Database: os.Getenv(prefix + "_DATABASE"),
	}
}

// TestIntegrationMySQL exercises the engine against a real MySQL.
func TestIntegrationMySQL(t *testing.T) {
	cfg := envCfg(t, "PICKLOCK_TEST_MYSQL")
	cfg.Driver = conn.DriverMySQL
	testStore(t, cfg)
}

// TestIntegrationMariaDB exercises the engine against a real MariaDB.
func TestIntegrationMariaDB(t *testing.T) {
	cfg := envCfg(t, "PICKLOCK_TEST_MARIADB")
	cfg.Driver = conn.DriverMariaDB
	testStore(t, cfg)
}

func testStore(t *testing.T, cfg conn.ConnectionConfig) {
	var (
		s   *Store
		err error
	)
	if cfg.Driver == conn.DriverMariaDB {
		s, err = NewMariaDB(cfg)
	} else {
		s, err = NewMySQL(cfg)
	}
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	if _, err := s.Exec("DROP TABLE IF EXISTS picklock_test"); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := s.Exec("CREATE TABLE picklock_test (id INT AUTO_INCREMENT PRIMARY KEY, name VARCHAR(255) NOT NULL, email VARCHAR(255))"); err != nil {
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
}

// TestIntegrationTLSOptions verifies the TLS field values that open a
// connection against the compose containers (prefer and disable; "require"
// needs a server with a trusted certificate, out of scope here).
func TestIntegrationTLSOptions(t *testing.T) {
	drivers := []struct {
		driver conn.Driver
		prefix string
	}{
		{conn.DriverMySQL, "PICKLOCK_TEST_MYSQL"},
		{conn.DriverMariaDB, "PICKLOCK_TEST_MARIADB"},
	}
	for _, d := range drivers {
		t.Run(string(d.driver), func(t *testing.T) {
			for _, ssl := range []string{"prefer", "disable"} {
				cfg := envCfg(t, d.prefix)
				cfg.Driver = d.driver
				cfg.SSLMode = ssl
				var (
					s   *Store
					err error
				)
				if d.driver == conn.DriverMariaDB {
					s, err = NewMariaDB(cfg)
				} else {
					s, err = NewMySQL(cfg)
				}
				if err != nil {
					t.Fatalf("New(tls=%s): %v", ssl, err)
				}
				if _, err := s.Query("SELECT 1"); err != nil {
					t.Errorf("query tls=%s: %v", ssl, err)
				}
				s.Close()
			}
		})
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
// (via an pinned connection with SET SESSION TRANSACTION READ ONLY) while reads
// keep working. A writable connection prepares the table first.
func TestIntegrationReadOnly(t *testing.T) {
	for _, driver := range []conn.Driver{conn.DriverMySQL, conn.DriverMariaDB} {
		t.Run(string(driver), func(t *testing.T) {
			prefix := "PICKLOCK_TEST_MYSQL"
			if driver == conn.DriverMariaDB {
				prefix = "PICKLOCK_TEST_MARIADB"
			}
			cfg := envCfg(t, prefix)
			cfg.Driver = driver

			var open = func(c conn.ConnectionConfig) (*Store, error) {
				if c.Driver == conn.DriverMariaDB {
					return NewMariaDB(c)
				}
				return NewMySQL(c)
			}
			w, err := open(cfg)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if _, err := w.Exec("DROP TABLE IF EXISTS picklock_ro_test"); err != nil {
				t.Fatalf("drop: %v", err)
			}
			if _, err := w.Exec("CREATE TABLE picklock_ro_test (id INT)"); err != nil {
				t.Fatalf("create: %v", err)
			}
			if n, err := w.Exec("INSERT INTO picklock_ro_test VALUES (1)"); err != nil || n != 1 {
				t.Fatalf("seed insert: n=%d err=%v", n, err)
			}
			w.Close()

			rc := cfg
			rc.ReadOnly = true
			ro, err := open(rc)
			if err != nil {
				t.Fatalf("New read-only: %v", err)
			}
			defer ro.Close()

			if _, err := ro.Exec("INSERT INTO picklock_ro_test VALUES (2)"); err == nil {
				t.Error("write must fail in read-only mode")
			}
			if n, err := ro.CountTable("picklock_ro_test"); err != nil || n != 1 {
				t.Errorf("read in read-only mode: n=%d err=%v, want 1", n, err)
			}
		})
	}
}
