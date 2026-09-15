// Package tui implements the bubbletea loop for picklock.
package tui

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/agmonetti/picklock/internal/browser"
	"github.com/agmonetti/picklock/internal/conn"
	"github.com/agmonetti/picklock/internal/editor"
	"github.com/agmonetti/picklock/internal/prefs"
	"github.com/agmonetti/picklock/internal/store"
	"github.com/agmonetti/picklock/internal/tui/screens"
	"github.com/agmonetti/picklock/internal/tui/styles"
)

// FlashMessageDuration defines how long flash notices (warnings, success, error) remain visible.
var FlashMessageDuration = 3 * time.Second

// Screen identifies the active screen.
type Screen int

const (
	// ScreenConnect is the connection screen (first on open).
	ScreenConnect Screen = iota
	// ScreenWorkspace is the single working screen: sidebar + main + editor.
	ScreenWorkspace
	// ScreenSettings is the preferences screen.
	ScreenSettings
)

// Divider targeted by a right-click drag.
const (
	resizeNone int = iota
	resizeSidebar
	resizeEditor
)

// editorDoneMsg carries the result of a query run in the background.
type editorDoneMsg struct {
	ed    *editor.Editor
	err   error
	token int
}

// NewOpts configures the initial model. A zero value is a plain start.
type NewOpts struct {
	// InitialCfg connects immediately on startup, skipping the connection
	// screen (used by `picklock <dsn>`). A connection error lands on the form.
	InitialCfg *conn.ConnectionConfig
	// GlobalReadOnly forces read-only connections for every engine, even those
	// made from the form or saved connections (used by `--read-only`).
	GlobalReadOnly bool
}

// Model is the main bubbletea model.
type Model struct {
	opts         NewOpts
	store        store.DataSource
	connect      *screens.ConnScreen
	settings     *screens.SettingsScreen
	browser      *browser.Browser
	editor       *editor.Editor
	editorScreen *screens.EditorScreen
	screen       Screen
	prevScreen   Screen // screen to return to from ScreenSettings
	focus        screens.WorkspaceFocus
	keys         KeyMap
	cfgLabel     string
	prefs        prefs.Prefs

	// workspace state
	structure     bool // the main pane shows the structure of the active item
	sidebarCursor int  // item selected in the sidebar

	spinner spinner.Model
	loading bool
	queryID int                // token to discard results from stale queries
	cancel  context.CancelFunc // cancels the running query

	// browser navigation runs in the background (like the editor) so a slow
	// item cannot freeze the UI.
	navigating    bool
	navCancel     context.CancelFunc
	navID         int // incremented per navigation; stale results are dropped
	connecting    bool
	connectCancel context.CancelFunc

	// workspace pane sizes (0 = auto), resizable with a right-click drag
	sidebarW  int
	editorH   int
	resizing  bool
	resizeDiv int // resizeNone, resizeSidebar or resizeEditor

	// horizontal column scroll in the main browser table (index of first visible column)
	colScroll int

	// vertical scroll offset for the structure inspector
	structureScroll int

	width           int
	height          int
	showSidebar     bool
	showMain        bool
	showEditor      bool
	maximized       bool
	prevShowSidebar bool
	prevShowMain    bool
	prevShowEditor  bool
	showHelp        bool
	err             string
	warn            string // advisory notice, rendered in amber below the content
	msgSeq          int    // sequence token to invalidate older clear timers

	// detail view ("v"): a snapshot of the selected item with full values
	showDetail   bool
	detailCursor int
	detailScroll int
	detailTitle  string
	detailCols   []string
	detailVals   []string
	detailDoc    string // formatted JSON document preview
	detailGraph  *store.GraphNode

	// export prompt ("alt+e"): a centered input for the target filename
	exporting   bool
	exportInput textinput.Model
	exportErr   string
	exportRes   store.DataView
	exportNote  string // "N rows" description appended to the success message
	exported    string // last success message, rendered in green

	// mouse text drag-selection in query editor
	dragSelecting bool
	dragStartLine int
	dragStartCol  int
	dragEndLine   int
	dragEndCol    int
}

// New creates the initial model (connection screen).
func New(opts ...NewOpts) *Model {
	var o NewOpts
	if len(opts) > 0 {
		o = opts[0]
	}
	saved, _ := conn.LoadSaved()
	p, _ := prefs.Load()
	exportInput := textinput.New()
	exportInput.Cursor.BlinkSpeed = screens.CursorBlink
	exportInput.Prompt = " "
	exportInput.Width = 40
	m := &Model{
		opts:         o,
		connect:      screens.NewConnScreen(saved),
		settings:     screens.NewSettingsScreen(),
		editor:       newEditorWithPersistedHistory(),
		editorScreen: screens.NewEditorScreen(),
		exportInput:  exportInput,
		screen:       ScreenConnect,
		prevScreen:   ScreenConnect,
		focus:        screens.FocusSidebar,
		keys:         DefaultKeyMap(),
		spinner:      spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(styles.StyleHeaderDim)),
		showSidebar:  true,
		showMain:     true,
		showEditor:   true,
		prefs:        p,
		sidebarW:     p.SidebarWidth,
		editorH:      p.EditorHeight,
	}
	if o.InitialCfg != nil {
		// show the workspace (with its connecting spinner) instead of flashing
		// the form for a single frame
		m.screen = ScreenWorkspace
	}
	return m
}

// Init implements tea.Model. With an initial DSN it connects immediately,
// skipping the connection screen.
func (m *Model) Init() tea.Cmd {
	if m.opts.InitialCfg != nil {
		return m.doConnect(*m.opts.InitialCfg)
	}
	return nil
}
