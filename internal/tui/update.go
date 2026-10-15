package tui

import (
	"os"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	term "github.com/charmbracelet/x/term"

	"github.com/agmonetti/picklock/internal/browser"
	"github.com/agmonetti/picklock/internal/editor"
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
		if m.structure {
			innerW := m.width - 2
			if innerW < 1 {
				innerW = 1
			}
			contentH := m.height - 4
			if contentH < 1 {
				contentH = 1
			}
			layout := screens.ComputeLayout(innerW, contentH, m.showSidebar, m.showMain, m.showEditor, m.sidebarW, m.editorH)
			m.clampStructureScroll(layout.MainH - 2)
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.closeDetailCell()
			return m, tea.Quit
		case "q":
			if !m.typing() {
				m.closeDetailCell()
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

		// The file-path prompt owns every key while it is open.
		if m.detailSaving {
			return m.handleDetailSaveKeys(msg)
		}
		if m.browseDialog != browseDialogNone {
			return m, m.handleBrowseDialogKey(msg)
		}
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
			m.navID++
			m.cancelNav()
			m.navigating = false
			m.pendingBrowseAcceptance = ""
			m.browseOpenPending = false
			if m.browsePreparing {
				m.sidebarCursor = m.browsePreviousIndex
				m.browsePreparing = false
			}
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
		p := msg.Prefs
		p.SidebarWidth = m.prefs.SidebarWidth
		p.EditorHeight = m.prefs.EditorHeight
		if err := p.Save(); err != nil {
			m.settings.SetError(err.Error())
			return m, nil
		}
		m.prefs = p
		m.leaveSettings()

	case screens.SettingsBackMsg:
		m.leaveSettings()

	case cellSpoolTickMsg:
		if m.detailCellLoading {
			return m, cellSpoolTickCmd()
		}

	case cellSpoolDoneMsg:
		m.detailCellLoading = false
		if m.detailCellCancel != nil {
			m.detailCellCancel()
			m.detailCellCancel = nil
		}
		m.detailCellProgress = nil
		if !m.showDetail {
			if msg.file != nil {
				name := msg.file.Name()
				_ = msg.file.Close()
				_ = os.Remove(name)
			}
			return m, nil
		}
		if msg.err != nil {
			return m, m.setError("complete cell read failed: " + msg.err.Error())
		}
		m.detailCellFile, m.detailCellSize, m.detailCellBinary = msg.file, msg.size, msg.binary
		m.detailCellOffset = 0
		delete(m.detailIncomplete, m.detailCursor)
		if len(m.detailIncomplete) == 0 {
			m.detailIncompleteReason = ""
		}

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
			if cmd := m.reloadActiveCmd(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if len(cmds) == 0 {
			return m, nil
		}
		return m, tea.Batch(cmds...)

	case preparedDoneMsg:
		if msg.load {
			if msg.token != m.queryID {
				return m, nil
			}
			m.cancelConnect()
			m.connecting = false
			if msg.err != nil {
				m.closeStore()
				m.connect.SetError(friendlyErr(msg.err).Error())
				m.screen = ScreenConnect
				return m, nil
			}
			m.browser = msg.b
			m.screen = ScreenWorkspace
		} else {
			if msg.navID != m.navID || msg.token != m.queryID {
				return m, nil
			}
			m.cancelNav()
			m.navigating = false
			m.browsePreparing = false
			if msg.err != nil {
				m.browseForceOptions = false
				m.browseRefreshCurrent = false
				m.sidebarCursor = m.browsePreviousIndex
				return m, m.setErr(friendlyErr(msg.err))
			}
		}
		if msg.item == nil {
			return m, nil
		}
		sameActive := !msg.initial && m.browser != nil && msg.item.Name == m.browser.ActiveTable
		sameSchema := false
		if sameActive && m.browser.Structure != nil {
			sameSchema = browseSignature(&browser.PreparedItem{Structure: m.browser.Structure}) == browseSignature(msg.item)
		}
		if !msg.initial {
			m.browser = msg.b
		}
		m.browsePrepared = msg.item
		m.browseTargetIndex, m.browseInitial = msg.index, msg.initial
		if len(m.heavyTypes()) > 0 && m.acceptedBrowse[msg.item.Name] != browseSignature(msg.item) && !m.browseForceOptions {
			m.browseDialog = browseDialogNotice
			return m, nil
		}
		if m.browseForceOptions {
			m.openBrowseOptions()
			m.browseForceOptions = false
			return m, nil
		}
		if m.browseRefreshCurrent && sameActive && sameSchema {
			return m, m.refreshPrepared(msg.item)
		}
		options, pageSize := m.defaultBrowseOptions(), m.prefs.BrowsePageSize
		if sameActive {
			pageSize = m.browser.PageSize
			if sameSchema {
				options = m.browser.Options
			}
		}
		m.browseRefreshCurrent = false
		return m, m.applyPreparedBrowse(options, pageSize, false)

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
			m.pendingBrowseAcceptance = ""
			if m.browseOpenPending {
				m.sidebarCursor = m.browsePreviousIndex
				m.browseOpenPending = false
			}
			if msg.load {
				m.closeStore()
				m.connect.SetError(err.Error())
				m.screen = ScreenConnect
				return m, nil
			}
			return m, m.setErr(err)
		}
		if m.showDetail && m.browser != nil && msg.b != nil && m.browser.ActiveTable != msg.b.ActiveTable {
			m.closeDetailCell()
			m.showDetail = false
		}
		m.browser = msg.b
		if msg.load {
			m.screen = ScreenWorkspace
		}
		if m.pendingBrowseAcceptance != "" && m.browser != nil {
			m.acceptedBrowse[m.browser.ActiveTable] = m.pendingBrowseAcceptance
			m.pendingBrowseAcceptance = ""
		}
		if m.browseOpenPending {
			m.sidebarCursor = m.browseOpenIndex
			m.structure = false
			m.structureScroll, m.colScroll = 0, 0
			m.showMain = true
			m.setFocus(screens.FocusMain)
			m.browseOpenPending = false
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
