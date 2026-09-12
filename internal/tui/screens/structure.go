package screens

import (
	"fmt"
	"strings"

	"github.com/agmonetti/picklock/internal/browser"
	"github.com/agmonetti/picklock/internal/store"
	"github.com/agmonetti/picklock/internal/tui/styles"
)

// RenderStructure renders the structural description of the active item.
func RenderStructure(b *browser.Browser, width, height int) string {
	if b == nil {
		return styles.StyleHeaderDim.Render("no connection")
	}

	var sb strings.Builder

	if b.Structure != nil {
		switch s := b.Structure.(type) {
		case *store.RelationalStructure:
			renderRelationalStructure(&sb, s.Columns, s.Indexes)
		case *store.DocumentStructure:
			renderDocumentStructure(&sb, s)
		case *store.KeyValueStructure:
			renderKeyValueStructure(&sb, s)
		case *store.GraphStructure:
			renderGraphStructure(&sb, s)
		}
	} else if len(b.Columns) > 0 {
		renderRelationalStructure(&sb, b.Columns, b.Indexes)
	} else {
		sb.WriteString(styles.StyleHeaderDim.Render("no structure available for this item"))
	}

	lines := strings.Split(sb.String(), "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

func renderRelationalStructure(sb *strings.Builder, cols []store.Column, indexes []store.Index) {
	sb.WriteString(styles.StyleHeader.Render("Columns") + "\n")
	for _, c := range cols {
		var flags []string
		if c.PK {
			flags = append(flags, "PK")
		}
		if c.Clustering {
			flags = append(flags, "CC")
		}
		if c.NotNull {
			flags = append(flags, "NN")
		}
		def := ""
		if c.Default != "" {
			def = fmt.Sprintf(" DEF %s", c.Default)
		}
		line := fmt.Sprintf("  %-24s %-12s %s%s", c.Name, c.Type, strings.Join(flags, " "), def)
		sb.WriteString(styles.StyleSidebarItem.Render(line) + "\n")
	}

	sb.WriteString("\n" + styles.StyleHeader.Render("Indexes") + "\n")
	if len(indexes) == 0 {
		sb.WriteString(styles.StyleHeaderDim.Render("  no indexes") + "\n")
	}
	for _, ix := range indexes {
		uniq := ""
		if ix.Unique {
			uniq = " UNIQUE"
		}
		line := fmt.Sprintf("  %-24s (%s)%s", ix.Name, strings.Join(ix.Columns, ", "), uniq)
		sb.WriteString(styles.StyleSidebarItem.Render(line) + "\n")
	}
}

func renderDocumentStructure(sb *strings.Builder, s *store.DocumentStructure) {
	sb.WriteString(styles.StyleHeader.Render("Collection Statistics") + "\n")
	sb.WriteString(styles.StyleSidebarItem.Render(fmt.Sprintf("  %-20s %d", "Documents:", s.DocCount)) + "\n")
	sb.WriteString(styles.StyleSidebarItem.Render(fmt.Sprintf("  %-20s %s", "Storage Size:", formatBytes(s.TotalSize))) + "\n")
	sb.WriteString(styles.StyleSidebarItem.Render(fmt.Sprintf("  %-20s %s", "Avg Doc Size:", formatBytes(s.AvgSize))) + "\n")
	sb.WriteString(styles.StyleSidebarItem.Render(fmt.Sprintf("  %-20s %s", "Index Size:", formatBytes(s.IndexSize))) + "\n")

	if len(s.SampleFields) > 0 {
		sb.WriteString("\n" + styles.StyleHeader.Render("Inferred Fields") + "\n")
		for _, f := range s.SampleFields {
			sb.WriteString(styles.StyleSidebarItem.Render(fmt.Sprintf("  %-24s %s", f.Name, f.Type)) + "\n")
		}
	}

	sb.WriteString("\n" + styles.StyleHeader.Render("Indexes") + "\n")
	if len(s.Indexes) == 0 {
		sb.WriteString(styles.StyleHeaderDim.Render("  no indexes") + "\n")
	}
	for _, ix := range s.Indexes {
		uniq := ""
		if ix.Unique {
			uniq = " UNIQUE"
		}
		sb.WriteString(styles.StyleSidebarItem.Render(fmt.Sprintf("  %-24s (%s)%s", ix.Name, strings.Join(ix.Columns, ", "), uniq)) + "\n")
	}
}

func renderKeyValueStructure(sb *strings.Builder, s *store.KeyValueStructure) {
	sb.WriteString(styles.StyleHeader.Render("Key Information") + "\n")
	sb.WriteString(styles.StyleSidebarItem.Render(fmt.Sprintf("  %-20s %s", "Key:", s.Key)) + "\n")
	sb.WriteString(styles.StyleSidebarItem.Render(fmt.Sprintf("  %-20s %s", "Type:", s.Type)) + "\n")
	sb.WriteString(styles.StyleSidebarItem.Render(fmt.Sprintf("  %-20s %s", "TTL:", s.TTL)) + "\n")
	sb.WriteString(styles.StyleSidebarItem.Render(fmt.Sprintf("  %-20s %s", "Encoding:", s.Encoding)) + "\n")
	sb.WriteString(styles.StyleSidebarItem.Render(fmt.Sprintf("  %-20s %s", "Memory Usage:", formatBytes(s.MemUsage))) + "\n")
	sb.WriteString(styles.StyleSidebarItem.Render(fmt.Sprintf("  %-20s %d", "Length / Size:", s.Length)) + "\n")

	if len(s.ServerInfo) > 0 {
		sb.WriteString("\n" + styles.StyleHeader.Render("Server Overview") + "\n")
		for k, v := range s.ServerInfo {
			sb.WriteString(styles.StyleSidebarItem.Render(fmt.Sprintf("  %-20s %s", k+":", v)) + "\n")
		}
	}
}

func renderGraphStructure(sb *strings.Builder, s *store.GraphStructure) {
	sb.WriteString(styles.StyleHeader.Render("Node Label Schema") + "\n")
	sb.WriteString(styles.StyleSidebarItem.Render(fmt.Sprintf("  %-20s %s", "Label:", s.LabelName)) + "\n")

	if len(s.Properties) > 0 {
		sb.WriteString("\n" + styles.StyleHeader.Render("Properties") + "\n")
		for _, p := range s.Properties {
			sb.WriteString(styles.StyleSidebarItem.Render(fmt.Sprintf("  %-24s %s", p.Name, p.Type)) + "\n")
		}
	}

	if len(s.Constraints) > 0 {
		sb.WriteString("\n" + styles.StyleHeader.Render("Constraints") + "\n")
		for _, c := range s.Constraints {
			sb.WriteString(styles.StyleSidebarItem.Render("  "+c) + "\n")
		}
	}

	if len(s.Indexes) > 0 {
		sb.WriteString("\n" + styles.StyleHeader.Render("Indexes") + "\n")
		for _, ix := range s.Indexes {
			sb.WriteString(styles.StyleSidebarItem.Render(fmt.Sprintf("  %-24s (%s)", ix.Name, strings.Join(ix.Columns, ", "))) + "\n")
		}
	}

	if len(s.Relationships) > 0 {
		sb.WriteString("\n" + styles.StyleHeader.Render("Incident Relationship Types") + "\n")
		for _, r := range s.Relationships {
			sb.WriteString(styles.StyleSidebarItem.Render("  [:"+r+"]") + "\n")
		}
	}
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
