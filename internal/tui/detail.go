package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/agmonetti/picklock/internal/browser"
	"github.com/agmonetti/picklock/internal/store"
	"github.com/agmonetti/picklock/internal/tui/screens"
	"github.com/agmonetti/picklock/internal/tui/styles"
)

// handleDetailKeys handles the keys of the detail view.
func (m *Model) handleDetailKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyEsc || key.Matches(msg, m.keys.Back) || msg.String() == "q" || msg.String() == "v":
		m.showDetail = false
		m.detailCursor = 0
		m.detailScroll = 0
		m.detailDoc = ""
		m.detailGraph = nil
		m.detailCols = nil
		m.detailVals = nil
	case key.Matches(msg, m.keys.Up):
		if len(m.detailCols) > 0 {
			m.detailCursor--
			if m.detailCursor < 0 {
				m.detailCursor = 0
			}
			m.adjustDetailScroll()
		} else {
			m.detailScroll--
		}
	case key.Matches(msg, m.keys.Down):
		if len(m.detailCols) > 0 {
			m.detailCursor++
			if m.detailCursor >= len(m.detailCols) {
				m.detailCursor = len(m.detailCols) - 1
			}
			m.adjustDetailScroll()
		} else {
			m.detailScroll++
		}
	case key.Matches(msg, m.keys.PageUp):
		if len(m.detailCols) > 0 {
			m.detailCursor -= 5
			if m.detailCursor < 0 {
				m.detailCursor = 0
			}
			m.adjustDetailScroll()
		} else {
			m.detailScroll -= 10
		}
	case key.Matches(msg, m.keys.PageDown):
		if len(m.detailCols) > 0 {
			m.detailCursor += 5
			if m.detailCursor >= len(m.detailCols) {
				m.detailCursor = len(m.detailCols) - 1
			}
			m.adjustDetailScroll()
		} else {
			m.detailScroll += 10
		}
	case key.Matches(msg, m.keys.First):
		m.detailCursor = 0
		m.detailScroll = 0
	case key.Matches(msg, m.keys.Last):
		if len(m.detailCols) > 0 {
			m.detailCursor = len(m.detailCols) - 1
			m.adjustDetailScroll()
		} else {
			m.detailScroll = 1 << 30
		}
	case msg.String() == "c" || msg.String() == "y" || msg.Type == tea.KeyEnter:
		return m, m.copyDetailValue()
	case msg.String() == "C" || msg.String() == "Y":
		return m, m.copyDetailField()
	case msg.String() == "a" || msg.String() == "A" || key.Matches(msg, m.keys.CopyQuery):
		return m, m.copyDetailAll()
	}
	return m, nil
}

// openDetail shows the full values of a row.
func (m *Model) openDetail(title string, cols, vals []string) {
	m.detailTitle = title
	m.detailCols = cols
	m.detailVals = vals
	m.detailDoc = ""
	m.detailGraph = nil
	m.detailCursor = 0
	m.detailScroll = 0
	m.showDetail = true
}

// openDetailFromBrowser opens the appropriate detail view for the current DataView item.
func (m *Model) openDetailFromBrowser(b *browser.Browser) {
	if b.Data == nil {
		return
	}
	switch v := b.Data.(type) {
	case *store.TabularData:
		if len(v.Rows) > 0 && b.Cursor >= 0 && b.Cursor < len(v.Rows) {
			cols := v.Columns
			if len(cols) == 0 {
				cols = make([]string, len(b.Columns))
				for i, c := range b.Columns {
					cols[i] = c.Name
				}
			}
			m.openDetail(b.ActiveTable, cols, v.Rows[b.Cursor])
		}
	case *store.DocumentData:
		if len(v.Documents) > 0 && b.Cursor >= 0 && b.Cursor < len(v.Documents) {
			doc := v.Documents[b.Cursor]
			m.detailTitle = fmt.Sprintf("Document: %s", doc.ID)
			m.detailDoc = doc.RawJSON
			m.detailCols = nil
			m.detailVals = nil
			m.detailGraph = nil
			m.detailCursor = 0
			m.detailScroll = 0
			m.showDetail = true
		}
	case *store.KeyValueData:
		m.detailTitle = fmt.Sprintf("Key: %s (%s)", v.Key, v.Type)
		var bld strings.Builder
		bld.WriteString(fmt.Sprintf("TTL: %s\n", v.TTL))
		for k, val := range v.Metadata {
			bld.WriteString(fmt.Sprintf("%s: %s\n", k, val))
		}
		bld.WriteString("\nEntries:\n")
		for _, e := range v.Entries {
			extra := ""
			if e.Extra != "" {
				extra = fmt.Sprintf(" [%s]", e.Extra)
			}
			bld.WriteString(fmt.Sprintf("  %s -> %s%s\n", e.Index, e.Value, extra))
		}
		m.detailDoc = bld.String()
		m.detailCols = nil
		m.detailVals = nil
		m.detailGraph = nil
		m.detailCursor = 0
		m.detailScroll = 0
		m.showDetail = true
	case *store.GraphData:
		if len(v.Nodes) > 0 && b.Cursor >= 0 && b.Cursor < len(v.Nodes) {
			node := v.Nodes[b.Cursor]
			m.detailTitle = fmt.Sprintf("Node: %s (:%s)", node.ID, strings.Join(node.Labels, ":"))
			m.detailGraph = &node
			m.detailDoc = ""
			m.detailCols = nil
			m.detailVals = nil
			m.detailCursor = 0
			m.detailScroll = 0
			m.showDetail = true
		}
	}
}

func (m *Model) adjustDetailScroll() {
	if len(m.detailCols) == 0 {
		return
	}
	contentW := m.width - 6
	if contentW < 4 {
		contentW = 4
	}
	contentH := m.height - 4
	if contentH < 4 {
		contentH = 4
	}

	lineOffset := 2 // title + blank line
	for i := 0; i < len(m.detailCols); i++ {
		val := ""
		if i < len(m.detailVals) {
			val = m.detailVals[i]
		}
		wrappedCount := 1
		if val != "" {
			wrappedCount = len(screens.WrapCells(val, contentW-4))
		}
		fieldHeight := 1 + wrappedCount + 1

		if i == m.detailCursor {
			if lineOffset < m.detailScroll {
				m.detailScroll = lineOffset
			} else if lineOffset+fieldHeight > m.detailScroll+contentH {
				m.detailScroll = (lineOffset + fieldHeight) - contentH
			}
			break
		}
		lineOffset += fieldHeight
	}
	if m.detailScroll < 0 {
		m.detailScroll = 0
	}
}

func (m *Model) copyDetailValue() tea.Cmd {
	if len(m.detailCols) > 0 {
		if m.detailCursor >= 0 && m.detailCursor < len(m.detailCols) {
			col := m.detailCols[m.detailCursor]
			val := ""
			if m.detailCursor < len(m.detailVals) {
				val = m.detailVals[m.detailCursor]
			}
			if err := clipboardWriteAll(val); err == nil {
				return m.setSuccess(fmt.Sprintf("copied '%s' value to clipboard", col))
			} else {
				return m.setError("failed to copy: " + err.Error())
			}
		}
		return nil
	}
	if m.detailDoc != "" {
		if err := clipboardWriteAll(m.detailDoc); err == nil {
			return m.setSuccess("document copied to clipboard")
		} else {
			return m.setError("failed to copy: " + err.Error())
		}
	}
	if m.detailGraph != nil {
		var b strings.Builder
		for k, v := range m.detailGraph.Properties {
			b.WriteString(fmt.Sprintf("%s: %s\n", k, v))
		}
		if err := clipboardWriteAll(b.String()); err == nil {
			return m.setSuccess("node details copied to clipboard")
		} else {
			return m.setError("failed to copy: " + err.Error())
		}
	}
	return nil
}

func (m *Model) copyDetailField() tea.Cmd {
	if len(m.detailCols) > 0 {
		if m.detailCursor >= 0 && m.detailCursor < len(m.detailCols) {
			col := m.detailCols[m.detailCursor]
			val := ""
			if m.detailCursor < len(m.detailVals) {
				val = m.detailVals[m.detailCursor]
			}
			text := fmt.Sprintf("%s: %s", col, val)
			if err := clipboardWriteAll(text); err == nil {
				return m.setSuccess(fmt.Sprintf("copied field '%s' to clipboard", col))
			} else {
				return m.setError("failed to copy: " + err.Error())
			}
		}
		return nil
	}
	return m.copyDetailValue()
}

func (m *Model) copyDetailAll() tea.Cmd {
	if len(m.detailCols) > 0 {
		var b strings.Builder
		for i, c := range m.detailCols {
			val := ""
			if i < len(m.detailVals) {
				val = m.detailVals[i]
			}
			b.WriteString(fmt.Sprintf("%s: %s\n", c, val))
		}
		if err := clipboardWriteAll(strings.TrimSpace(b.String())); err == nil {
			return m.setSuccess("all fields copied to clipboard")
		} else {
			return m.setError("failed to copy: " + err.Error())
		}
	}
	if m.detailDoc != "" {
		if err := clipboardWriteAll(m.detailDoc); err == nil {
			return m.setSuccess("document copied to clipboard")
		} else {
			return m.setError("failed to copy: " + err.Error())
		}
	}
	if m.detailGraph != nil {
		var b strings.Builder
		b.WriteString(fmt.Sprintf("ID: %s\nLabels: %s\n", m.detailGraph.ID, strings.Join(m.detailGraph.Labels, ":")))
		for k, v := range m.detailGraph.Properties {
			b.WriteString(fmt.Sprintf("%s: %s\n", k, v))
		}
		if err := clipboardWriteAll(strings.TrimSpace(b.String())); err == nil {
			return m.setSuccess("node details copied to clipboard")
		} else {
			return m.setError("failed to copy: " + err.Error())
		}
	}
	return nil
}

// renderActiveDetail renders the active detail view.
func (m *Model) renderActiveDetail(width, height int) string {
	if m.detailDoc != "" {
		return renderTextDetail(m.detailTitle, m.detailDoc, m.detailScroll, width, height)
	}
	if m.detailGraph != nil {
		return renderGraphNodeDetail(m.detailTitle, m.detailGraph, m.detailScroll, width, height)
	}
	return screens.RenderRowDetail(m.detailTitle, m.detailCols, m.detailVals, m.detailCursor, m.detailScroll, width, height)
}

func renderTextDetail(title, text string, scroll, width, height int) string {
	var b strings.Builder
	b.WriteString(styles.StyleHeader.Render(title) + "\n\n")
	lines := strings.Split(text, "\n")
	if scroll < 0 {
		scroll = 0
	}
	if scroll > len(lines) {
		scroll = len(lines)
	}
	end := scroll + height - 2
	if end > len(lines) {
		end = len(lines)
	}
	b.WriteString(strings.Join(lines[scroll:end], "\n"))
	return b.String()
}

func renderGraphNodeDetail(title string, node *store.GraphNode, scroll, width, height int) string {
	var lines []string
	lines = append(lines, styles.StyleHeader.Render(title), "")
	lines = append(lines, styles.StyleColHeader.Render("Properties:"))
	if len(node.Properties) == 0 {
		lines = append(lines, styles.StyleHeaderDim.Render("  (no properties)"))
	}
	for k, v := range node.Properties {
		lines = append(lines, fmt.Sprintf("  %-20s %s", k+":", v))
	}
	lines = append(lines, "", styles.StyleColHeader.Render("Relationships:"))
	if len(node.Incident) == 0 {
		lines = append(lines, styles.StyleHeaderDim.Render("  (no incident edges)"))
	}
	for _, inc := range node.Incident {
		lines = append(lines, fmt.Sprintf("  %s %s %s (%s)", inc.Direction, inc.Type, inc.TargetID, inc.TargetSummary))
	}

	if scroll < 0 {
		scroll = 0
	}
	if scroll > len(lines) {
		scroll = len(lines)
	}
	end := scroll + height
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[scroll:end], "\n")
}
