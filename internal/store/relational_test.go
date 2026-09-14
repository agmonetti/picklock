package store_test

import (
	"context"
	"testing"

	"github.com/agmonetti/picklock/internal/conn"
	"github.com/agmonetti/picklock/internal/store"
	_ "github.com/agmonetti/picklock/internal/store/sqlite"
)

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
		if _, err := ds.Query().Execute(context.Background(), sql, 0, 100); err != nil {
			t.Fatalf("execute %q: %v", sql, err)
		}
	}
	exec("PRAGMA foreign_keys = ON")
	exec("CREATE TABLE discounts (id INTEGER PRIMARY KEY)")
	exec("CREATE TABLE products (id INTEGER PRIMARY KEY)")
	exec("CREATE TABLE sellers (id INTEGER PRIMARY KEY)")
	exec("CREATE TABLE discount_products (id_discount INTEGER NOT NULL REFERENCES discounts(id), id_product INTEGER NOT NULL REFERENCES products(id), PRIMARY KEY (id_discount, id_product))")
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
	if len(structure.ReferencedBy) != 2 {
		t.Fatalf("ReferencedBy = %d, want 2", len(structure.ReferencedBy))
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
		if _, err := ds.Query().Execute(context.Background(), query, 0, 100); err != nil {
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
		if _, err := ds.Query().Execute(context.Background(), query, 0, 100); err != nil {
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
