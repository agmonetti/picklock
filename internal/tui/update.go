package tui

import (
	"context"
	"os"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	term "github.com/charmbracelet/x/term"

	"github.com/agmonetti/picklock/internal/browser"
	"github.com/agmonetti/picklock/internal/editor"
	"github.com/agmonetti/picklock/internal/store"
	"github.com/agmonetti/picklock/internal/tui/screens"
)

// termGetSize is a seam for the size guard tests.
var termGetSize = term.GetSize

// correctSize returns a usable terminal size.
func correctSize(w, h int) (int, int) {
	if w < 10 || h < 3 || w > 500 || h > 1000 {
		if rw, rh, err := termGetSize(os.Stdout.Fd()); err == nil && rw >= 10 && rh >= 3 {
			w, h = rw, rh
		}
	}
	if w < 10 {
		w = 10
	}
	if h < 3 {
		h = 3
	}
	if w > 500 {
		w = 500
	}
	if h > 1000 {
		h = 1000
	}
	return w, h
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = correctSize(msg.Width, msg.Height)

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			if !m.typing() {
				return m, tea.Quit
			}
		case "?":
			if !m.typing() {
				m.showHelp = !m.showHelp
			}
			return m, nil
		case "ctrl+n":
			if !m.exporting && m.screen != ScreenSettings {
				m.newSession()
			}
			return m, nil
		case "ctrl+p":
			if !m.exporting && m.screen != ScreenSettings {
				m.openSettings()
			}
			return m, nil
		}

		// the export prompt owns every key while it is open
		if m.exporting {
			return m.handleExportKeys(msg)
		}

		// the detail view only accepts scrolling and Esc
		if m.showDetail {
			return m.handleDetailKeys(msg)
		}

		// while an operation runs, Esc cancels it
		if m.loading && msg.Type == tea.KeyEsc {
			m.cancelQuery()
			m.loading = false
			return m, nil
		}
		if m.navigating && msg.Type == tea.KeyEsc {
			m.cancelNav()
			m.navigating = false
			return m, nil
		}
		if m.connecting && msg.Type == tea.KeyEsc {
			m.cancelConnect()
			m.connecting = false
			return m, nil
		}
		// Esc also closes the help cheatsheet
		if m.showHelp && msg.Type == tea.KeyEsc {
			m.showHelp = false
			return m, nil
		}

		switch m.screen {
		case ScreenConnect:
			return m.handleConnectKeys(msg)
		case ScreenSettings:
			return m.handleSettingsKeys(msg)
		case ScreenWorkspace:
			return m.handleWorkspaceKeys(msg)
		}

	case screens.ConnectMsg:
		cmd := m.doConnect(msg.Cfg)
		return m, cmd

	case screens.SaveConnectionMsg:
		m.saveConnection(msg.Cfg)

	case screens.DeleteConnectionMsg:
		m.deleteConnection(msg.Name)

	case screens.SettingsMsg:
		m.prefs.QueryTimeoutSeconds = msg.QueryTimeoutSeconds
		if err := m.prefs.Save(); err != nil {
			m.settings.SetError(err.Error())
			return m, nil
		}
		m.leaveSettings()

	case screens.SettingsBackMsg:
		m.leaveSettings()

	case editorDoneMsg:
		m.cancelQuery()
		if msg.token != m.queryID {
			return m, nil
		}
		m.loading = false
		m.editorScreen.ResetResult()
		hist := m.editor.History
		m.editor = msg.ed
		m.editor.History = hist
		errCmd := m.setErr(friendlyErr(msg.err))
		m.editorScreen.Focus()

		if msg.err == nil && msg.ed.LastQuery != "" {
			hist.Push(msg.ed.LastQuery)
			editor.SaveHistory(hist.Items())
		}

		// A write query may have changed the schema or the data: refresh the catalog and active item in background
		var cmds []tea.Cmd
		if errCmd != nil {
			cmds = append(cmds, errCmd)
		}
		if msg.err == nil && m.browser != nil && m.store != nil && msg.ed.Wrote {
			if cmd := m.runBrowserOp(func(b *browser.Browser, st store.DataSource, ctx context.Context) error {
				return b.Reload(ctx, st)
			}); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if len(cmds) == 0 {
			return m, nil
		}
		return m, tea.Batch(cmds...)

	case browserDoneMsg:
		if msg.load {
			if msg.token != m.queryID {
				return m, nil
			}
			m.cancelConnect()
			m.connecting = false
		} else {
			if msg.navID != m.navID || msg.token != m.queryID {
				return m, nil
			}
			m.cancelNav()
			m.navigating = false
		}
		if msg.err != nil {
			err := friendlyErr(msg.err)
			if msg.load {
				m.closeStore()
				m.connect.SetError(err.Error())
				m.screen = ScreenConnect
				return m, nil
			}
			return m, m.setErr(err)
		}
		m.browser = msg.b
		if msg.load {
			m.screen = ScreenWorkspace
		}
		m.clampSidebar()
		return m, nil

	case clearMessageMsg:
		if msg.seq == m.msgSeq {
			m.err = ""
			m.warn = ""
			m.exported = ""
		}
		return m, nil

	case spinner.TickMsg:
		if m.busy() {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}

	case tea.MouseMsg:
		return m, m.handleMouse(msg)
	}

	return m, nil
}

// clearMessageMsg signals that the visibility timer for a flash notice expired.
type clearMessageMsg struct {
	seq int
}

// setWarn sets an advisory warning and returns a command to clear it after FlashMessageDuration.
func (m *Model) setWarn(msg string) tea.Cmd {
	m.warn = msg
	m.err = ""
	m.exported = ""
	if msg == "" {
		return nil
	}
	m.msgSeq++
	seq := m.msgSeq
	return tea.Tick(FlashMessageDuration, func(time.Time) tea.Msg {
		return clearMessageMsg{seq: seq}
	})
}

// setSuccess sets a confirmation message and returns a command to clear it after FlashMessageDuration.
func (m *Model) setSuccess(msg string) tea.Cmd {
	m.exported = msg
	m.err = ""
	m.warn = ""
	if msg == "" {
		return nil
	}
	m.msgSeq++
	seq := m.msgSeq
	return tea.Tick(FlashMessageDuration, func(time.Time) tea.Msg {
		return clearMessageMsg{seq: seq}
	})
}

// setError sets an error notice and returns a command to clear it after FlashMessageDuration.
func (m *Model) setError(msg string) tea.Cmd {
	m.err = msg
	m.warn = ""
	m.exported = ""
	if msg == "" {
		return nil
	}
	m.msgSeq++
	seq := m.msgSeq
	return tea.Tick(FlashMessageDuration, func(time.Time) tea.Msg {
		return clearMessageMsg{seq: seq}
	})
}

// typing reports whether the user is typing text (q/? must not quit).
func (m *Model) typing() bool {
	if m.exporting {
		return true
	}
	switch m.screen {
	case ScreenConnect:
		return m.connect.FocusOnField()
	case ScreenSettings:
		return m.settings.FocusOnField()
	case ScreenWorkspace:
		return m.focus == screens.FocusEditor
	}
	return false
}

// handleConnectKeys handles keys of the connection screen.
func (m *Model) handleConnectKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	updated, cmd := m.connect.Update(msg)
	m.connect = updated
	return m, cmd
}

// handleSettingsKeys handles keys of the settings screen.
func (m *Model) handleSettingsKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	updated, cmd := m.settings.Update(msg)
	m.settings = updated
	return m, cmd
}

// setErr stores or clears the current error message, auto-clearing after FlashMessageDuration if non-nil.
func (m *Model) setErr(err error) tea.Cmd {
	if err != nil {
		return m.setError(err.Error())
	}
	m.err = ""
	return nil
}
