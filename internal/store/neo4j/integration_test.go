package neo4j

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/agmonetti/picklock/internal/conn"
	"github.com/agmonetti/picklock/internal/store"
)

const itLabel = "PicklockItTest"

func neoEnvCfg(t *testing.T) conn.ConnectionConfig {
	t.Helper()
	host := os.Getenv("PICKLOCK_TEST_NEO4J_HOST")
	if host == "" {
		t.Skipf("env PICKLOCK_TEST_NEO4J_HOST not set; skipping integration test")
	}
	port := 7687
	if p := os.Getenv("PICKLOCK_TEST_NEO4J_PORT"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			port = n
		}
	}
	db := os.Getenv("PICKLOCK_TEST_NEO4J_DATABASE")
	if db == "" {
		db = "neo4j"
	}
	return conn.ConnectionConfig{
		Driver:   conn.DriverNeo4j,
		Host:     host,
		Port:     port,
		User:     os.Getenv("PICKLOCK_TEST_NEO4J_USER"),
		Password: os.Getenv("PICKLOCK_TEST_NEO4J_PASSWORD"),
		Database: db,
	}
}

// TestIntegration exercises the Neo4jSource against a real server, including
// the query result cap and label browsing.
func TestIntegration(t *testing.T) {
	cfg := neoEnvCfg(t)

	ds, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer ds.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Clean any previous run, then seed 7 nodes under a dedicated label.
	execSeed(t, ds, cfg, "MATCH (n:"+itLabel+") DETACH DELETE n")
	for i := 0; i < 7; i++ {
		execSeed(t, ds, cfg, "CREATE (n:"+itLabel+" {name: 'n"+strconv.Itoa(i)+"'})")
	}
	t.Cleanup(func() {
		cds, err := New(cfg)
		if err != nil {
			return
		}
		defer cds.Close()
		_, _ = cds.Query().Execute(context.Background(), "MATCH (n:"+itLabel+") DETACH DELETE n", 0, 100)
	})

	if v, err := ds.Version(ctx); err != nil || v == "" {
		t.Errorf("Version = %q err=%v", v, err)
	}

	// Catalog lists the label
	items, err := ds.Catalog().ListObjects(ctx)
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	if !neoHasLabel(items, itLabel) {
		t.Fatalf("catalog missing %s: %v", itLabel, items)
	}

	// Browse page 0 (pageSize 3) -> 3 nodes, page 1 -> 3, page 2 -> 1 (last).
	// Neo4j pages by offset, so the Page field drives the skip.
	resp, err := ds.Browse(ctx, store.BrowseRequest{ObjectName: itLabel, PageSize: 3, Page: 0})
	if err != nil {
		t.Fatalf("Browse page 0: %v", err)
	}
	g, ok := resp.Data.(*store.GraphData)
	if !ok || len(g.Nodes) != 3 {
		t.Fatalf("page 0 = %T (%d nodes), want 3", resp.Data, neoNodes(resp.Data))
	}
	if !resp.HasNext || resp.TotalCount != 7 {
		t.Errorf("page 0 HasNext=%v Total=%d, want true/7", resp.HasNext, resp.TotalCount)
	}

	resp1, err := ds.Browse(ctx, store.BrowseRequest{ObjectName: itLabel, PageSize: 3, Page: 1, Cursor: resp.NextCursor})
	if err != nil {
		t.Fatalf("Browse page 1: %v", err)
	}
	if neoNodes(resp1.Data) != 3 {
		t.Fatalf("page 1 nodes = %d, want 3", neoNodes(resp1.Data))
	}
	resp2, err := ds.Browse(ctx, store.BrowseRequest{ObjectName: itLabel, PageSize: 3, Page: 2, Cursor: resp1.NextCursor})
	if err != nil {
		t.Fatalf("Browse page 2: %v", err)
	}
	if neoNodes(resp2.Data) != 1 || resp2.HasNext {
		t.Errorf("page 2 nodes = %d HasNext=%v, want 1/false", neoNodes(resp2.Data), resp2.HasNext)
	}

	// Inspect exposes the label schema and relationships
	insp, err := ds.Inspect(ctx, itLabel)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	gs, ok := insp.(*store.GraphStructure)
	if !ok || gs.LabelName != itLabel {
		t.Errorf("Inspect = %T, want GraphStructure for %s", insp, itLabel)
	}

	// Editor queries: MATCH ... RETURN n materializes nodes
	exec := ds.Query()
	data, err := exec.Execute(ctx, "MATCH (n:"+itLabel+") RETURN n", 0, 100)
	if err != nil {
		t.Fatalf("MATCH RETURN n: %v", err)
	}
	if newNodeGraph(data).len() != 7 {
		t.Errorf("MATCH RETURN n => %T, want 7 nodes", data)
	}

	// The result cap stops the fetch at maxRows (returns 2 nodes, not 7).
	data, err = exec.Execute(ctx, "MATCH (n:"+itLabel+") RETURN n", 0, 2)
	if err != nil {
		t.Fatalf("capped MATCH: %v", err)
	}
	if n := neoNodes(data); n != 2 {
		t.Errorf("capped MATCH materialized %d nodes, want 2", n)
	}

	// Aggregate/scalar queries render as tabular data
	data, err = exec.Execute(ctx, "MATCH (n:"+itLabel+") RETURN count(n) AS total", 0, 100)
	if err != nil {
		t.Fatalf("RETURN count: %v", err)
	}
	if tab, ok := data.(*store.TabularData); !ok || len(tab.Rows) != 1 || tab.Rows[0][0] != "7" {
		t.Errorf("count query = %v, want 7", data)
	}

	// False-positive regression: a read that merely contains 'CREATE' in a
	// string literal must run.
	data, err = exec.Execute(ctx, "MATCH (n:"+itLabel+") WHERE n.name='CREATE' RETURN n", 0, 10)
	if err != nil {
		t.Errorf("read with mutation word in string: %v", err)
	}

	// Mutation queries still execute and report affected counters
	data, err = exec.Execute(ctx, "CREATE (n:"+itLabel+" {name: 'temp'})", 0, 100)
	if err != nil {
		t.Fatalf("CREATE: %v", err)
	}
	if tab, ok := data.(*store.TabularData); !ok || tab.Affected != 1 {
		t.Errorf("CREATE affected = %v, want 1", data)
	}
	execSeed(t, ds, cfg, "MATCH (n:"+itLabel+" {name: 'temp'}) DELETE n")
}

// execSeed is a tiny seam so the harness can run setup/teardown writes that
// the DataSource itself classifies as mutations.
func execSeed(t *testing.T, ds store.DataSource, cfg conn.ConnectionConfig, q string) {
	t.Helper()
	if _, err := ds.Query().Execute(context.Background(), q, 0, 100); err != nil {
		t.Fatalf("seed %q: %v", q, err)
	}
}

func neoHasLabel(items []store.CatalogItem, label string) bool {
	for _, it := range items {
		if it.Name == label {
			return true
		}
	}
	return false
}

func neoNodes(v store.DataView) int {
	if g, ok := v.(*store.GraphData); ok {
		return len(g.Nodes)
	}
	return -1
}

func neoRows(v store.DataView) int {
	graphNodes := neoNodes(v)
	if graphNodes >= 0 {
		return graphNodes
	}
	if tab, ok := v.(*store.TabularData); ok {
		return len(tab.Rows)
	}
	return -1
}

func neoTrunc(v store.DataView) bool {
	if tab, ok := v.(*store.TabularData); ok {
		return tab.Truncated
	}
	return false
}

type nodeGraph struct{ n int }

func newNodeGraph(v store.DataView) nodeGraph {
	return nodeGraph{n: neoNodes(v)}
}

func (g nodeGraph) len() int { return g.n }

// TestIntegrationReadOnly blocks writes and allows reads.
func TestIntegrationReadOnly(t *testing.T) {
	cfg := neoEnvCfg(t)
	cfg.ReadOnly = true

	ds, err := New(cfg)
	if err != nil {
		t.Fatalf("New (read-only): %v", err)
	}
	defer ds.Close()

	ctx := context.Background()
	if _, err := ds.Query().Execute(ctx, "CREATE (n:PicklockRo {x: 1})", 0, 10); err == nil {
		t.Error("CREATE in read-only must be blocked")
	}
	if _, err := ds.Query().Execute(ctx, "MATCH (n) RETURN count(n) LIMIT 1", 0, 10); err != nil {
		t.Errorf("read in read-only failed: %v", err)
	}
}
