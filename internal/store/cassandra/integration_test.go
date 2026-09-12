package cassandra

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/gocql/gocql"

	"github.com/agmonetti/picklock/internal/conn"
	"github.com/agmonetti/picklock/internal/store"
)

const itKeyspace = "picklock_it_test"

func cassEnvCfg(t *testing.T) conn.ConnectionConfig {
	t.Helper()
	host := os.Getenv("PICKLOCK_TEST_CASSANDRA_HOST")
	if host == "" {
		t.Skipf("env PICKLOCK_TEST_CASSANDRA_HOST not set; skipping integration test")
	}
	port := 9042
	if p := os.Getenv("PICKLOCK_TEST_CASSANDRA_PORT"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			port = n
		}
	}
	return conn.ConnectionConfig{
		Driver:   conn.DriverCassandra,
		Host:     host,
		Port:     port,
		User:     os.Getenv("PICKLOCK_TEST_CASSANDRA_USER"),
		Password: os.Getenv("PICKLOCK_TEST_CASSANDRA_PASSWORD"),
		Database: itKeyspace,
	}
}

// cassSystemSession connects with no default keyspace so DDL can run.
func cassSystemSession(t *testing.T, cfg conn.ConnectionConfig) *gocql.Session {
	t.Helper()
	cluster := gocql.NewCluster(cfg.Host)
	cluster.Port = cfg.Port
	cluster.ConnectTimeout = 15 * time.Second
	cluster.Timeout = 15 * time.Second
	if cfg.User != "" {
		cluster.Authenticator = gocql.PasswordAuthenticator{Username: cfg.User, Password: cfg.Password}
	}
	session, err := cluster.CreateSession()
	if err != nil {
		t.Fatalf("system session: %v", err)
	}
	return session
}

// cassEnsureKeyspace recreates the dedicated keyspace and registers a cleanup.
func cassEnsureKeyspace(t *testing.T, cfg conn.ConnectionConfig) {
	t.Helper()
	session := cassSystemSession(t, cfg)
	defer session.Close()
	for _, q := range []string{
		fmt.Sprintf("DROP KEYSPACE IF EXISTS %s", itKeyspace),
		fmt.Sprintf("CREATE KEYSPACE %s WITH replication = {'class':'SimpleStrategy','replication_factor':1}", itKeyspace),
	} {
		if err := session.Query(q).Exec(); err != nil {
			t.Fatalf("keyspace setup %q: %v", q, err)
		}
	}
	t.Cleanup(func() {
		s2 := cassSystemSession(t, cfg)
		defer s2.Close()
		_ = s2.Query(fmt.Sprintf("DROP KEYSPACE IF EXISTS %s", itKeyspace)).Exec()
	})
}

// TestIntegration exercises the CassandraSource against a real server,
// including PageState cursor pagination (previously replayed page 1).
func TestIntegration(t *testing.T) {
	cfg := cassEnvCfg(t)
	cassEnsureKeyspace(t, cfg)

	ds, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer ds.Close()

	ctx := context.Background()
	setup := []string{
		"CREATE TABLE IF NOT EXISTS it_tbl (pk text, cc timestamp, val text, PRIMARY KEY ((pk), cc))",
	}
	for _, q := range setup {
		if err := ds.(*CassandraSource).session.Query(q).Exec(); err != nil {
			t.Fatalf("setup %q: %v", q, err)
		}
	}
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 6; i++ {
		q := "INSERT INTO it_tbl (pk, cc, val) VALUES (?, ?, ?)"
		if err := ds.(*CassandraSource).session.Query(q, "p1", base.Add(time.Duration(i)*time.Minute), "v"+strconv.Itoa(i)).Exec(); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	if v, err := ds.Version(ctx); err != nil || v == "" {
		t.Errorf("Version = %q err=%v", v, err)
	}

	// Catalog lists the seeded table
	items, err := ds.Catalog().ListObjects(ctx)
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	if !cassHasTable(items, "it_tbl") {
		t.Fatalf("catalog missing it_tbl: %v", items)
	}

	// Page 0: 2 rows + a page-state cursor; page 1 must return different rows
	resp, err := ds.Browse(ctx, store.BrowseRequest{ObjectName: "it_tbl", PageSize: 2})
	if err != nil {
		t.Fatalf("Browse page 0: %v", err)
	}
	vals0 := cassVals(resp)
	if len(vals0) != 2 || resp.NextCursor == "" || !resp.HasNext {
		t.Fatalf("page 0: %d rows cursor=%q hasNext=%v", len(vals0), resp.NextCursor, resp.HasNext)
	}
	resp2, err := ds.Browse(ctx, store.BrowseRequest{ObjectName: "it_tbl", PageSize: 2, Cursor: resp.NextCursor})
	if err != nil {
		t.Fatalf("Browse page 1: %v", err)
	}
	vals1 := cassVals(resp2)
	if len(vals1) != 2 {
		t.Fatalf("page 1 rows = %d, want 2", len(vals1))
	}
	for _, v := range vals1 {
		if cassValsContains(vals0, v) {
			t.Errorf("page 1 replayed row %q from page 0", v)
		}
	}

	// Inspect reports partition key + clustering column
	insp, err := ds.Inspect(ctx, "it_tbl")
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	rel, ok := insp.(*store.RelationalStructure)
	if !ok {
		t.Fatalf("Inspect = %T, want RelationalStructure", insp)
	}
	hasPK, hasCC := false, false
	for _, c := range rel.Columns {
		if c.PK {
			hasPK = true
		}
		if c.Clustering {
			hasCC = true
		}
	}
	if !hasPK || !hasCC {
		t.Errorf("columns lack PK/CC flags: %+v", rel.Columns)
	}

	// Editor queries: SELECT with a hard maxRows cap (no unbounded fetch)
	exec := ds.Query()
	data, err := exec.Execute(ctx, "SELECT * FROM it_tbl", 0, 2)
	if err != nil {
		t.Fatalf("SELECT capped: %v", err)
	}
	tab, ok := data.(*store.TabularData)
	if !ok || len(tab.Rows) != 2 || !tab.Truncated {
		t.Errorf("SELECT capped = %T rows=%d truncated=%v, want 2 rows truncated", data, cassRows(data), cassTrunc(data))
	}

	// Applied CQL with a predicate
	data, err = exec.Execute(ctx, "SELECT val FROM it_tbl WHERE pk = 'p1'", 0, 100)
	if err != nil {
		t.Fatalf("SELECT predicate: %v", err)
	}
	if tabs, ok := data.(*store.TabularData); !ok || len(tabs.Rows) != 6 {
		t.Errorf("predicate query = %T rows=%d, want 6", data, cassRows(data))
	}

	// An empty table must still expose its schema columns: the browser and
	// the editor draw the headers even with zero rows (same contract as the
	// relational engines, where columns come from the schema, not the rows).
	if err := ds.(*CassandraSource).session.Query(
		"CREATE TABLE IF NOT EXISTS it_empty (k text PRIMARY KEY, v int)").Exec(); err != nil {
		t.Fatalf("create it_empty: %v", err)
	}
	respE, err := ds.Browse(ctx, store.BrowseRequest{ObjectName: "it_empty", PageSize: 2})
	if err != nil {
		t.Fatalf("Browse empty table: %v", err)
	}
	tabE, ok := respE.Data.(*store.TabularData)
	if !ok || len(tabE.Rows) != 0 {
		t.Fatalf("empty browse = %T rows=%d, want TabularData with 0 rows", respE.Data, cassRows(respE.Data))
	}
	if len(tabE.Columns) != 2 || tabE.Columns[0] != "k" || tabE.Columns[1] != "v" {
		t.Errorf("empty table columns = %v, want [k v]", tabE.Columns)
	}
	data, err = exec.Execute(ctx, "SELECT * FROM it_empty", 0, 10)
	if err != nil {
		t.Fatalf("SELECT empty table: %v", err)
	}
	if tab, ok := data.(*store.TabularData); !ok || len(tab.Columns) != 2 || len(tab.Rows) != 0 {
		t.Errorf("SELECT empty = %T columns=%v rows=%d, want headers [k v] and 0 rows",
			data, cassCols(data), cassRows(data))
	}
}

func cassCols(v store.DataView) []string {
	if tab, ok := v.(*store.TabularData); ok {
		return tab.Columns
	}
	return nil
}

func cassHasTable(items []store.CatalogItem, name string) bool {
	for _, it := range items {
		if it.Name == name {
			return true
		}
	}
	return false
}

func cassVals(resp store.BrowseResponse) []string {
	tab, ok := resp.Data.(*store.TabularData)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(tab.Rows))
	for _, r := range tab.Rows {
		out = append(out, r[len(r)-1]) // val column
	}
	return out
}

func cassValsContains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func cassRows(v store.DataView) int {
	if tab, ok := v.(*store.TabularData); ok {
		return len(tab.Rows)
	}
	return -1
}

func cassTrunc(v store.DataView) bool {
	if tab, ok := v.(*store.TabularData); ok {
		return tab.Truncated
	}
	return false
}

// TestIntegrationReadOnly blocks mutations and allows reads.
func TestIntegrationReadOnly(t *testing.T) {
	cfg := cassEnvCfg(t)
	cassEnsureKeyspace(t, cfg)
	cfg.ReadOnly = true

	ds, err := New(cfg)
	if err != nil {
		t.Fatalf("New (read-only): %v", err)
	}
	defer ds.Close()

	ctx := context.Background()
	if _, err := ds.Query().Execute(ctx, "INSERT INTO it_tbl (pk, cc, val) VALUES ('p2', now(), 'x')", 0, 10); err == nil {
		t.Error("INSERT in read-only must be blocked")
	}
	if _, err := ds.Query().Execute(ctx, "SELECT * FROM system.local", 0, 10); err != nil {
		t.Errorf("read in read-only failed: %v", err)
	}
}
