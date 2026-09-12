package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/agmonetti/picklock/internal/conn"
	"github.com/agmonetti/picklock/internal/editor"
	"github.com/agmonetti/picklock/internal/store"
	"github.com/agmonetti/picklock/internal/tui/screens"
)

// newEditorWithPersistedHistory creates an editor whose history is preloaded
// from the previous sessions, so the last queries survive a restart.
func newEditorWithPersistedHistory() *editor.Editor {
	e := editor.New()
	if items := editor.LoadHistory(); items != nil {
		for _, q := range items {
			e.History.Push(q)
		}
	}
	return e
}

// doConnect opens the store and starts loading the browser.
func (m *Model) doConnect(cfg conn.ConnectionConfig) tea.Cmd {
	// a global --read-only forces every connection, including saved ones
	cfg.ReadOnly = cfg.ReadOnly || m.opts.GlobalReadOnly
	m.cancelNav()
	m.cancelConnect()
	m.cancelQuery()
	m.closeStore()
	m.queryID++
	m.navID = 0
	m.loading = false
	m.navigating = false
	m.connecting = false
	m.resizing = false
	m.showDetail = false
	m.exporting = false
	m.exported = ""
	m.warn = ""
	m.editorScreen.ResetResult()
	m.browser = nil
	m.editor = newEditorWithPersistedHistory()
	m.editorScreen.SetValue("")
	m.cfgLabel = cfg.Label()
	m.err = ""
	m.structure = false
	m.sidebarCursor = 0
	m.setFocus(screens.FocusSidebar)

	st, err := store.New(cfg)
	if err != nil {
		m.connect.SetError(err.Error())
		m.screen = ScreenConnect
		return nil
	}
	m.store = st

	// Configure query executor placeholder and title in editor
	q := st.Query()
	m.editorScreen.SetPlaceholder(q.Placeholder())
	m.editorScreen.SetTitle(q.PromptTitle())

	// Read-only advisory warnings for engines without native session-level locking
	var warnCmd tea.Cmd
	if cfg.ReadOnly && cfg.Driver == conn.DriverCassandra {
		warnCmd = m.setWarn("read-only is client-guarded on cassandra — connect with a read-only role for full enforcement")
	}
	loadCmd := m.loadBrowserCmd(st)
	if warnCmd != nil {
		return tea.Batch(loadCmd, warnCmd)
	}
	return loadCmd
}

// saveConnection persists the current connection.
func (m *Model) saveConnection(cfg conn.ConnectionConfig) {
	if cfg.Name == "" {
		cfg.Name = defaultName(cfg)
	}
	saved, err := conn.LoadSaved()
	if err == nil {
		saved = conn.SaveNamed(saved, cfg)
		err = conn.SaveSaved(saved)
	}
	if err != nil {
		m.connect.SetError(err.Error())
		return
	}
	m.connect.SetSaved(saved)
}

func defaultName(cfg conn.ConnectionConfig) string {
	if cfg.Driver == conn.DriverSQLite {
		return cfg.Path
	}
	if cfg.URI != "" {
		return cfg.Label()
	}
	return fmt.Sprintf("%s:%d/%s", cfg.Host, cfg.Port, cfg.Database)
}

// deleteConnection removes a saved connection by name.
func (m *Model) deleteConnection(name string) {
	if name == "" {
		return
	}
	saved, err := conn.LoadSaved()
	if err != nil {
		m.connect.SetError(err.Error())
		return
	}
	filtered := saved[:0]
	for _, s := range saved {
		if s.Name != name {
			filtered = append(filtered, s)
		}
	}
	if err := conn.SaveSaved(filtered); err != nil {
		m.connect.SetError(err.Error())
		return
	}
	m.connect.SetSaved(filtered)
}

// newSession closes the session and returns to the connection screen.
func (m *Model) newSession() {
	m.cancelNav()
	m.cancelConnect()
	m.cancelQuery()
	m.closeStore()
	m.queryID++
	m.navID = 0
	m.loading = false
	m.navigating = false
	m.connecting = false
	m.resizing = false
	m.showDetail = false
	m.exporting = false
	m.exported = ""
	m.warn = ""
	m.editorScreen.ResetResult()
	m.browser = nil
	m.editor = newEditorWithPersistedHistory()
	m.editorScreen.SetValue("")
	m.cfgLabel = ""
	m.err = ""
	m.structure = false
	m.sidebarCursor = 0
	m.setFocus(screens.FocusSidebar)
	m.screen = ScreenConnect
	m.connect.ResetForm()
}

func (m *Model) closeStore() {
	if m.store != nil {
		m.store.Close()
		m.store = nil
	}
}

// openSettings opens the preferences screen, remembering where to return.
func (m *Model) openSettings() {
	m.prevScreen = m.screen
	m.screen = ScreenSettings
	m.settings.SetValue(fmt.Sprintf("%d", m.prefs.QueryTimeout()))
	m.settings.SetError("")
	m.settings.Focus()
}

// leaveSettings returns to the screen the user was on before settings.
func (m *Model) leaveSettings() {
	m.screen = m.prevScreen
	m.settings.Blur()
}
