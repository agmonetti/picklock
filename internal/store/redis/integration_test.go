package redis

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/agmonetti/picklock/internal/conn"
	"github.com/agmonetti/picklock/internal/store"
)

func redisEnvCfg(t *testing.T) conn.ConnectionConfig {
	t.Helper()
	host := os.Getenv("PICKLOCK_TEST_REDIS_HOST")
	if host == "" {
		t.Skipf("env PICKLOCK_TEST_REDIS_HOST not set; skipping integration test")
	}
	port := 6379
	if p := os.Getenv("PICKLOCK_TEST_REDIS_PORT"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			port = n
		}
	}
	db := os.Getenv("PICKLOCK_TEST_REDIS_DATABASE")
	if db == "" {
		db = "0"
	}
	return conn.ConnectionConfig{
		Driver:   conn.DriverRedis,
		Host:     host,
		Port:     port,
		Password: os.Getenv("PICKLOCK_TEST_REDIS_PASSWORD"),
		Database: db,
	}
}

// TestIntegration exercises the RedisSource against a real server, including
// scan-cursor pagination (the previous code replayed page 1 forever).
func TestIntegration(t *testing.T) {
	cfg := redisEnvCfg(t)
	client := goredis.NewClient(&goredis.Options{
		Addr: cfg.Host + ":" + strconv.Itoa(cfg.Port),
		DB:   0,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("cannot reach redis: %v", err)
	}
	_ = client.FlushDB(ctx).Err()
	defer client.FlushDB(ctx)

	for i := 1; i <= 250; i++ {
		if err := client.HSet(ctx, "it:hash", map[string]any{"f" + strconv.Itoa(i): "v" + strconv.Itoa(i)}).Err(); err != nil {
			t.Fatalf("hset: %v", err)
		}
	}
	client.Set(ctx, "it:str", "hello-world", 0)
	client.RPush(ctx, "it:list", "a", "b", "c", "d", "e", "f")
	client.SAdd(ctx, "it:set", "m1", "m2", "m3", "m4", "m5")
	client.ZAdd(ctx, "it:zset", goredis.Z{Score: 1, Member: "a"}, goredis.Z{Score: 2, Member: "b"}, goredis.Z{Score: 3, Member: "c"})

	ds, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer ds.Close()

	if v, err := ds.Version(ctx); err != nil || v == "" {
		t.Errorf("Version = %q err=%v", v, err)
	}

	// Catalog lists the keys with type badges
	items, err := ds.Catalog().ListObjects(ctx)
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	if !redisHasKey(items, "it:hash") {
		t.Fatalf("catalog missing it:hash: %v", items)
	}

	// String browse
	resp, err := ds.Browse(ctx, store.BrowseRequest{ObjectName: "it:str", PageSize: 50})
	if err != nil {
		t.Fatalf("Browse string: %v", err)
	}
	kv, ok := resp.Data.(*store.KeyValueData)
	if !ok || kv.Type != "string" || len(kv.Entries) != 1 || kv.Entries[0].Value != "hello-world" {
		t.Errorf("Browse string = %T %+v", resp.Data, resp.Data)
	}

	// Hash scan-cursor pagination: paging through 250 fields must eventually
	// see every field exactly once (the previous code replayed page 1 forever).
	seen := map[string]bool{}
	cur := ""
	pages := 0
	for {
		resp, err := ds.Browse(ctx, store.BrowseRequest{ObjectName: "it:hash", PageSize: 50, Cursor: cur})
		if err != nil {
			t.Fatalf("Browse hash page %d: %v", pages, err)
		}
		for _, f := range redisFields(resp) {
			if seen[f] {
				t.Errorf("hash scan replayed field %q", f)
			}
			seen[f] = true
		}
		if resp.NextCursor == "" {
			break
		}
		if resp.NextCursor == cur {
			t.Fatalf("hash scan cursor did not advance (stuck at %q)", cur)
		}
		cur = resp.NextCursor
		pages++
		if pages > 20 {
			t.Fatalf("hash scan did not terminate after 20 pages")
		}
	}
	if len(seen) != 250 {
		t.Errorf("hash scan saw %d unique fields, want 250", len(seen))
	}

	// zset entries carry a member and a score (index is a plain rank)
	zresp, err := ds.Browse(ctx, store.BrowseRequest{ObjectName: "it:zset", PageSize: 50})
	if err != nil {
		t.Fatalf("Browse zset: %v", err)
	}
	zdata, ok := zresp.Data.(*store.KeyValueData)
	if !ok || len(zdata.Entries) != 3 {
		t.Fatalf("zset entries = %T len %d want 3", zresp.Data, len(zdata.Entries))
	}
	if zdata.Entries[0].Value == zdata.Entries[0].Index {
		t.Errorf("zset entry index duplicates the member: %+v", zdata.Entries[0])
	}

	// Inspect reports type, length and memory usage
	insp, err := ds.Inspect(ctx, "it:hash")
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	ks, ok := insp.(*store.KeyValueStructure)
	if !ok || ks.Type != "hash" || ks.Length != 250 {
		t.Errorf("Inspect = %T len=%d type=%q, want hash/250", insp, lenOf(insp), typeOf(insp))
	}

	// Editor queries
	exec := ds.Query()
	data, err := exec.Execute(ctx, "GET it:str", 0, 10)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	if tab, ok := data.(*store.TabularData); !ok || tab.Rows[0][0] != "hello-world" {
		t.Errorf("GET = %v, want hello-world", data)
	}

	data, err = exec.Execute(ctx, "HGETALL it:hash", 0, 10)
	if err != nil {
		t.Fatalf("HGETALL: %v", err)
	}
	if hk, ok := data.(*store.KeyValueData); !ok || len(hk.Entries) != 250 {
		t.Errorf("HGETALL = %T with %d entries, want 250", data, hashLenOf(data))
	}
}

func redisHasKey(items []store.CatalogItem, name string) bool {
	for _, it := range items {
		if it.Name == name {
			return true
		}
	}
	return false
}

func redisFields(resp store.BrowseResponse) []string {
	kv, ok := resp.Data.(*store.KeyValueData)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(kv.Entries))
	for _, e := range kv.Entries {
		out = append(out, e.Index)
	}
	return out
}

func lenOf(v store.InspectionView) int64 {
	if ks, ok := v.(*store.KeyValueStructure); ok {
		return ks.Length
	}
	return -1
}

func typeOf(v store.InspectionView) string {
	if ks, ok := v.(*store.KeyValueStructure); ok {
		return ks.Type
	}
	return ""
}

func hashLenOf(v store.DataView) int {
	if kv, ok := v.(*store.KeyValueData); ok {
		return len(kv.Entries)
	}
	return -1
}

// TestIntegrationReadOnly blocks writes but allows reads.
func TestIntegrationReadOnly(t *testing.T) {
	cfg := redisEnvCfg(t)
	cfg.ReadOnly = true

	ds, err := New(cfg)
	if err != nil {
		t.Fatalf("New (read-only): %v", err)
	}
	defer ds.Close()

	ctx := context.Background()
	if _, err := ds.Query().Execute(ctx, "SET it:ro blocked", 0, 10); err == nil {
		t.Error("SET in read-only must be blocked")
	}
	if _, err := ds.Query().Execute(ctx, "GET it:str", 0, 10); err != nil {
		t.Errorf("GET in read-only failed: %v", err)
	}
}
