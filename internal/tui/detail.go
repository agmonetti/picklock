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
		m.detailNulls = nil
		m.detailIncomplete = nil
		m.detailItemIncomplete = nil
		m.detailIncompleteReason = ""
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

// openDetail borrows the selected values without materializing presentation text.
func (m *Model) openDetail(title string, cols, vals []string) {
	m.detailTitle = title
	m.detailCols = cols
	m.detailVals = vals
	m.detailDoc = ""
	m.detailGraph = nil
	m.detailNulls = nil
	m.detailIncomplete = nil
	m.detailItemIncomplete = nil
	m.detailIncompleteReason = ""
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
		if b.Cursor < 0 || b.Cursor >= len(v.Rows) {
			return
		}
		cols := v.Columns
		if len(cols) == 0 {
			cols = make([]string, len(b.Columns))
			for i, c := range b.Columns {
				cols[i] = c.Name
			}
		}
		m.openDetail(b.ActiveTable, cols, v.Rows[b.Cursor])
		if b.Cursor < len(v.Nulls) {
			m.detailNulls = v.Nulls[b.Cursor]
		}
		for col := range cols {
			if cell, ok := v.CellIncomplete(b.Cursor, col); ok {
				if m.detailIncomplete == nil {
					m.detailIncomplete = make(map[int]store.IncompleteCell)
				}
				m.detailIncomplete[col] = cell
			}
		}
	case *store.DocumentData:
		if b.Cursor < 0 || b.Cursor >= len(v.Documents) {
			return
		}
		doc := &v.Documents[b.Cursor]
		m.openDetail(fmt.Sprintf("Document: %s", doc.ID), nil, nil)
		m.detailDoc, m.detailItemIncomplete = doc.RawJSON, doc.Incomplete
	case *store.KeyValueData:
		// Store string headers only; never concatenate the entry payloads into a
		// second document merely to render the detail.
		cols := make([]string, 0, len(v.Metadata)+len(v.Entries))
		vals := make([]string, 0, cap(cols))
		for name, value := range v.Metadata {
			cols, vals = append(cols, name), append(vals, value)
		}
		var incomplete map[int]store.IncompleteCell
		for _, entry := range v.Entries {
			if entry.Incomplete != nil {
				if incomplete == nil {
					incomplete = make(map[int]store.IncompleteCell)
				}
				incomplete[len(cols)] = *entry.Incomplete
			}
			name := entry.Index
			if entry.Extra != "" {
				name += " [" + entry.Extra + "]"
			}
			cols, vals = append(cols, name), append(vals, entry.Value)
		}
		m.openDetail(fmt.Sprintf("Key: %s (%s, TTL: %s)", v.Key, v.Type, v.TTL), cols, vals)
		m.detailIncomplete = incomplete
	case *store.GraphData:
		if b.Cursor < 0 || b.Cursor >= len(v.Nodes) {
			return
		}
		node := &v.Nodes[b.Cursor]
		m.openDetail(fmt.Sprintf("Node: %s (:%s)", node.ID, strings.Join(node.Labels, ":")), nil, nil)
		m.detailGraph = node
		if len(node.Incomplete) > 0 {
			m.detailIncompleteReason = "node has preview/deferred properties"
		}
	case *store.RawTextData:
		m.openDetail(v.Title, nil, nil)
		m.detailDoc, m.detailItemIncomplete = v.Text, v.Incomplete
	default:
		return
	}
	if summary := store.IncompletenessSummary(b.Data); summary != "" {
		m.detailIncompleteReason = summary
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
	for i := range m.detailCols {
		val := ""
		if i < len(m.detailVals) {
			val = m.detailVals[i]
		}
		if cell, ok := m.detailIncomplete[i]; ok {
			val = screens.RenderRawText(&store.RawTextData{Text: val, Incomplete: &cell}, contentW-2, 1)
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
	if m.detailCursor < 0 {
		return nil
	}
	if cell, ok := m.detailIncomplete[m.detailCursor]; ok {
		return m.setError("cannot copy " + cell.Label() + ": complete value is not loaded")
	}
	if len(m.detailCols) == 0 && m.detailIncompleteReason != "" {
		return m.incompleteDetailCopyError()
	}
	if len(m.detailCols) > 0 {
		if m.detailCursor >= 0 && m.detailCursor < len(m.detailCols) {
			col := m.detailCols[m.detailCursor]
			val := ""
			if m.detailCursor < len(m.detailVals) {
				val = m.detailVals[m.detailCursor]
			}
			if m.detailCursor < len(m.detailNulls) && m.detailNulls[m.detailCursor] {
				val = "NULL"
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
	if cell, ok := m.detailIncomplete[m.detailCursor]; ok {
		return m.setError("cannot copy " + cell.Label() + ": complete value is not loaded")
	}
	if len(m.detailCols) > 0 {
		if m.detailCursor >= 0 && m.detailCursor < len(m.detailCols) {
			col := m.detailCols[m.detailCursor]
			val := ""
			if m.detailCursor < len(m.detailVals) {
				val = m.detailVals[m.detailCursor]
			}
			if m.detailCursor < len(m.detailNulls) && m.detailNulls[m.detailCursor] {
				val = "NULL"
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
	if m.detailIncompleteReason != "" {
		return m.incompleteDetailCopyError()
	}
	if len(m.detailCols) > 0 {
		var b strings.Builder
		for i, c := range m.detailCols {
			val := ""
			if i < len(m.detailVals) {
				val = m.detailVals[i]
			}
			if i < len(m.detailNulls) && m.detailNulls[i] {
				val = "NULL"
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

func (m *Model) incompleteDetailCopyError() tea.Cmd {
	return m.setError("cannot copy incomplete detail: " + m.detailIncompleteReason + "; complete data is not loaded")
}

// renderActiveDetail renders the active detail view.
func (m *Model) renderActiveDetail(width, height int) string {
	title := m.detailTitle
	if m.detailIncompleteReason != "" {
		title = "INCOMPLETE: " + title + " — " + m.detailIncompleteReason
	}
	if m.detailDoc != "" || m.detailItemIncomplete != nil {
		if m.detailItemIncomplete != nil {
			raw := &store.RawTextData{Text: m.detailDoc, Incomplete: m.detailItemIncomplete}
			return styles.StyleHeader.Render(title) + "\n\n" + screens.RenderRawText(raw, width, height-2)
		}
		return renderTextDetail(title, m.detailDoc, m.detailScroll, width, height)
	}
	if m.detailGraph != nil {
		return renderGraphNodeDetail(title, m.detailGraph, m.detailScroll, width, height)
	}
	if m.detailIncompleteReason != "" {
		height = max(0, height-1)
	}
	rows := screens.RenderRowDetailWithMetadata(title, m.detailCols, m.detailVals, m.detailNulls, m.detailIncomplete, m.detailCursor, m.detailScroll, width, height)
	if m.detailIncompleteReason != "" {
		warning := screens.RenderRawText(&store.RawTextData{Text: "INCOMPLETE: " + m.detailIncompleteReason}, width, 1)
		return rows + "\n" + styles.StyleHeaderDim.Render(warning)
	}
	return rows
}

func renderTextDetail(title, text string, scroll, width, height int) string {
	var b strings.Builder
	b.WriteString(styles.StyleHeader.Render(title) + "\n\n")
	// Walk line headers without splitting/copying the entire retained payload.
	for range max(0, scroll) {
		_, rest, found := strings.Cut(text, "\n")
		if !found {
			text = ""
			break
		}
		text = rest
	}
	b.WriteString(screens.RenderRawText(&store.RawTextData{Text: text}, width, height-2))
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
		raw := &store.RawTextData{Text: v}
		if cell, ok := node.Incomplete[k]; ok {
			raw.Incomplete = &cell
		}
		value := screens.RenderRawText(raw, width-24, 1)
		lines = append(lines, fmt.Sprintf("  %-20s %s", k+":", value))
	}
	for k, cell := range node.Incomplete {
		if _, ok := node.Properties[k]; !ok {
			lines = append(lines, fmt.Sprintf("  %-20s %s", k+":", cell.Label()))
		}
	}
	lines = append(lines, "", styles.StyleColHeader.Render("Relationships:"))
	if len(node.Incident) == 0 {
		lines = append(lines, styles.StyleHeaderDim.Render("  (no incident edges)"))
	}
	for _, inc := range node.Incident {
		summary := screens.RenderRawText(&store.RawTextData{Text: inc.TargetSummary}, width, 1)
		lines = append(lines, fmt.Sprintf("  %s %s %s (%s)", inc.Direction, inc.Type, inc.TargetID, summary))
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
