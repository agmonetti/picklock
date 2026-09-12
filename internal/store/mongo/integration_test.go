package mongo

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"

	"github.com/agmonetti/picklock/internal/conn"
	"github.com/agmonetti/picklock/internal/store"
)

// envCfg builds the config from PICKLOCK_TEST_MONGODB_* env vars, or skips.
func envCfg(t *testing.T) conn.ConnectionConfig {
	t.Helper()
	host := os.Getenv("PICKLOCK_TEST_MONGODB_HOST")
	if host == "" {
		t.Skipf("env PICKLOCK_TEST_MONGODB_HOST not set; skipping integration test")
	}
	port := 0
	if p := os.Getenv("PICKLOCK_TEST_MONGODB_PORT"); p != "" {
		port, _ = strconv.Atoi(p)
	}
	db := os.Getenv("PICKLOCK_TEST_MONGODB_DATABASE")
	if db == "" {
		db = "test"
	}
	return conn.ConnectionConfig{
		Driver:   conn.DriverMongo,
		Host:     host,
		Port:     port,
		User:     os.Getenv("PICKLOCK_TEST_MONGODB_USER"),
		Password: os.Getenv("PICKLOCK_TEST_MONGODB_PASSWORD"),
		Database: db,
	}
}

func mongoSeed(t *testing.T, cfg conn.ConnectionConfig, coll string, n int) {
	t.Helper()
	ds, err := New(cfg)
	if err != nil {
		t.Fatalf("seed New: %v", err)
	}
	defer ds.Close()
	src := ds.(*MongoSource)
	docs := make([]any, 0, n)
	for i := 0; i < n; i++ {
		docs = append(docs, bson.M{"_id": i + 1, "num": i + 1, "name": "doc" + strconv.Itoa(i+1)})
	}
	if _, err := src.database.Collection(coll).InsertMany(context.Background(), docs); err != nil {
		t.Fatalf("seed insert: %v", err)
	}
}

func mongoDrop(t *testing.T, cfg conn.ConnectionConfig, coll string) {
	t.Helper()
	ds, err := New(cfg)
	if err != nil {
		return
	}
	defer ds.Close()
	src := ds.(*MongoSource)
	_ = src.database.Collection(coll).Drop(context.Background())
}

// TestIntegration exercises the MongoSource against a real server, including
// the editor find path (offset must be 0 and the limit capped at maxRows).
func TestIntegration(t *testing.T) {
	cfg := envCfg(t)
	const coll = "picklock_integration"
	mongoDrop(t, cfg, coll)
	mongoSeed(t, cfg, coll, 12)

	ds, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer ds.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if v, err := ds.Version(ctx); err != nil || v == "" {
		t.Errorf("Version = %q, err=%v", v, err)
	}

	// Catalog lists the seeded collection
	items, err := ds.Catalog().ListObjects(ctx)
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	if !mongoHasColl(items, coll) {
		t.Fatalf("catalog missing %s: %v", coll, items)
	}

	// Browse page 1 (pageSize 5 -> 5 rows), page 2 (5), page 3 (2, last)
	resp, err := ds.Browse(ctx, store.BrowseRequest{ObjectName: coll, PageSize: 5, Page: 0})
	if err != nil {
		t.Fatalf("Browse page 0: %v", err)
	}
	docData, ok := resp.Data.(*store.DocumentData)
	if !ok || len(docData.Documents) != 5 {
		t.Fatalf("Browse page 0 = %T (%d docs), want 5", resp.Data, lenDocData(resp.Data))
	}
	if !resp.HasNext {
		t.Error("page 0 should have a next page")
	}
	if resp.TotalCount != 12 {
		t.Errorf("TotalCount = %d, want 12", resp.TotalCount)
	}

	resp2, err := ds.Browse(ctx, store.BrowseRequest{ObjectName: coll, PageSize: 5, Page: 1, Cursor: resp.NextCursor})
	if err != nil {
		t.Fatalf("Browse page 1: %v", err)
	}
	if lenDocData(resp2.Data) != 5 {
		t.Fatalf("Browse page 1 docs = %d, want 5", lenDocData(resp2.Data))
	}

	resp3, err := ds.Browse(ctx, store.BrowseRequest{ObjectName: coll, PageSize: 5, Page: 2, Cursor: resp2.NextCursor})
	if err != nil {
		t.Fatalf("Browse page 2: %v", err)
	}
	if lenDocData(resp3.Data) != 2 || resp3.HasNext {
		t.Errorf("Browse page 2 = %d docs, HasNext=%v, want 2/false", lenDocData(resp3.Data), resp3.HasNext)
	}

	// Inspect returns collection stats + inferred fields
	insp, err := ds.Inspect(ctx, coll)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	docStruct, ok := insp.(*store.DocumentStructure)
	if !ok || docStruct.DocCount < 12 {
		t.Errorf("Inspect = %T (count=%d), want DocumentStructure with >= 12 docs", insp, docCountOf(insp))
	}

	// Editor queries: the find path must NOT skip the first rows
	exec := ds.Query()
	data, err := exec.Execute(ctx, "db."+coll+".find({})", 0, 3)
	if err != nil {
		t.Fatalf("find({}) maxRows=3: %v", err)
	}
	if n := lenDocData(data); n != 3 {
		t.Errorf("find({}) returned %d docs, want 3 (limit caps and no skip)", n)
	}

	// Unquoted shell-style filter (USAGE.md documents this exact syntax)
	data, err = exec.Execute(ctx, "db."+coll+".find({ num: { $gt: 5 } })", 0, 100)
	if err != nil {
		t.Fatalf("find with unquoted keys: %v", err)
	}
	if n := lenDocData(data); n != 7 {
		t.Errorf("find({num:{$gt:5}}) = %d docs, want 7", n)
	}

	// Strict JSON keeps working
	data, err = exec.Execute(ctx, `db.`+coll+`.find({"num": 1})`, 0, 100)
	if err != nil {
		t.Fatalf("find strict JSON: %v", err)
	}
	if n := lenDocData(data); n != 1 {
		t.Errorf("find({\"num\":1}) = %d docs, want 1", n)
	}

	// countDocuments
	data, err = exec.Execute(ctx, "db."+coll+".countDocuments({})", 0, 100)
	if err != nil {
		t.Fatalf("countDocuments: %v", err)
	}
	if tab, ok := data.(*store.TabularData); !ok || tab.Rows[0][0] != "12" {
		t.Errorf("countDocuments = %v, want 12", data)
	}
}

func mongoHasColl(items []store.CatalogItem, name string) bool {
	for _, it := range items {
		if it.Name == name {
			return true
		}
	}
	return false
}

func lenDocData(v store.DataView) int {
	if d, ok := v.(*store.DocumentData); ok {
		return len(d.Documents)
	}
	if t, ok := v.(*store.TabularData); ok {
		return len(t.Rows)
	}
	return -1
}

func docCountOf(v store.InspectionView) int64 {
	if d, ok := v.(*store.DocumentStructure); ok {
		return d.DocCount
	}
	return -1
}

// TestIntegrationReadOnly verifies writes are blocked and reads keep working.
func TestIntegrationReadOnly(t *testing.T) {
	cfg := envCfg(t)
	cfg.ReadOnly = true

	ds, err := New(cfg)
	if err != nil {
		t.Fatalf("New (read-only): %v", err)
	}
	defer ds.Close()

	ctx := context.Background()
	_, err = ds.Query().Execute(ctx, "db.nonexistent.insertOne({x: 1})", 0, 10)
	if err == nil || !containsStr(err.Error(), "read-only") {
		t.Errorf("insertOne in read-only: err = %v, want a read-only error", err)
	}

	const coll = "picklock_integration_ro"
	mongoDrop(t, cfg, coll)
	mongoSeed(t, cfg, coll, 1)
	defer mongoDrop(t, cfg, coll)

	data, err := ds.Query().Execute(ctx, "db."+coll+".find({})", 0, 10)
	if err != nil || lenDocData(data) != 1 {
		t.Errorf("read query in read-only = docs %d err %v, want 1 row nil err", lenDocData(data), err)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
