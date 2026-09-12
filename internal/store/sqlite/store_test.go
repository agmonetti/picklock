package sqlite

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/agmonetti/picklock/internal/conn"
)

func TestOpenMemoryAndIntrospect(t *testing.T) {
	cfg := conn.New(conn.DriverSQLite)
	cfg.Path = ":memory:"

	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	if _, err := s.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL, email TEXT)"); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if _, err := s.Exec("INSERT INTO users (name) VALUES ('Alice'), ('Bob')"); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	tables, err := s.Tables()
	if err != nil {
		t.Fatalf("Tables: %v", err)
	}
	if len(tables) != 1 || tables[0] != "users" {
		t.Fatalf("Tables = %v, want [users]", tables)
	}

	cols, err := s.Columns("users")
	if err != nil {
		t.Fatalf("Columns: %v", err)
	}
	if len(cols) != 3 {
		t.Fatalf("len(Columns) = %d, want 3", len(cols))
	}
	if !cols[0].PK || !cols[1].NotNull || cols[2].NotNull {
		t.Errorf("bad constraints: %+v", cols)
	}

	n, err := s.CountTable("users")
	if err != nil {
		t.Fatalf("CountTable: %v", err)
	}
	if n != 2 {
		t.Errorf("CountTable = %d, want 2", n)
	}

	page, err := s.SelectTablePage("users", 10, 0)
	if err != nil {
		t.Fatalf("SelectTablePage: %v", err)
	}
	if len(page.Rows) != 2 || page.Rows[0][1] != "Alice" {
		t.Errorf("page.Rows = %v", page.Rows)
	}
}

func TestOpenMissingFileErrors(t *testing.T) {
	cfg := conn.New(conn.DriverSQLite)
	cfg.Path = filepath.Join(t.TempDir(), "missing.db")

	if _, err := New(cfg); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

func TestOpenPathWithSpecialChars(t *testing.T) {
	for _, name := range []string{"a?b.db", "a#b.db", "a b.db", "a%b.db"} {
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatalf("create %q: %v", name, err)
		}
		cfg := conn.New(conn.DriverSQLite)
		cfg.Path = path
		s, err := New(cfg)
		if err != nil {
			t.Errorf("New(%q): %v", name, err)
			continue
		}
		if _, err := s.Exec("CREATE TABLE t (x INT)"); err != nil {
			t.Errorf("Exec(%q): %v", name, err)
		}
		s.Close()
	}
}

func TestDsnPathEscapesQueryChars(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/data/app.db", "/data/app.db"},
		{"demo.db", "demo.db"},
		{":memory:", ":memory:"},
		{"/tmp/a?b.db", "/tmp/a%3Fb.db"},
		{"/tmp/a#b.db", "/tmp/a%23b.db"},
		{"/tmp/a b.db", "/tmp/a%20b.db"},
		{"/tmp/a%b.db", "/tmp/a%25b.db"},
	}
	for _, tc := range cases {
		if got := dsnPath(tc.in); got != tc.want {
			t.Errorf("dsnPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSelectTableKeysetPage(t *testing.T) {
	cfg := conn.New(conn.DriverSQLite)
	cfg.Path = ":memory:"
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()
	if _, err := s.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)"); err != nil {
		t.Fatalf("create: %v", err)
	}
	for i := 0; i < 55; i++ {
		if _, err := s.Exec("INSERT INTO users (name) VALUES ('u" + itoa(i) + "')"); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// first page: no cursor
	first, err := s.SelectTableKeysetPage("users", "id", 50, "")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if len(first.Rows) != 50 || first.Rows[0][1] != "u0" {
		t.Errorf("first = %d rows, first %v", len(first.Rows), first.Rows[0])
	}

	// second page after the last id of the first
	last := first.Rows[len(first.Rows)-1][0]
	second, err := s.SelectTableKeysetPage("users", "id", 50, last)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if len(second.Rows) != 5 || second.Rows[0][1] != "u50" {
		t.Errorf("second = %d rows, first %v", len(second.Rows), second.Rows[0])
	}

	// beyond the end: empty
	third, err := s.SelectTableKeysetPage("users", "id", 50, second.Rows[len(second.Rows)-1][0])
	if err != nil {
		t.Fatalf("third: %v", err)
	}
	if len(third.Rows) != 0 {
		t.Errorf("third = %v, want empty", third.Rows)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestReadOnlyBlocksWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ro.db")
	// picklock does not create files: we create the .db before opening it.
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("create file: %v", err)
	}

	cfg := conn.New(conn.DriverSQLite)
	cfg.Path = path
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New (crear): %v", err)
	}
	if _, err := s.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)"); err != nil {
		t.Fatalf("Exec create table: %v", err)
	}
	s.Close()

	cfg.ReadOnly = true
	ro, err := New(cfg)
	if err != nil {
		t.Fatalf("New (read-only): %v", err)
	}
	defer ro.Close()

	if _, err := ro.Exec("INSERT INTO users (name) VALUES ('x')"); err == nil {
		t.Error("expected a write error in read-only mode")
	}
	tables, err := ro.Tables()
	if err != nil {
		t.Fatalf("Tables read-only: %v", err)
	}
	if len(tables) != 1 || tables[0] != "users" {
		t.Errorf("Tables = %v, want [users]", tables)
	}
}
