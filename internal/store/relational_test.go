package store_test

import (
	"context"
	"testing"

	"github.com/agmonetti/picklock/internal/conn"
	"github.com/agmonetti/picklock/internal/store"
	"github.com/agmonetti/picklock/internal/store/cassandra"
	"github.com/agmonetti/picklock/internal/store/mongo"
	"github.com/agmonetti/picklock/internal/store/neo4j"
	"github.com/agmonetti/picklock/internal/store/redis"
	_ "github.com/agmonetti/picklock/internal/store/sqlite"
)

func queryLimitsForRows(maxRows int) store.QueryLimits {
	limits := store.DefaultQueryLimits()
	limits.MaxRows = maxRows
	return limits
}

func TestExecutorsRejectInvalidLimitsBeforeDatabaseAccess(t *testing.T) {
	executors := []store.QueryExecutor{
		&store.RelationalQueryExecutor{},
		&mongo.MongoExecutor{},
		&redis.RedisExecutor{},
		&cassandra.CassandraExecutor{},
		&neo4j.Neo4jExecutor{},
	}
	invalid := []store.QueryLimits{
		{},
		queryLimitsForRows(-1),
		{MaxRows: 1, MaxCellBytes: 2, MaxResultBytes: 1},
	}
	for _, executor := range executors {
		for _, limits := range invalid {
			if _, err := executor.Execute(context.Background(), "SELECT 1", 0, limits); err == nil {
				t.Errorf("%T accepted invalid limits %+v", executor, limits)
			}
		}
	}
}

func TestNonRelationalBrowseRejectsInvalidAndUnsupportedOptions(t *testing.T) {
	sources := []store.DataSource{
		&mongo.MongoSource{},
		&redis.RedisSource{},
		&cassandra.CassandraSource{},
		&neo4j.Neo4jSource{},
	}
	requests := []store.BrowseRequest{
		{ObjectName: "items", PageSize: 0},
		{ObjectName: "items", PageSize: 1, Page: -1},
		{ObjectName: "items", PageSize: 1, Options: store.BrowseOptions{Columns: []string{"id"}}},
		{ObjectName: "items", PageSize: 1, Options: store.BrowseOptions{Filters: []store.BrowseFilter{{Column: "id", Operator: store.FilterIsNull}}}},
		{ObjectName: "items", PageSize: 1, Options: store.BrowseOptions{PreviewBytes: 64}},
	}
	for _, source := range sources {
		for _, req := range requests {
			if _, err := source.Browse(context.Background(), req); err == nil {
				t.Errorf("%T accepted invalid or unsupported request %+v", source, req)
			}
		}
	}
}

func TestRelationalBrowseUsesLookaheadWithoutExactTotal(t *testing.T) {
	cfg := conn.New(conn.DriverSQLite)
	cfg.Path = ":memory:"
	ds, err := store.New(cfg)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer ds.Close()

	q := ds.Query()
	if _, err := q.Execute(context.Background(), "CREATE TABLE keyless (value TEXT)", 0, store.DefaultQueryLimits()); err != nil {
		t.Fatalf("create table: %v", err)
	}
	for _, value := range []string{"a", "b", "c"} {
		stmt := "INSERT INTO keyless VALUES ('" + value + "')"
		if _, err := q.Execute(context.Background(), stmt, 0, store.DefaultQueryLimits()); err != nil {
			t.Fatalf("insert row: %v", err)
		}
	}

	first, err := ds.Browse(context.Background(), store.BrowseRequest{ObjectName: "keyless", PageSize: 2})
	if err != nil {
		t.Fatalf("browse first page: %v", err)
	}
	firstData := first.Data.(*store.TabularData)
	if len(firstData.Rows) != 2 || firstData.Rows[1][0] != "b" || !first.HasNext {
		t.Fatalf("first page rows=%v hasNext=%v, want [a b]/true", firstData.Rows, first.HasNext)
	}
	if first.TotalCount != -1 || firstData.TotalRows != -1 {
		t.Fatalf("total count=%d total rows=%d, want unknown", first.TotalCount, firstData.TotalRows)
	}

	last, err := ds.Browse(context.Background(), store.BrowseRequest{ObjectName: "keyless", PageSize: 2, Page: 1})
	if err != nil {
		t.Fatalf("browse last page: %v", err)
	}
	lastData := last.Data.(*store.TabularData)
	if len(lastData.Rows) != 1 || lastData.Rows[0][0] != "c" || last.HasNext {
		t.Fatalf("last page rows=%v hasNext=%v, want [c]/false", lastData.Rows, last.HasNext)
	}
}

func TestRelationalBrowseOrdersCompositePrimaryKeyPages(t *testing.T) {
	cfg := conn.New(conn.DriverSQLite)
	cfg.Path = ":memory:"
	ds, err := store.New(cfg)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer ds.Close()

	q := ds.Query()
	for _, stmt := range []string{
		"CREATE TABLE composite_key (a TEXT NOT NULL, b INTEGER NOT NULL, PRIMARY KEY (b, a))",
		"INSERT INTO composite_key VALUES ('same', 2)",
		"INSERT INTO composite_key VALUES ('x', 1)",
		"INSERT INTO composite_key VALUES ('same', 1)",
	} {
		if _, err := q.Execute(context.Background(), stmt, 0, store.DefaultQueryLimits()); err != nil {
			t.Fatalf("execute %q: %v", stmt, err)
		}
	}

	first, err := ds.Browse(context.Background(), store.BrowseRequest{ObjectName: "composite_key", PageSize: 1})
	if err != nil {
		t.Fatalf("browse first page: %v", err)
	}
	firstData := first.Data.(*store.TabularData)
	if len(firstData.Rows) != 1 || firstData.Rows[0][0] != "same" || firstData.Rows[0][1] != "1" || !first.HasNext {
		t.Fatalf("first page rows=%v hasNext=%v, want [(same,1)]/true", firstData.Rows, first.HasNext)
	}

	second, err := ds.Browse(context.Background(), store.BrowseRequest{ObjectName: "composite_key", PageSize: 1, Page: 1})
	if err != nil {
		t.Fatalf("browse second page: %v", err)
	}
	secondData := second.Data.(*store.TabularData)
	if len(secondData.Rows) != 1 || secondData.Rows[0][0] != "same" || secondData.Rows[0][1] != "2" {
		t.Fatalf("second page rows=%v, want [(same,2)]", secondData.Rows)
	}
}

func TestRelationalBrowseOrdersByUniqueNotNullIndex(t *testing.T) {
	cfg := conn.New(conn.DriverSQLite)
	cfg.Path = ":memory:"
	ds, err := store.New(cfg)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer ds.Close()

	q := ds.Query()
	for _, stmt := range []string{
		"CREATE TABLE indexed_key (value TEXT, id INTEGER NOT NULL)",
		"CREATE UNIQUE INDEX indexed_key_id ON indexed_key (id)",
		"INSERT INTO indexed_key VALUES ('same', 2)",
		"INSERT INTO indexed_key VALUES ('same', 1)",
		"INSERT INTO indexed_key VALUES ('other', 3)",
	} {
		if _, err := q.Execute(context.Background(), stmt, 0, store.DefaultQueryLimits()); err != nil {
			t.Fatalf("execute %q: %v", stmt, err)
		}
	}

	first, err := ds.Browse(context.Background(), store.BrowseRequest{ObjectName: "indexed_key", PageSize: 1})
	if err != nil {
		t.Fatalf("browse first page: %v", err)
	}
	firstData := first.Data.(*store.TabularData)
	if len(firstData.Rows) != 1 || firstData.Rows[0][1] != "1" || !first.HasNext {
		t.Fatalf("first page rows=%v hasNext=%v, want id 1/true", firstData.Rows, first.HasNext)
	}
}

func TestRelationalBrowseIgnoresPartialUniqueIndexForOrdering(t *testing.T) {
	cfg := conn.New(conn.DriverSQLite)
	cfg.Path = ":memory:"
	ds, err := store.New(cfg)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer ds.Close()

	q := ds.Query()
	for _, stmt := range []string{
		"CREATE TABLE partial_key (value TEXT, id INTEGER NOT NULL)",
		"CREATE UNIQUE INDEX partial_key_id ON partial_key (id) WHERE id > 0",
		"INSERT INTO partial_key VALUES ('a', 2)",
		"INSERT INTO partial_key VALUES ('z', 1)",
	} {
		if _, err := q.Execute(context.Background(), stmt, 0, store.DefaultQueryLimits()); err != nil {
			t.Fatalf("execute %q: %v", stmt, err)
		}
	}

	first, err := ds.Browse(context.Background(), store.BrowseRequest{ObjectName: "partial_key", PageSize: 1})
	if err != nil {
		t.Fatalf("browse first page: %v", err)
	}
	firstData := first.Data.(*store.TabularData)
	if len(firstData.Rows) != 1 || firstData.Rows[0][1] != "2" {
		t.Fatalf("first page rows=%v, want partial-index-independent fallback row id 2", firstData.Rows)
	}
}

func TestRelationalCatalogClassifiesJoinTables(t *testing.T) {
	cfg := conn.New(conn.DriverSQLite)
	cfg.Path = ":memory:"
	ds, err := store.New(cfg)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer ds.Close()

	exec := func(sql string) {
		t.Helper()
		if _, err := ds.Query().Execute(context.Background(), sql, 0, queryLimitsForRows(100)); err != nil {
			t.Fatalf("execute %q: %v", sql, err)
		}
	}
	exec("PRAGMA foreign_keys = ON")
	exec("CREATE TABLE discounts (id INTEGER PRIMARY KEY)")
	exec("CREATE TABLE products (id INTEGER PRIMARY KEY)")
	exec("CREATE TABLE sellers (id INTEGER PRIMARY KEY)")
	exec("CREATE TABLE discount_products (id_discount INTEGER NOT NULL REFERENCES discounts(id), id_product INTEGER NOT NULL REFERENCES products(id), PRIMARY KEY (id_discount, id_product))")
	exec("CREATE TABLE q9 (discount_ref INTEGER NOT NULL REFERENCES discounts(id), seller_ref INTEGER NOT NULL REFERENCES sellers(id), PRIMARY KEY (discount_ref, seller_ref))")
	exec("CREATE TABLE order_items (id INTEGER PRIMARY KEY, order_id INTEGER REFERENCES discounts(id), product_id INTEGER REFERENCES products(id), quantity INTEGER NOT NULL)")
	items, err := ds.Catalog().ListObjects(context.Background())
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	kinds := make(map[string]store.CatalogItemKind, len(items))
	for _, item := range items {
		kinds[item.Name] = item.Kind
	}
	if got := kinds["discount_products"]; got != store.CatalogItemRelation {
		t.Errorf("discount_products kind = %q, want relation", got)
	}
	if got := kinds["q9"]; got != store.CatalogItemRelation {
		t.Errorf("q9 kind = %q, want relation without name heuristic", got)
	}
	if got := kinds["order_items"]; got != store.CatalogItemTable {
		t.Errorf("order_items kind = %q, want table", got)
	}
	if got := kinds["discounts"]; got != store.CatalogItemTable {
		t.Errorf("discounts kind = %q, want table", got)
	}

	view, err := ds.Inspect(context.Background(), "discounts")
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	structure, ok := view.(*store.RelationalStructure)
	if !ok {
		t.Fatalf("Inspect = %T, want RelationalStructure", view)
	}
	if len(structure.ReferencedBy) != 3 {
		t.Fatalf("ReferencedBy = %d, want 3", len(structure.ReferencedBy))
	}
	if len(structure.ReferencedBy[0].Columns) == 0 || len(structure.ReferencedBy[0].ReferencedColumns) == 0 {
		t.Errorf("ReferencedBy has incomplete columns: %+v", structure.ReferencedBy)
	}
}

func TestRelationalStructurePreservesCompositeForeignKeyOrder(t *testing.T) {
	cfg := conn.New(conn.DriverSQLite)
	cfg.Path = ":memory:"
	ds, err := store.New(cfg)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer ds.Close()

	queries := []string{
		"CREATE TABLE parent (a INTEGER, b INTEGER, PRIMARY KEY (a, b))",
		"CREATE TABLE child (x INTEGER, y INTEGER, FOREIGN KEY (x, y) REFERENCES parent (a, b))",
	}
	for _, query := range queries {
		if _, err := ds.Query().Execute(context.Background(), query, 0, queryLimitsForRows(100)); err != nil {
			t.Fatalf("execute %q: %v", query, err)
		}
	}
	fks, err := ds.Inspect(context.Background(), "child")
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	structure := fks.(*store.RelationalStructure)
	if len(structure.ForeignKeys) != 1 {
		t.Fatalf("ForeignKeys = %d, want 1", len(structure.ForeignKeys))
	}
	fk := structure.ForeignKeys[0]
	if len(fk.Columns) != 2 || fk.Columns[0] != "x" || fk.Columns[1] != "y" {
		t.Errorf("Columns = %v, want [x y]", fk.Columns)
	}
	if len(fk.ReferencedColumns) != 2 || fk.ReferencedColumns[0] != "a" || fk.ReferencedColumns[1] != "b" {
		t.Errorf("ReferencedColumns = %v, want [a b]", fk.ReferencedColumns)
	}
}

func TestSQLiteForeignKeyWithoutReferencedColumn(t *testing.T) {
	cfg := conn.New(conn.DriverSQLite)
	cfg.Path = ":memory:"
	ds, err := store.New(cfg)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer ds.Close()

	for _, query := range []string{
		"CREATE TABLE parent (id INTEGER PRIMARY KEY)",
		"CREATE TABLE child (parent_id INTEGER REFERENCES parent)",
	} {
		if _, err := ds.Query().Execute(context.Background(), query, 0, queryLimitsForRows(100)); err != nil {
			t.Fatalf("execute %q: %v", query, err)
		}
	}
	view, err := ds.Inspect(context.Background(), "child")
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	fks := view.(*store.RelationalStructure).ForeignKeys
	if len(fks) != 1 {
		t.Fatalf("ForeignKeys = %d, want 1", len(fks))
	}
	if fks[0].ReferencedTable != "parent" || len(fks[0].ReferencedColumns) != 1 || fks[0].ReferencedColumns[0] != "" {
		t.Errorf("ForeignKey = %+v, want parent with implicit referenced column", fks[0])
	}
}
