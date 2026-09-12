package tui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/agmonetti/picklock/internal/browser"
	"github.com/agmonetti/picklock/internal/store"
	"github.com/agmonetti/picklock/internal/tui/screens"
)

// handleWorkspaceKeys dispatches keys of the single working screen.
func (m *Model) handleWorkspaceKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	typingEditor := m.focus == screens.FocusEditor

	switch {
	case key.Matches(msg, m.keys.ToggleSidebar):
		m.toggleSidebar()
		return m, nil
	case key.Matches(msg, m.keys.ToggleMain):
		m.toggleMain()
		return m, nil
	case key.Matches(msg, m.keys.ToggleEditor):
		m.toggleEditor()
		return m, nil
	case key.Matches(msg, m.keys.ZoomPane):
		m.toggleZoom()
		return m, nil
	case key.Matches(msg, m.keys.FocusSidebar):
		m.maximized = false
		m.showSidebar = true
		m.setFocus(screens.FocusSidebar)
		return m, nil
	case key.Matches(msg, m.keys.FocusMain):
		m.maximized = false
		m.showMain = true
		m.setFocus(screens.FocusMain)
		return m, nil
	case key.Matches(msg, m.keys.FocusEditor):
		m.maximized = false
		m.showEditor = true
		m.setFocus(screens.FocusEditor)
		return m, nil
	case key.Matches(msg, m.keys.Switch):
		m.cycleFocus()
		return m, nil
	case !typingEditor && key.Matches(msg, m.keys.Inspect):
		m.showMain = true
		m.structure = true
		m.setFocus(screens.FocusMain)
		return m, nil
	case !typingEditor && key.Matches(msg, m.keys.Refresh) && m.browser != nil:
		return m, m.runBrowserOp(func(b *browser.Browser, st store.DataSource, ctx context.Context) error {
			return b.Reload(ctx, st)
		})
	case !m.loading && !m.navigating && key.Matches(msg, m.keys.Export):
		return m, m.openExport()
	}

	switch m.focus {
	case screens.FocusSidebar:
		return m.handleSidebarKeys(msg)
	case screens.FocusMain:
		return m.handleMainKeys(msg)
	case screens.FocusEditor:
		return m.handleEditorKeys(msg)
	}
	return m, nil
}

// visiblePanels returns how many workspace panes are currently visible.
func (m *Model) visiblePanels() int {
	count := 0
	if m.showSidebar {
		count++
	}
	if m.showMain {
		count++
	}
	if m.showEditor {
		count++
	}
	return count
}

// isPaneVisible returns whether a workspace pane is currently visible.
func (m *Model) isPaneVisible(f screens.WorkspaceFocus) bool {
	switch f {
	case screens.FocusSidebar:
		return m.showSidebar
	case screens.FocusMain:
		return m.showMain
	case screens.FocusEditor:
		return m.showEditor
	}
	return false
}

// ensureValidFocus moves focus to a visible pane if the current focused pane became hidden.
func (m *Model) ensureValidFocus() {
	if m.isPaneVisible(m.focus) {
		return
	}
	if m.showMain {
		m.setFocus(screens.FocusMain)
	} else if m.showEditor {
		m.setFocus(screens.FocusEditor)
	} else if m.showSidebar {
		m.setFocus(screens.FocusSidebar)
	}
}

// toggleSidebar toggles the sidebar visibility if at least one pane remains.
func (m *Model) toggleSidebar() {
	if m.showSidebar && m.visiblePanels() <= 1 {
		return
	}
	m.maximized = false
	m.showSidebar = !m.showSidebar
	m.ensureValidFocus()
}

// toggleMain toggles the main data view visibility if at least one pane remains.
func (m *Model) toggleMain() {
	if m.showMain && m.visiblePanels() <= 1 {
		return
	}
	m.maximized = false
	m.showMain = !m.showMain
	m.ensureValidFocus()
}

// toggleEditor toggles the query editor visibility if at least one pane remains.
func (m *Model) toggleEditor() {
	if m.showEditor && m.visiblePanels() <= 1 {
		return
	}
	m.maximized = false
	m.showEditor = !m.showEditor
	m.ensureValidFocus()
}

// toggleZoom maximizes the focused pane to take 100% of the workspace,
// or restores the previous visibility layout when toggled again.
func (m *Model) toggleZoom() {
	if m.maximized || m.visiblePanels() == 1 {
		if m.prevShowSidebar || m.prevShowMain || m.prevShowEditor {
			m.showSidebar = m.prevShowSidebar
			m.showMain = m.prevShowMain
			m.showEditor = m.prevShowEditor
		} else {
			m.showSidebar = true
			m.showMain = true
			m.showEditor = true
		}
		m.maximized = false
		m.ensureValidFocus()
		return
	}

	m.prevShowSidebar = m.showSidebar
	m.prevShowMain = m.showMain
	m.prevShowEditor = m.showEditor

	m.showSidebar = m.focus == screens.FocusSidebar
	m.showMain = m.focus == screens.FocusMain
	m.showEditor = m.focus == screens.FocusEditor
	m.maximized = true
}

// cycleFocus moves the focus to the next visible workspace pane.
func (m *Model) cycleFocus() {
	order := []screens.WorkspaceFocus{screens.FocusSidebar, screens.FocusMain, screens.FocusEditor}
	curIdx := 0
	for i, f := range order {
		if f == m.focus {
			curIdx = i
			break
		}
	}
	for i := 1; i <= len(order); i++ {
		next := order[(curIdx+i)%len(order)]
		if m.isPaneVisible(next) {
			m.setFocus(next)
			return
		}
	}
}

// setFocus moves the focus to a pane.
func (m *Model) setFocus(f screens.WorkspaceFocus) {
	if f == screens.FocusEditor {
		m.editorScreen.Focus()
	} else {
		m.editorScreen.Blur()
	}
	m.focus = f
}

// handleSidebarKeys handles keys when the sidebar has the focus.
func (m *Model) handleSidebarKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.browser == nil {
		return m, nil
	}
	b := m.browser
	switch {
	case key.Matches(msg, m.keys.Up):
		if m.sidebarCursor > 0 {
			m.sidebarCursor--
		}
		return m, nil
	case key.Matches(msg, m.keys.Down):
		if m.sidebarCursor < len(b.Tables)-1 {
			m.sidebarCursor++
		}
		return m, nil
	case key.Matches(msg, m.keys.PageUp):
		m.sidebarCursor -= 10
		m.clampSidebar()
		return m, nil
	case key.Matches(msg, m.keys.PageDown):
		m.sidebarCursor += 10
		m.clampSidebar()
		return m, nil
	case key.Matches(msg, m.keys.First):
		m.sidebarCursor = 0
		return m, nil
	case key.Matches(msg, m.keys.Last):
		if len(b.Tables) > 0 {
			m.sidebarCursor = len(b.Tables) - 1
		}
		return m, nil
	case msg.Type == tea.KeyEnter:
		m.maximized = false
		m.showMain = true
		m.setFocus(screens.FocusMain)
		return m, m.selectTable(m.sidebarCursor)
	case msg.Type == tea.KeyRunes && !msg.Alt && len(msg.Runes) == 1 &&
		msg.Runes[0] >= '1' && msg.Runes[0] <= '9':
		idx := int(msg.Runes[0] - '1')
		return m, m.selectTable(idx)
	}
	return m, nil
}

// selectTable opens the item at index idx in the sidebar.
func (m *Model) selectTable(idx int) tea.Cmd {
	if m.browser == nil || idx < 0 || idx >= len(m.browser.Tables) {
		return nil
	}
	m.sidebarCursor = idx
	m.structure = false
	m.colScroll = 0 // reset horizontal scroll when opening a new table
	name := m.browser.Tables[idx]
	return m.runBrowserOp(func(b *browser.Browser, st store.DataSource, ctx context.Context) error {
		return b.SelectItem(ctx, name, st)
	})
}

// clampSidebar keeps the sidebar cursor inside the item list.
func (m *Model) clampSidebar() {
	if m.browser == nil {
		return
	}
	if n := len(m.browser.Tables); n == 0 {
		m.sidebarCursor = 0
	} else if m.sidebarCursor >= n {
		m.sidebarCursor = n - 1
	} else if m.sidebarCursor < 0 {
		m.sidebarCursor = 0
	}
}

// handleMainKeys handles keys when the main pane has the focus.
func (m *Model) handleMainKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.browser == nil {
		return m, nil
	}
	b := m.browser
	if m.navigating {
		return m, nil
	}
	switch {
	case key.Matches(msg, m.keys.Back):
		if m.structure {
			m.structure = false
		}
		return m, nil
	case key.Matches(msg, m.keys.Detail):
		if !m.structure && b.Data != nil {
			m.openDetailFromBrowser(b)
		}
		return m, nil
	case key.Matches(msg, m.keys.Up):
		b.MoveCursor(-1)
		return m, nil
	case key.Matches(msg, m.keys.Down):
		b.MoveCursor(1)
		return m, nil
	case key.Matches(msg, m.keys.PageUp):
		return m, m.runBrowserOp(func(nb *browser.Browser, st store.DataSource, ctx context.Context) error {
			return nb.PrevPage(ctx, st)
		})
	case key.Matches(msg, m.keys.PageDown):
		return m, m.runBrowserOp(func(nb *browser.Browser, st store.DataSource, ctx context.Context) error {
			return nb.NextPage(ctx, st)
		})
	case key.Matches(msg, m.keys.First):
		b.MoveCursor(-1000000)
		return m, nil
	case key.Matches(msg, m.keys.Last):
		b.MoveCursor(1000000)
		return m, nil
	case msg.Type == tea.KeyLeft || key.Matches(msg, m.keys.ScrollColLeft):
		m.scrollColLeft()
		return m, nil
	case msg.Type == tea.KeyRight || key.Matches(msg, m.keys.ScrollColRight):
		m.scrollColRight()
		return m, nil
	}
	return m, nil
}

// scrollColLeft shifts the column viewport one column to the left.
func (m *Model) scrollColLeft() {
	m.colScroll--
	if m.colScroll < 0 {
		m.colScroll = 0
	}
}

// scrollColRight shifts the column viewport one column to the right.
func (m *Model) scrollColRight() {
	max := m.maxColScroll()
	m.colScroll++
	if m.colScroll > max {
		m.colScroll = max
	}
}

// maxColScroll returns the maximum allowed colScroll for the current browser data.
func (m *Model) maxColScroll() int {
	if m.browser == nil {
		return 0
	}
	b := m.browser
	var n int
	switch v := b.Data.(type) {
	case *store.TabularData:
		n = len(v.Columns)
		if n == 0 {
			n = len(b.Columns)
		}
	default:
		n = len(b.Columns)
	}
	if n <= 1 {
		return 0
	}
	return n - 1
}

// scrollEditorColLeft shifts the editor result column viewport one column to the left.
func (m *Model) scrollEditorColLeft() {
	c := m.editorScreen.ColScroll() - 1
	if c < 0 {
		c = 0
	}
	m.editorScreen.SetColScroll(c)
}

// scrollEditorColRight shifts the editor result column viewport one column to the right.
func (m *Model) scrollEditorColRight() {
	max := m.maxEditorColScroll()
	c := m.editorScreen.ColScroll() + 1
	if c > max {
		c = max
	}
	m.editorScreen.SetColScroll(c)
}

// maxEditorColScroll returns the maximum allowed colScroll for the current editor tabular result.
func (m *Model) maxEditorColScroll() int {
	if m.editor == nil || m.editor.Data == nil {
		return 0
	}
	tab, ok := m.editor.Data.(*store.TabularData)
	if !ok || len(tab.Columns) <= 1 {
		return 0
	}
	return len(tab.Columns) - 1
}

// copyQueryToClipboard copies the current query editor buffer to the system clipboard.
func (m *Model) copyQueryToClipboard() tea.Cmd {
	buf := m.editorScreen.Value()
	if strings.TrimSpace(buf) == "" {
		return m.setWarn("no query to copy")
	}
	if err := clipboardWriteAll(buf); err != nil {
		return m.setError("failed to copy: " + err.Error())
	}
	return m.setSuccess("query copied to clipboard")
}
