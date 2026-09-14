package mcp

import (
	"fmt"
	"strings"

	"github.com/agmonetti/picklock/internal/store"
)

// formatDataView renders any DataView as human-readable text for MCP tool results.
func formatDataView(dv store.DataView, hasNext bool, totalCount int64, page int) string {
	if dv == nil {
		return "(no data)"
	}

	var b strings.Builder

	switch v := dv.(type) {
	case *store.TabularData:
		formatTabular(&b, v)
	case *store.DocumentData:
		formatDocuments(&b, v)
	case *store.KeyValueData:
		formatKeyValue(&b, v)
	case *store.GraphData:
		formatGraph(&b, v)
	case *store.RawTextData:
		if v.Title != "" {
			fmt.Fprintf(&b, "# %s\n", v.Title)
		}
		b.WriteString(v.Text)
	default:
		fmt.Fprintf(&b, "%s", dv.Summary())
	}

	// Pagination footer
	if hasNext || page > 0 {
		fmt.Fprintf(&b, "\n--- Page %d", page)
		if totalCount >= 0 {
			fmt.Fprintf(&b, " | Total: %d", totalCount)
		}
		if hasNext {
			b.WriteString(" | More pages available")
		}
		b.WriteString(" ---")
	}

	return b.String()
}

func formatTabular(b *strings.Builder, t *store.TabularData) {
	if t.Affected >= 0 {
		fmt.Fprintf(b, "%s\n", t.Summary())
		return
	}
	if len(t.Rows) == 0 {
		b.WriteString("(empty result set)")
		return
	}

	// Header
	b.WriteString(strings.Join(t.Columns, " | "))
	b.WriteByte('\n')
	for i, col := range t.Columns {
		if i > 0 {
			b.WriteString("-+-")
		}
		b.WriteString(strings.Repeat("-", len(col)))
	}
	b.WriteByte('\n')

	// Rows
	for ri, row := range t.Rows {
		for ci, cell := range row {
			if ci > 0 {
				b.WriteString(" | ")
			}
			if t.Nulls != nil && ri < len(t.Nulls) && ci < len(t.Nulls[ri]) && t.Nulls[ri][ci] {
				b.WriteString("NULL")
			} else {
				b.WriteString(cell)
			}
		}
		b.WriteByte('\n')
	}

	if t.Truncated {
		b.WriteString("... (truncated)\n")
	}
}

func formatDocuments(b *strings.Builder, d *store.DocumentData) {
	if len(d.Documents) == 0 {
		b.WriteString("(no documents)")
		return
	}
	for i, doc := range d.Documents {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(b, "--- %s ---\n", doc.ID)
		b.WriteString(doc.RawJSON)
		b.WriteByte('\n')
	}
}

func formatKeyValue(b *strings.Builder, k *store.KeyValueData) {
	fmt.Fprintf(b, "Key: %s  Type: %s  TTL: %s\n", k.Key, k.Type, k.TTL)
	if k.Type == "string" && len(k.Entries) > 0 {
		b.WriteString(k.Entries[0].Value)
		b.WriteByte('\n')
		return
	}
	for _, e := range k.Entries {
		fmt.Fprintf(b, "  %s: %s", e.Index, e.Value)
		if e.Extra != "" {
			fmt.Fprintf(b, " (%s)", e.Extra)
		}
		b.WriteByte('\n')
	}
}

func formatGraph(b *strings.Builder, g *store.GraphData) {
	if len(g.Nodes) == 0 && len(g.Edges) == 0 {
		b.WriteString("(no graph data)")
		return
	}
	for i, n := range g.Nodes {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(b, "(%s) %s\n", n.ID, strings.Join(n.Labels, ":"))
		for k, v := range n.Properties {
			fmt.Fprintf(b, "  %s: %s\n", k, v)
		}
		for _, e := range n.Incident {
			fmt.Fprintf(b, "  %s[%s]%s %s\n", e.Direction, e.Type, e.Direction, e.TargetSummary)
		}
	}
}

// formatInspection renders an InspectionView as text.
func formatInspection(iv store.InspectionView) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", iv.Title())

	switch v := iv.(type) {
	case *store.RelationalStructure:
		b.WriteString("Columns:\n")
		for _, c := range v.Columns {
			flags := ""
			if c.PK {
				flags += " PK"
			}
			if c.NotNull {
				flags += " NOT NULL"
			}
			if c.Clustering {
				flags += " CC"
			}
			def := ""
			if c.Default != "" {
				def = fmt.Sprintf(" DEFAULT %s", c.Default)
			}
			fmt.Fprintf(&b, "  %-30s %s%s%s\n", c.Name, c.Type, flags, def)
		}
		if len(v.ForeignKeys) > 0 {
			b.WriteString("\nForeign keys:\n")
			for _, fk := range v.ForeignKeys {
				fmt.Fprintf(&b, "  %s -> %s.%s\n", strings.Join(fk.Columns, ", "), fk.ReferencedTable, strings.Join(fk.ReferencedColumns, ", "))
			}
		}
		if len(v.ReferencedBy) > 0 {
			b.WriteString("\nReferenced by:\n")
			for _, fk := range v.ReferencedBy {
				fmt.Fprintf(&b, "  %s.%s -> %s.%s\n",
					fk.Table, strings.Join(fk.Columns, ", "),
					fk.ReferencedTable, strings.Join(fk.ReferencedColumns, ", "))
			}
		}

	case *store.DocumentStructure:
		fmt.Fprintf(&b, "Documents: %d  Avg size: %d B  Total: %d B  Index size: %d B\n",
			v.DocCount, v.AvgSize, v.TotalSize, v.IndexSize)
		if len(v.SampleFields) > 0 {
			b.WriteString("\nInferred fields:\n")
			for _, f := range v.SampleFields {
				fmt.Fprintf(&b, "  %-30s %s\n", f.Name, f.Type)
			}
		}
		if len(v.Indexes) > 0 {
			b.WriteString("\nIndexes:\n")
			for _, idx := range v.Indexes {
				fmt.Fprintf(&b, "  %s (%s)\n", idx.Name, strings.Join(idx.Columns, ", "))
			}
		}

	case *store.KeyValueStructure:
		fmt.Fprintf(&b, "Type: %s  TTL: %s  Encoding: %s\n", v.Type, v.TTL, v.Encoding)
		fmt.Fprintf(&b, "Memory: %d B  Length: %d\n", v.MemUsage, v.Length)
		if len(v.ServerInfo) > 0 {
			b.WriteString("\nServer info:\n")
			for k, val := range v.ServerInfo {
				fmt.Fprintf(&b, "  %s: %s\n", k, val)
			}
		}

	case *store.GraphStructure:
		if len(v.Properties) > 0 {
			b.WriteString("Properties:\n")
			for _, p := range v.Properties {
				fmt.Fprintf(&b, "  %-30s %s\n", p.Name, p.Type)
			}
		}
		if len(v.Constraints) > 0 {
			b.WriteString("\nConstraints:\n")
			for _, c := range v.Constraints {
				fmt.Fprintf(&b, "  %s\n", c)
			}
		}
		if len(v.Indexes) > 0 {
			b.WriteString("\nIndexes:\n")
			for _, idx := range v.Indexes {
				fmt.Fprintf(&b, "  %s (%s)\n", idx.Name, strings.Join(idx.Columns, ", "))
			}
		}
		if len(v.Relationships) > 0 {
			b.WriteString("\nRelationships:\n")
			for _, r := range v.Relationships {
				fmt.Fprintf(&b, "  %s\n", r)
			}
		}

	default:
		fmt.Fprintf(&b, "%s\n", iv.Title())
	}

	return b.String()
}
