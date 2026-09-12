package tui

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/agmonetti/picklock/internal/export"
	"github.com/agmonetti/picklock/internal/store"
	"github.com/agmonetti/picklock/internal/tui/screens"
	"github.com/agmonetti/picklock/internal/tui/styles"
)

// openExport opens the export prompt for the data under the cursor: the last
// query result when the editor has the focus, the active item's current page
// otherwise.
func (m *Model) openExport() tea.Cmd {
	res, note, err := m.exportSource()
	if err != nil {
		return m.setError(err.Error())
	}
	m.exported = ""
	m.exportErr = ""
	m.exportRes = res
	m.exportNote = note
	m.exportInput.SetValue(fmt.Sprintf("picklock-export-%s.csv", time.Now().Format("20060102-150405")))
	m.exportInput.Focus()
	m.exporting = true
	return nil
}

// exportSource snapshots the data to export according to the current focus.
func (m *Model) exportSource() (store.DataView, string, error) {
	if m.focus == screens.FocusEditor {
		res := m.editor.Data
		if res == nil || res.IsEmpty() {
			return nil, "", fmt.Errorf("nothing to export — run a query that returns data")
		}
		return res, res.Summary(), nil
	}
	b := m.browser
	if b == nil || b.ActiveTable == "" || b.Data == nil || b.Data.IsEmpty() {
		return nil, "", fmt.Errorf("nothing to export — open an item first")
	}
	return b.Data, b.Data.Summary(), nil
}

// handleExportKeys handles the keys of the export prompt.
func (m *Model) handleExportKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyEsc:
		m.closeExport()
		return m, nil
	case msg.Type == tea.KeyEnter:
		name := strings.TrimSpace(m.exportInput.Value())
		if name == "" {
			m.exportErr = "type a file name"
			return m, nil
		}
		path, note, err := writeExport(m.exportRes, name)
		if err != nil {
			m.exportErr = err.Error()
			return m, nil
		}
		if note == "" {
			note = m.exportNote
		}
		m.closeExport()
		return m, m.setSuccess(fmt.Sprintf("exported %s → %s", note, path))
	}
	updated, cmd := m.exportInput.Update(msg)
	m.exportInput = updated
	return m, cmd
}

// closeExport closes the export prompt and returns to the workspace.
func (m *Model) closeExport() {
	m.exporting = false
	m.exportRes = nil
	m.exportNote = ""
	m.exportErr = ""
	m.exportInput.Blur()
}

// writeExport serializes the data view to the file name and returns the absolute path.
func writeExport(data store.DataView, name string) (string, string, error) {
	var buf bytes.Buffer
	var err error
	if strings.HasSuffix(strings.ToLower(name), ".json") {
		err = export.WriteJSON(data, &buf)
	} else {
		err = export.WriteCSV(data, &buf)
	}
	if err != nil {
		return "", "", err
	}
	if dir := filepath.Dir(name); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", "", err
		}
	}
	if err := os.WriteFile(name, buf.Bytes(), 0o600); err != nil {
		return "", "", err
	}
	abs, err := filepath.Abs(name)
	if err != nil {
		abs = name
	}
	return abs, data.Summary(), nil
}

// renderExportPrompt renders the export prompt.
func (m *Model) renderExportPrompt(width, height int) string {
	style := styles.StyleInputBox
	if m.exportInput.Focused() {
		style = styles.StyleInputBoxFocus
	}
	box := style.Width(44).Render(m.exportInput.View())

	var b strings.Builder
	b.WriteString(styles.StyleHeader.Render("Export"))
	b.WriteString("\n\n")
	b.WriteString(box)
	b.WriteString("\n")
	b.WriteString(styles.StyleHeaderDim.Render("  format by extension: .csv or .json"))
	b.WriteString("\n\n")
	b.WriteString(styles.StyleBtnPrimary.Render("Enter · Export"))
	b.WriteString("  ")
	b.WriteString(styles.StyleBtnSecondary.Render("esc  cancel"))
	if m.exportErr != "" {
		b.WriteString("\n\n" + styles.StyleError.Render(m.exportErr))
	}

	content := lipgloss.NewStyle().Width(width).Align(lipgloss.Center).Render(b.String())
	if lipgloss.Height(content) > height {
		content = lipgloss.Place(width, height, lipgloss.Center, lipgloss.Top, content)
	} else {
		content = lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
	}
	lines := strings.Split(content, "\n")
	if len(lines) > height {
		content = strings.Join(lines[:height], "\n")
	}
	return content
}
