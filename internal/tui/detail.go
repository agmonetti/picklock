package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/agmonetti/picklock/internal/browser"
	"github.com/agmonetti/picklock/internal/store"
	"github.com/agmonetti/picklock/internal/tui/screens"
	"github.com/agmonetti/picklock/internal/tui/styles"
)

// handleDetailKeys handles the keys of the detail view.
func (m *Model) handleDetailKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.detailCellLoading && msg.Type != tea.KeyEsc && !key.Matches(msg, m.keys.Back) {
		return m, nil
	}
	switch {
	case msg.Type == tea.KeyEsc || key.Matches(msg, m.keys.Back) || msg.String() == "q" || msg.String() == "v":
		m.closeDetailCell()
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
	case msg.String() == "e":
		return m, m.spoolSelectedDetailCell()
	case msg.String() == "left" || msg.String() == "h":
		m.detailCellOffset = max(0, m.detailCellOffset-4096)
	case msg.String() == "right" || msg.String() == "l":
		m.detailCellOffset = min(max(int64(0), m.detailCellSize-1), m.detailCellOffset+4096)
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
	case msg.String() == "s":
		return m, m.openDetailSave()
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
	if m.detailCellFile != nil {
		if m.detailCellSize > int64(m.prefs.EditorMaxCellBytes) {
			return m.setError(fmt.Sprintf("value is %d bytes; clipboard limit is %d bytes; press s to save it to a file", m.detailCellSize, m.prefs.EditorMaxCellBytes))
		}
		value := make([]byte, int(m.detailCellSize))
		n, err := m.detailCellFile.ReadAt(value, 0)
		if err != nil && err != io.EOF {
			return m.setError("failed to read spooled value: " + err.Error())
		}
		if int64(n) != m.detailCellSize {
			return m.setError("spooled value is incomplete; refusing to copy")
		}
		if err := clipboardWriteAll(string(value)); err != nil {
			return m.setError("failed to copy: " + err.Error())
		}
		return m.setSuccess("complete value copied to clipboard")
	}
	if cell, ok := m.detailIncomplete[m.detailCursor]; ok {
		return m.setError("cannot copy " + cell.Label() + ": open complete value with e first")
	}
	if len(m.detailCols) == 0 && m.detailIncompleteReason != "" {
		return m.incompleteDetailCopyError()
	}
	if len(m.detailCols) > 0 {
		if m.detailCursor >= len(m.detailCols) {
			return nil
		}
		col := m.detailCols[m.detailCursor]
		val := ""
		if m.detailCursor < len(m.detailVals) {
			val = m.detailVals[m.detailCursor]
		}
		if m.detailCursor < len(m.detailNulls) && m.detailNulls[m.detailCursor] {
			val = "NULL"
		}
		if err := clipboardWriteAll(val); err != nil {
			return m.setError("failed to copy: " + err.Error())
		}
		return m.setSuccess(fmt.Sprintf("copied '%s' value to clipboard", col))
	}
	if m.detailDoc != "" {
		if err := clipboardWriteAll(m.detailDoc); err != nil {
			return m.setError("failed to copy: " + err.Error())
		}
		return m.setSuccess("document copied to clipboard")
	}
	return nil
}

func (m *Model) copyDetailField() tea.Cmd {
	if m.detailCellFile != nil {
		if m.detailCellSize > int64(m.prefs.EditorMaxCellBytes) {
			return m.setError(fmt.Sprintf("value is %d bytes; clipboard limit is %d bytes; press s to save it to a file", m.detailCellSize, m.prefs.EditorMaxCellBytes))
		}
		value := make([]byte, int(m.detailCellSize))
		n, err := m.detailCellFile.ReadAt(value, 0)
		if err != nil && err != io.EOF {
			return m.setError("failed to read spooled value: " + err.Error())
		}
		if int64(n) != m.detailCellSize {
			return m.setError("spooled value is incomplete; refusing to copy")
		}
		return m.copySpoolField(value)
	}
	if cell, ok := m.detailIncomplete[m.detailCursor]; ok {
		return m.setError("cannot copy " + cell.Label() + ": complete value is not loaded")
	}
	if len(m.detailCols) > 0 && m.detailCursor >= 0 && m.detailCursor < len(m.detailCols) {
		col := m.detailCols[m.detailCursor]
		val := ""
		if m.detailCursor < len(m.detailVals) {
			val = m.detailVals[m.detailCursor]
		}
		if m.detailCursor < len(m.detailNulls) && m.detailNulls[m.detailCursor] {
			val = "NULL"
		}
		if err := clipboardWriteAll(fmt.Sprintf("%s: %s", col, val)); err != nil {
			return m.setError("failed to copy: " + err.Error())
		}
		return m.setSuccess(fmt.Sprintf("copied field '%s' to clipboard", col))
	}
	return m.copyDetailValue()
}

func (m *Model) copySpoolField(value []byte) tea.Cmd {
	if m.detailCursor < 0 || m.detailCursor >= len(m.detailCols) {
		return nil
	}
	text := m.detailCols[m.detailCursor] + ": " + string(value)
	if err := clipboardWriteAll(text); err != nil {
		return m.setError("failed to copy: " + err.Error())
	}
	return m.setSuccess(fmt.Sprintf("complete field '%s' copied to clipboard", m.detailCols[m.detailCursor]))
}

func (m *Model) copyDetailAll() tea.Cmd {
	if m.detailCellFile != nil {
		return m.setError("cannot copy all fields: only the selected cell is complete; save it with s or copy it with c")
	}
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
		if err := clipboardWriteAll(strings.TrimSpace(b.String())); err != nil {
			return m.setError("failed to copy: " + err.Error())
		}
		return m.setSuccess("all fields copied to clipboard")
	}
	if m.detailDoc != "" {
		if err := clipboardWriteAll(m.detailDoc); err != nil {
			return m.setError("failed to copy: " + err.Error())
		}
		return m.setSuccess("document copied to clipboard")
	}
	if m.detailGraph != nil {
		var b strings.Builder
		b.WriteString(fmt.Sprintf("ID: %s\nLabels: %s\n", m.detailGraph.ID, strings.Join(m.detailGraph.Labels, ":")))
		for k, v := range m.detailGraph.Properties {
			b.WriteString(fmt.Sprintf("%s: %s\n", k, v))
		}
		if err := clipboardWriteAll(strings.TrimSpace(b.String())); err != nil {
			return m.setError("failed to copy: " + err.Error())
		}
		return m.setSuccess("node details copied to clipboard")
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
	if m.detailCellLoading {
		done, total := int64(0), int64(-1)
		if m.detailCellProgress != nil {
			done, total = m.detailCellProgress.done.Load(), m.detailCellProgress.total.Load()
		}
		progress := fmt.Sprintf("Spooling: %d bytes", done)
		if total >= 0 {
			progress += fmt.Sprintf(" of %d bytes", total)
			if total > 0 {
				progress += fmt.Sprintf(" (%d%%)", min(100, int(float64(done)*100/float64(total))))
			}
		}
		return styles.StyleHeader.Render(title) + "\n\n" + progress
	}
	if m.detailCellFile != nil {
		buf := make([]byte, min(8192, max(0, width*max(1, height-3))))
		n, err := m.detailCellFile.ReadAt(buf, m.detailCellOffset)
		if err != nil && err != io.EOF {
			return styles.StyleHeader.Render(title) + "\n\n" + err.Error()
		}
		content := string(buf[:n])
		if m.detailCellBinary {
			content = fmt.Sprintf("% x", buf[:n])
		} else {
			content = strings.ToValidUTF8(content, "�")
		}
		return styles.StyleHeader.Render(fmt.Sprintf("%s — %d/%d bytes (←/→ page, Esc close)", title, m.detailCellOffset, m.detailCellSize)) + "\n\n" + screens.RenderRawText(&store.RawTextData{Text: content}, width, height-2)
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
func (m *Model) closeDetailCell() {
	if m.detailCellCancel != nil {
		m.detailCellCancel()
		m.detailCellCancel = nil
	}
	m.detailCellLoading = false
	if m.detailCellFile != nil {
		name := m.detailCellFile.Name()
		_ = m.detailCellFile.Close()
		_ = os.Remove(name)
		m.detailCellFile = nil
	}
	m.detailCellProgress = nil
	m.detailCellSize, m.detailCellOffset = 0, 0
}
func (m *Model) openDetailSave() tea.Cmd {
	if m.detailCellFile == nil {
		return m.setError("open the complete value with e before saving it")
	}
	m.exportInput.SetValue("")
	m.exportInput.Focus()
	m.exportErr = ""
	m.detailSaving = true
	return nil
}

func (m *Model) handleDetailSaveKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.detailSaving = false
		m.exportInput.Blur()
		return m, nil
	case tea.KeyEnter:
		path := strings.TrimSpace(m.exportInput.Value())
		if path == "" {
			m.exportErr = "type a destination file path"
			return m, nil
		}
		if dir := filepath.Dir(path); dir != "." {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				m.exportErr = err.Error()
				return m, nil
			}
		}
		dst, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			m.exportErr = err.Error()
			return m, nil
		}
		if _, err = io.Copy(dst, io.NewSectionReader(m.detailCellFile, 0, m.detailCellSize)); err != nil {
			_ = dst.Close()
			m.exportErr = "save failed; destination may contain a partial file: " + err.Error()
			return m, nil
		}
		if err = dst.Close(); err != nil {
			m.exportErr = "save failed; destination may contain a partial file: " + err.Error()
			return m, nil
		}
		abs, absErr := filepath.Abs(path)
		if absErr != nil {
			abs = path
		}
		m.detailSaving = false
		m.exportInput.Blur()
		return m, m.setSuccess(fmt.Sprintf("saved complete value (%d bytes) → %s", m.detailCellSize, abs))
	}
	updated, cmd := m.exportInput.Update(msg)
	m.exportInput = updated
	return m, cmd
}

func (m *Model) renderDetailSavePrompt(width, height int) string {
	var b strings.Builder
	b.WriteString(styles.StyleHeader.Render("Save complete value"))
	b.WriteString("\n\n")
	b.WriteString(m.exportInput.View())
	b.WriteString("\n\n")
	b.WriteString(styles.StyleHeaderDim.Render("The spooled bytes are copied to this path without loading the whole value."))
	if m.exportErr != "" {
		b.WriteString("\n\n" + styles.StyleError.Render(m.exportErr))
	}
	content := b.String()
	lines := strings.Split(content, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

func (m *Model) spoolSelectedDetailCell() tea.Cmd {
	if m.detailCellLoading || m.detailCellFile != nil || m.detailCursor < 0 {
		return nil
	}
	if _, incomplete := m.detailIncomplete[m.detailCursor]; !incomplete {
		return m.setError("selected field is already complete")
	}
	if m.browser == nil {
		return m.setError("selected field has no reliable row identity")
	}
	data, ok := m.browser.Data.(*store.TabularData)
	if !ok || m.browser.Cursor < 0 || m.browser.Cursor >= len(data.RowRefs) || m.detailCursor >= len(m.detailCols) {
		return m.setError("selected field has no reliable row identity")
	}
	reader, ok := m.store.(store.CellReader)
	if !ok {
		return m.setError("this data source does not support complete cell reads")
	}
	ref := data.RowRef(m.browser.Cursor)
	if ref == nil {
		return m.setError("selected field has no reliable row identity")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(m.prefs.QueryTimeout())*time.Second)
	progress := &cellSpoolProgress{}
	progress.total.Store(-1)
	m.detailCellCancel, m.detailCellLoading, m.detailCellProgress = cancel, true, progress
	req := store.CellReadRequest{Row: ref, Column: m.detailCols[m.detailCursor], BlockBytes: m.prefs.ValueBlockBytes}
	worker := func() tea.Msg {
		cell, err := reader.ReadCell(ctx, req)
		if err == nil && cell.IsNull {
			return cellSpoolDoneMsg{err: fmt.Errorf("selected value is NULL")}
		}
		var file *os.File
		var size int64
		var binary bool
		if err == nil {
			binary = cell.Binary
			if cell.SizeBytes != nil {
				progress.total.Store(*cell.SizeBytes)
			}
			file, size, err = store.SpoolCellProgress(ctx, cell, req.BlockBytes, func(done, total int64) {
				progress.done.Store(done)
				progress.total.Store(total)
			})
		}
		return cellSpoolDoneMsg{file: file, size: size, binary: binary, err: err}
	}
	return tea.Batch(worker, cellSpoolTickCmd())
}
func cellSpoolTickCmd() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg { return cellSpoolTickMsg{} })
}
