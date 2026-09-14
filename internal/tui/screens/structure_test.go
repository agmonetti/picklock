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
