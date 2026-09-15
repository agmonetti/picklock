package screens

import (
	"strings"
	"testing"

	"github.com/agmonetti/picklock/internal/browser"
	"github.com/agmonetti/picklock/internal/store"
)

func TestRenderStructureShowsRelationalReferences(t *testing.T) {
	b := &browser.Browser{
		Structure: &store.RelationalStructure{
			Columns: []store.Column{{Name: "id", Type: "BIGINT", PK: true}},
			ForeignKeys: []store.ForeignKey{{
				Columns:           []string{"id_seller"},
				ReferencedTable:   "sellers",
				ReferencedColumns: []string{"id"},
			}},
			ReferencedBy: []store.ForeignKey{{
				Table:             "discount_products",
				Columns:           []string{"id_discount"},
				ReferencedTable:   "discounts",
				ReferencedColumns: []string{"id"},
			}},
			RelationTable: true,
		},
	}
	out := RenderStructure(b, 100, 30)
	for _, want := range []string{"Relation table", "Foreign Keys", "id_seller → sellers.id", "Referenced By", "discount_products.id_discount → discounts.id"} {
		if !strings.Contains(out, want) {
			t.Errorf("RenderStructure missing %q in %q", want, out)
		}
	}
}

func TestRenderStructureScrolledShowsAllSections(t *testing.T) {
	b := &browser.Browser{
		Structure: &store.RelationalStructure{
			Columns: []store.Column{
				{Name: "id", Type: "BIGINT"},
				{Name: "name", Type: "TEXT"},
				{Name: "email", Type: "TEXT"},
			},
			ForeignKeys: []store.ForeignKey{{
				Columns: []string{"seller_id"}, ReferencedTable: "sellers", ReferencedColumns: []string{"id"},
			}},
			Indexes: []store.Index{{Name: "idx_name", Columns: []string{"name"}}},
		},
	}
	first := RenderStructureScrolled(b, 0, 80, 5)
	if !strings.Contains(first, "↓") || strings.Contains(first, "Foreign Keys") {
		t.Fatalf("first structure viewport = %q", first)
	}
	_, maxScroll := StructureViewport(b, 5)
	seen := first
	for scroll := 1; scroll <= maxScroll; scroll++ {
		seen += "\n" + RenderStructureScrolled(b, scroll, 80, 5)
	}
	for _, want := range []string{"Foreign Keys", "Indexes", "idx_name"} {
		if !strings.Contains(seen, want) {
			t.Errorf("scrolled structure never exposed %q in %q", want, seen)
		}
	}
	last := RenderStructureScrolled(b, maxScroll, 80, 5)
	if strings.Contains(last, "↓") || !strings.Contains(last, "↑") {
		t.Errorf("last structure viewport indicator = %q", last)
	}
}

func TestRenderStructureSupportsDocumentInspection(t *testing.T) {
	b := &browser.Browser{
		Structure: &store.DocumentStructure{
			CollectionName: "users",
			DocCount:       3,
			SampleFields:   []store.FieldSchema{{Name: "email", Type: "string"}},
		},
	}
	out := RenderStructure(b, 80, 10)
	for _, want := range []string{"Collection Statistics", "Documents:", "Inferred Fields", "email"} {
		if !strings.Contains(out, want) {
			t.Errorf("document structure missing %q in %q", want, out)
		}
	}
}
