package tui

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	_ "modernc.org/sqlite"

	"github.com/agmonetti/picklock/internal/conn"
	"github.com/agmonetti/picklock/internal/prefs"
	"github.com/agmonetti/picklock/internal/store"
	_ "github.com/agmonetti/picklock/internal/store/mssql"
	_ "github.com/agmonetti/picklock/internal/store/mysql" // registers the engines for the tests
	_ "github.com/agmonetti/picklock/internal/store/postgres"
	_ "github.com/agmonetti/picklock/internal/store/sqlite"
	"github.com/agmonetti/picklock/internal/tui/screens"
)

// Cursor blink, spinner wait, and flash message timers would block the synchronous step()
// helper for their real duration; tiny durations keep the tests fast.
func init() {
	clipboardWriteAll = func(text string) error { return nil }
	screens.CursorBlink = time.Millisecond
	FlashMessageDuration = time.Millisecond
}

// TestMain isolates every test from the real user configuration: saved
// connections, prefs and the query history all read/write a throwaway dir.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "picklock-tui-test")
	if err != nil {
		panic(err)
	}
	os.Setenv("PICKLOCK_CONFIG_DIR", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// createTestDB creates a temporary sqlite with users and orders tables.
func createTestDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	stmts := []string{
		"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT)",
		"CREATE TABLE orders (id INTEGER PRIMARY KEY)",
		"INSERT INTO users (name, email) VALUES ('Alice','a@t.com'), ('Bob','b@t.com')",
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("exec %q: %v", s, err)
		}
	}
	return path
}

// press sends a KeyMsg with runes to the model (typed text).
func press(t *testing.T, m *Model, text string) {
	t.Helper()
	step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
}

// newModel creates a Model and closes its store at the end of the test, so the
// SQLite file is not left locked (Windows fails to delete a TempDir in use).
func newModel(t *testing.T) *Model {
	t.Helper()
	m := New()
	t.Cleanup(func() {
		if m.store != nil {
			m.store.Close()
		}
	})
	return m
}

// connect creates a model connected to a fresh SQLite test database.
func connect(t *testing.T) *Model {
	t.Helper()
	db := createTestDB(t)
	m := newModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	pressKey(t, m, "tab") // focus the File field
	press(t, m, db)
	pressKey(t, m, "enter") // connect
	if m.screen != ScreenWorkspace {
		t.Fatalf("screen = %d, want ScreenWorkspace", m.screen)
	}
	return m
}

func execSQL(t *testing.T, m *Model, sql string) {
	t.Helper()
	if _, err := m.store.Query().Execute(context.Background(), sql, 0, 100); err != nil {
		t.Fatalf("execSQL(%q): %v", sql, err)
	}
}

var namedKeys = map[string]tea.KeyType{
	"tab":    tea.KeyTab,
	"enter":  tea.KeyEnter,
	"esc":    tea.KeyEsc,
	"up":     tea.KeyUp,
	"down":   tea.KeyDown,
	"left":   tea.KeyLeft,
	"right":  tea.KeyRight,
	"home":   tea.KeyHome,
	"end":    tea.KeyEnd,
	"pgup":   tea.KeyPgUp,
	"pgdown": tea.KeyPgDown,
	"ctrl+c": tea.KeyCtrlC,
	"ctrl+i": tea.KeyCtrlI,
	"ctrl+l": tea.KeyCtrlL,
	"ctrl+n": tea.KeyCtrlN,
	"ctrl+p": tea.KeyCtrlP,
	"ctrl+r": tea.KeyCtrlR,
	"ctrl+s": tea.KeyCtrlS,
}

// pressKey sends a typed KeyMsg (tab, enter, ctrl+n, etc.).
func pressKey(t *testing.T, m *Model, key string) {
	t.Helper()
	kt, ok := namedKeys[key]
	if !ok {
		t.Fatalf("key %q not mapped in the test", key)
	}
	step(t, m, tea.KeyMsg{Type: kt})
}

// pressAlt sends an alt+digit key (used to jump between workspace panes).
func pressAlt(t *testing.T, m *Model, digit string) {
	t.Helper()
	step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(digit), Alt: true})
}

// step applies a message to the model and runs the returned cmd, feeding back
// the messages it produces (like the bubbletea program does at runtime).
func step(t *testing.T, m *Model, msg tea.Msg) {
	t.Helper()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, sub := range batch {
			if sub == nil {
				continue
			}
			if subMsg := sub(); subMsg != nil {
				step(t, m, subMsg)
			}
		}
		return
	}
	updated, cmd := m.Update(msg)
	m2, ok := updated.(*Model)
	if !ok {
		t.Fatalf("Update returned %T, want *Model", updated)
	}
	*m = *m2
	if cmd != nil {
		if out := cmd(); out != nil {
			if _, isClear := out.(clearMessageMsg); isClear {
				return
			}
			if batch, ok := out.(tea.BatchMsg); ok {
				for _, sub := range batch {
					if sub == nil {
						continue
					}
					if subMsg := sub(); subMsg != nil {
						if _, isClear := subMsg.(clearMessageMsg); !isClear {
							step(t, m, subMsg)
						}
					}
				}
			} else {
				step(t, m, out)
			}
		}
	}
}

func TestModel_StartsOnConnect(t *testing.T) {
	m := newModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if m.screen != ScreenConnect {
		t.Fatalf("screen = %d, want ScreenConnect", m.screen)
	}
	v := m.View()
	if !strings.Contains(v, "picklock") || !strings.Contains(v, "Connect") {
		t.Errorf("View does not show the connection screen: %q", v)
	}
}

func TestModel_ConnectShowsWorkspace(t *testing.T) {
	m := connect(t)
	if m.screen != ScreenWorkspace {
		t.Fatalf("screen = %d, want ScreenWorkspace", m.screen)
	}
	if m.browser == nil {
		t.Fatal("browser nil after connecting")
	}
	if m.focus != screens.FocusSidebar {
		t.Errorf("focus = %v, want sidebar", m.focus)
	}
	if m.browser.ActiveTable != "orders" {
		t.Errorf("ActiveTable = %q, want orders", m.browser.ActiveTable)
	}
	v := m.View()
	if !strings.Contains(v, "users") || !strings.Contains(v, "orders") {
		t.Errorf("View does not show the sidebar with tables: %q", v)
	}
}

func TestModel_ConnectErrorStaysOnConnect(t *testing.T) {
	m := newModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	pressKey(t, m, "tab")
	press(t, m, filepath.Join(t.TempDir(), "definitely-missing.db"))
	pressKey(t, m, "enter")

	if m.screen != ScreenConnect {
		t.Fatalf("screen = %d, want ScreenConnect after error", m.screen)
	}
	if m.connect == nil || m.connect.Error() == "" {
		t.Error("expected a visible connection error")
	}
}

func TestModel_TabCyclesFocus(t *testing.T) {
	m := connect(t)
	if m.focus != screens.FocusSidebar {
		t.Fatalf("setup: focus = %v, want sidebar", m.focus)
	}
	pressKey(t, m, "tab")
	if m.focus != screens.FocusMain {
		t.Errorf("focus = %v, want main", m.focus)
	}
	pressKey(t, m, "tab")
	if m.focus != screens.FocusEditor {
		t.Errorf("focus = %v, want editor", m.focus)
	}
	pressKey(t, m, "tab")
	if m.focus != screens.FocusSidebar {
		t.Errorf("focus = %v, want sidebar (cycle)", m.focus)
	}
}

func TestModel_AltDigitsJumpFocus(t *testing.T) {
	m := connect(t)
	pressAlt(t, m, "3")
	if m.focus != screens.FocusEditor {
		t.Errorf("alt+3: focus = %v, want editor", m.focus)
	}
	pressAlt(t, m, "1")
	if m.focus != screens.FocusSidebar {
		t.Errorf("alt+1: focus = %v, want sidebar", m.focus)
	}
	pressAlt(t, m, "2")
	if m.focus != screens.FocusMain {
		t.Errorf("alt+2: focus = %v, want main", m.focus)
	}
}

func TestModel_NewSessionReturnsToConnect(t *testing.T) {
	m := connect(t)
	pressKey(t, m, "ctrl+n")
	if m.screen != ScreenConnect {
		t.Fatalf("screen = %d, want ScreenConnect", m.screen)
	}
}

func TestModel_SidebarEnterOpensTable(t *testing.T) {
	m := connect(t)
	if m.browser.ActiveTable != "orders" {
		t.Fatalf("setup: ActiveTable = %q, want orders", m.browser.ActiveTable)
	}
	pressKey(t, m, "down") // sidebar cursor -> users
	if m.sidebarCursor != 1 {
		t.Fatalf("sidebarCursor = %d, want 1", m.sidebarCursor)
	}
	pressKey(t, m, "enter")
	if m.browser.ActiveTable != "users" {
		t.Errorf("ActiveTable = %q, want users", m.browser.ActiveTable)
	}
	if m.focus != screens.FocusMain {
		t.Errorf("focus = %v, want FocusMain", m.focus)
	}
}

func TestModel_SidebarFirstLast(t *testing.T) {
	m := connect(t)
	// tables: orders, users (alphabetical); cursor starts at 0
	pressKey(t, m, "end") // last table
	if m.sidebarCursor != 1 {
		t.Fatalf("end: sidebarCursor = %d, want 1", m.sidebarCursor)
	}
	press(t, m, "g") // first table
	if m.sidebarCursor != 0 {
		t.Errorf("g: sidebarCursor = %d, want 0", m.sidebarCursor)
	}
}

func TestModel_OpenSettingsAndSave(t *testing.T) {
	t.Setenv("PICKLOCK_CONFIG_DIR", t.TempDir())
	m := connect(t)

	pressKey(t, m, "ctrl+p")
	if m.screen != ScreenSettings {
		t.Fatalf("screen = %d, want ScreenSettings", m.screen)
	}
	if v := m.View(); !strings.Contains(v, "Settings") {
		t.Errorf("settings screen does not render: %q", v)
	}
	// prefill the value as if the user typed it, then save
	m.settings.SetValue("90")
	pressKey(t, m, "enter")

	if m.screen != ScreenWorkspace {
		t.Fatalf("after save screen = %d, want back to workspace", m.screen)
	}
	if m.prefs.QueryTimeoutSeconds != 90 {
		t.Errorf("prefs.QueryTimeoutSeconds = %d, want 90", m.prefs.QueryTimeoutSeconds)
	}
	p, err := prefs.Load()
	if err != nil {
		t.Fatalf("prefs.Load: %v", err)
	}
	if p.QueryTimeoutSeconds != 90 {
		t.Errorf("persisted QueryTimeoutSeconds = %d, want 90", p.QueryTimeoutSeconds)
	}
}

func TestModel_SettingsEscBack(t *testing.T) {
	m := connect(t)
	pressKey(t, m, "ctrl+p")
	if m.screen != ScreenSettings {
		t.Fatalf("screen = %d, want ScreenSettings", m.screen)
	}
	pressKey(t, m, "esc")
	if m.screen != ScreenWorkspace {
		t.Errorf("after esc screen = %d, want workspace", m.screen)
	}
}

func TestModel_SettingsKeepsContext(t *testing.T) {
	m := newModel(t) // starts on the connect screen
	pressKey(t, m, "ctrl+p")
	if m.screen != ScreenSettings {
		t.Fatalf("screen = %d, want ScreenSettings", m.screen)
	}
	pressKey(t, m, "esc")
	if m.screen != ScreenConnect {
		t.Errorf("after esc screen = %d, want connect", m.screen)
	}
}

func TestFriendlyErr(t *testing.T) {
	if got := friendlyErr(context.DeadlineExceeded).Error(); got != "query timed out" {
		t.Errorf("DeadlineExceeded -> %q", got)
	}
	if got := friendlyErr(context.Canceled).Error(); got != "query cancelled" {
		t.Errorf("Canceled -> %q", got)
	}
	if got := friendlyErr(errors.New("boom")); got.Error() != "boom" {
		t.Errorf("other error -> %q", got)
	}
}

func mouseMsg(x, y int, button tea.MouseButton, action tea.MouseAction) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Button: button, Action: action}
}

// The workspace content starts at terminal cell (1, 2), and the test window is
// 100x30 (innerW=98, contentHeight=26). With automatic sizes the sidebar
// divider is at workspace x=19 (terminal x=20) and the editor divider at
// workspace y=16 (terminal y=18).
func TestModel_RightDragResizesSidebar(t *testing.T) {
	t.Setenv("PICKLOCK_CONFIG_DIR", t.TempDir())
	m := connect(t)

	step(t, m, mouseMsg(20, 5, tea.MouseButtonRight, tea.MouseActionPress))
	if !m.resizing || m.resizeDiv != resizeSidebar {
		t.Fatalf("resizing=%v div=%d, want sidebar drag", m.resizing, m.resizeDiv)
	}
	step(t, m, mouseMsg(30, 5, tea.MouseButtonRight, tea.MouseActionMotion))
	if m.sidebarW != 29 { // workspace x = 30-1
		t.Errorf("sidebarW = %d, want 29", m.sidebarW)
	}
	step(t, m, mouseMsg(30, 5, tea.MouseButtonRight, tea.MouseActionRelease))
	if m.resizing {
		t.Error("resizing should end on release")
	}

	p, err := prefs.Load()
	if err != nil {
		t.Fatalf("prefs.Load: %v", err)
	}
	if p.SidebarWidth != 29 {
		t.Errorf("persisted SidebarWidth = %d, want 29", p.SidebarWidth)
	}
}

func TestModel_RightDragResizesEditor(t *testing.T) {
	t.Setenv("PICKLOCK_CONFIG_DIR", t.TempDir())
	m := connect(t)

	step(t, m, mouseMsg(50, 18, tea.MouseButtonRight, tea.MouseActionPress))
	if !m.resizing || m.resizeDiv != resizeEditor {
		t.Fatalf("resizing=%v div=%d, want editor drag", m.resizing, m.resizeDiv)
	}
	// contentHeight-1 = 25; editorH = 25 - wy = 25 - 20 = 5
	step(t, m, mouseMsg(50, 22, tea.MouseButtonRight, tea.MouseActionMotion))
	if m.editorH != 5 {
		t.Errorf("editorH = %d, want 5", m.editorH)
	}
	step(t, m, mouseMsg(50, 22, tea.MouseButtonRight, tea.MouseActionRelease))
	if m.resizing {
		t.Error("resizing should end on release")
	}

	p, err := prefs.Load()
	if err != nil {
		t.Fatalf("prefs.Load: %v", err)
	}
	if p.EditorHeight != 5 {
		t.Errorf("persisted EditorHeight = %d, want 5", p.EditorHeight)
	}
}

func TestModel_LeftClickFocusesPane(t *testing.T) {
	m := connect(t)

	// sidebar region (workspace x < 19)
	step(t, m, mouseMsg(5, 5, tea.MouseButtonLeft, tea.MouseActionPress))
	if m.focus != screens.FocusSidebar {
		t.Errorf("click sidebar: focus = %v, want sidebar", m.focus)
	}

	// main region (workspace y <= 16)
	step(t, m, mouseMsg(50, 5, tea.MouseButtonLeft, tea.MouseActionPress))
	if m.focus != screens.FocusMain {
		t.Errorf("click main: focus = %v, want main", m.focus)
	}

	// editor region (workspace y > 16)
	step(t, m, mouseMsg(50, 22, tea.MouseButtonLeft, tea.MouseActionPress))
	if m.focus != screens.FocusEditor {
		t.Errorf("click editor: focus = %v, want editor", m.focus)
	}
}

func TestModel_DetailView(t *testing.T) {
	t.Setenv("PICKLOCK_CONFIG_DIR", t.TempDir())
	m := connect(t)
	press(t, m, "2")    // open users
	pressAlt(t, m, "2") // focus main
	press(t, m, "v")    // row detail
	if !m.showDetail {
		t.Fatal("detail should be open after v")
	}
	v := m.View()
	if !strings.Contains(v, "Alice") || !strings.Contains(v, "email") {
		t.Errorf("detail content missing: %q", v)
	}
	step(t, m, mouseMsg(50, 5, tea.MouseButtonWheelDown, tea.MouseActionPress))
	if m.detailScroll == 0 {
		t.Error("detailScroll should change with the wheel")
	}
	pressKey(t, m, "esc")
	if m.showDetail {
		t.Error("detail should close on Esc")
	}
}

func TestModel_DetailViewShowsLongValue(t *testing.T) {
	t.Setenv("PICKLOCK_CONFIG_DIR", t.TempDir())
	m := connect(t)
	// insert a row whose first column is very long and select it
	execSQL(t, m, "INSERT INTO orders (id) VALUES (999)")
	press(t, m, "2")    // open users (has rows)
	pressAlt(t, m, "2") // focus main
	// move the cursor to the last row and open its detail
	press(t, m, "G")
	press(t, m, "v")
	if !m.showDetail {
		t.Fatal("detail should be open after v")
	}
	v := m.View()
	if !strings.Contains(v, "users") {
		t.Errorf("detail title missing: %q", v)
	}
	pressKey(t, m, "esc")
	if m.showDetail {
		t.Error("detail should close on Esc")
	}
}

func TestModel_DetailView_ColumnOrdering(t *testing.T) {
	t.Setenv("PICKLOCK_CONFIG_DIR", t.TempDir())
	m := connect(t)
	press(t, m, "2") // open users

	// Simulate a situation where b.Columns (from schema/inspect) has different order than tab.Columns
	m.browser.Columns = []store.Column{
		{Name: "email"},
		{Name: "id"},
		{Name: "name"},
	}
	m.browser.Data = &store.TabularData{
		Columns: []string{"id", "name", "email"},
		Rows: [][]string{
			{"42", "Arthur", "arthur@galaxy.org"},
		},
	}
	m.browser.Cursor = 0

	pressAlt(t, m, "2") // focus main
	press(t, m, "v")    // open detail
	if !m.showDetail {
		t.Fatal("detail should be open")
	}

	if len(m.detailCols) != 3 || m.detailCols[0] != "id" || m.detailCols[1] != "name" || m.detailCols[2] != "email" {
		t.Errorf("detailCols = %v, want [id name email]", m.detailCols)
	}
	if len(m.detailVals) != 3 || m.detailVals[0] != "42" || m.detailVals[1] != "Arthur" || m.detailVals[2] != "arthur@galaxy.org" {
		t.Errorf("detailVals = %v, want [42 Arthur arthur@galaxy.org]", m.detailVals)
	}

	v := m.View()
	if !strings.Contains(v, "id:") || !strings.Contains(v, "42") ||
		!strings.Contains(v, "name:") || !strings.Contains(v, "Arthur") ||
		!strings.Contains(v, "email:") || !strings.Contains(v, "arthur@galaxy.org") {
		t.Errorf("rendered view missing expected column-value pairs: %q", v)
	}
}

func TestModel_DetailView_NavigateAndCopy(t *testing.T) {
	t.Setenv("PICKLOCK_CONFIG_DIR", t.TempDir())
	m := connect(t)
	press(t, m, "2")    // open users
	pressAlt(t, m, "2") // focus main
	press(t, m, "v")    // open detail

	if !m.showDetail {
		t.Fatal("detail should be open")
	}
	if m.detailCursor != 0 {
		t.Fatalf("initial detailCursor = %d, want 0", m.detailCursor)
	}

	// Navigate down
	press(t, m, "j")
	if m.detailCursor != 1 {
		t.Fatalf("detailCursor after j = %d, want 1", m.detailCursor)
	}

	// Copy value with 'c'
	press(t, m, "c")
	if !strings.Contains(m.exported, "copied") || !strings.Contains(m.exported, "value") {
		t.Errorf("expected copied value notification, got: %q", m.exported)
	}

	// Copy field with 'C'
	press(t, m, "C")
	if !strings.Contains(m.exported, "copied field") {
		t.Errorf("expected copied field notification, got: %q", m.exported)
	}

	// Copy all with 'a'
	press(t, m, "a")
	if m.exported != "all fields copied to clipboard" {
		t.Errorf("expected all fields copied notification, got: %q", m.exported)
	}

	// Jump to end with 'G' and start with 'g'
	press(t, m, "G")
	if m.detailCursor != len(m.detailCols)-1 {
		t.Errorf("detailCursor after G = %d, want %d", m.detailCursor, len(m.detailCols)-1)
	}
	press(t, m, "g")
	if m.detailCursor != 0 {
		t.Errorf("detailCursor after g = %d, want 0", m.detailCursor)
	}

	// Mouse click to select field 1 (terminal Y=7)
	step(t, m, mouseMsg(20, 7, tea.MouseButtonLeft, tea.MouseActionPress))
	step(t, m, mouseMsg(20, 7, tea.MouseButtonLeft, tea.MouseActionRelease))
	if m.detailCursor != 1 {
		t.Errorf("detailCursor after click = %d, want 1", m.detailCursor)
	}

	// Close with 'v'
	press(t, m, "v")
	if m.showDetail {
		t.Error("detail should close on 'v'")
	}
}

func TestModel_ClickFocusesConnectField(t *testing.T) {
	m := newModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30}) // innerW=98, contentHeight=26
	_ = m.View()                                        // establish the geometry (c.width/c.height + click rows)

	// the connect content starts at terminal cell (1,2). Within the 26-row
	// content area the File row sits at content y=15 -> terminal y=17.
	step(t, m, mouseMsg(50, 17, tea.MouseButtonLeft, tea.MouseActionPress))
	if m.connect.FocusIndex() != 1 {
		t.Fatalf("focus = %d, want 1 (File)", m.connect.FocusIndex())
	}
	if !m.connect.FocusOnField() {
		t.Error("the model must be in typing mode after clicking a field")
	}

	// a right-click or wheel must not disturb the form
	step(t, m, mouseMsg(50, 17, tea.MouseButtonRight, tea.MouseActionPress))
	if m.resizing {
		t.Error("right-click on the connect screen must not start a resize")
	}
	if m.connect.FocusIndex() != 1 {
		t.Errorf("focus changed to %d after a right-click", m.connect.FocusIndex())
	}
}

func TestModel_MouseIgnoredOnConnectScreen(t *testing.T) {
	t.Setenv("PICKLOCK_CONFIG_DIR", t.TempDir()) // don't depend on the user's prefs
	m := newModel(t)                         // starts on the connect screen
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	step(t, m, mouseMsg(20, 5, tea.MouseButtonRight, tea.MouseActionPress))
	if m.resizing {
		t.Error("mouse must be ignored outside the workspace")
	}
	step(t, m, mouseMsg(30, 5, tea.MouseButtonRight, tea.MouseActionMotion))
	if m.sidebarW != 0 {
		t.Errorf("sidebarW = %d, want 0", m.sidebarW)
	}
}

func TestModel_EscClosesHelp(t *testing.T) {
	m := connect(t)
	press(t, m, "?")
	if !m.showHelp {
		t.Fatal("help should be open after ?")
	}
	pressKey(t, m, "esc")
	if m.showHelp {
		t.Error("help should close on Esc")
	}
}

func TestModel_WheelScrollsSidebar(t *testing.T) {
	t.Setenv("PICKLOCK_CONFIG_DIR", t.TempDir()) // deterministic pane geometry
	m := connect(t)
	for i := 0; i < 30; i++ {
		execSQL(t, m, fmt.Sprintf("CREATE TABLE t%02d (id INTEGER PRIMARY KEY)", i))
	}
	if err := m.browser.Reload(context.Background(), m.store); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if m.sidebarCursor != 0 {
		t.Fatalf("setup: sidebarCursor = %d", m.sidebarCursor)
	}
	step(t, m, mouseMsg(5, 5, tea.MouseButtonWheelDown, tea.MouseActionPress))
	if m.sidebarCursor != 3 { // wheelStep
		t.Errorf("sidebarCursor = %d, want 3", m.sidebarCursor)
	}
	step(t, m, mouseMsg(5, 5, tea.MouseButtonWheelUp, tea.MouseActionPress))
	if m.sidebarCursor != 0 {
		t.Errorf("sidebarCursor = %d, want 0", m.sidebarCursor)
	}
}

func TestModel_WheelScrollsMain(t *testing.T) {
	t.Setenv("PICKLOCK_CONFIG_DIR", t.TempDir())
	m := connect(t)
	for i := 0; i < 60; i++ {
		execSQL(t, m, fmt.Sprintf("INSERT INTO users (name, email) VALUES ('u%d','u%d@t.com')", i, i))
	}
	press(t, m, "2") // open users
	pressAlt(t, m, "2")
	press(t, m, "G") // last row of page 0
	if m.browser.Cursor != 49 {
		t.Fatalf("setup: cursor = %d, want 49", m.browser.Cursor)
	}
	step(t, m, mouseMsg(50, 5, tea.MouseButtonWheelDown, tea.MouseActionPress))
	if m.browser.Page != 1 || m.browser.Cursor != 0 {
		t.Errorf("after wheel at bottom: page=%d cursor=%d, want page 1 cursor 0", m.browser.Page, m.browser.Cursor)
	}
	step(t, m, mouseMsg(50, 5, tea.MouseButtonWheelUp, tea.MouseActionPress))
	if m.browser.Page != 0 || m.browser.Cursor != len(m.browser.Rows)-1 {
		t.Errorf("after wheel at top: page=%d cursor=%d, want page 0 last row", m.browser.Page, m.browser.Cursor)
	}
}

func TestModel_WheelScrollsResults(t *testing.T) {
	t.Setenv("PICKLOCK_CONFIG_DIR", t.TempDir())
	m := connect(t)
	m.editor.Data = &store.TabularData{Columns: []string{"c"}, Rows: make([][]string, 60)}
	m.editorScreen.ResetResult()

	step(t, m, mouseMsg(50, 24, tea.MouseButtonWheelDown, tea.MouseActionPress))
	if got := m.editorScreen.ResultScroll(); got != 3 {
		t.Errorf("resultScroll = %d, want 3", got)
	}
	step(t, m, mouseMsg(50, 24, tea.MouseButtonWheelUp, tea.MouseActionPress))
	if got := m.editorScreen.ResultScroll(); got != 0 {
		t.Errorf("resultScroll = %d, want 0", got)
	}
}

func TestModel_ClickSelectsSidebarTable(t *testing.T) {
	t.Setenv("PICKLOCK_CONFIG_DIR", t.TempDir())
	m := connect(t)
	for i := 0; i < 30; i++ {
		execSQL(t, m, fmt.Sprintf("CREATE TABLE t%02d (id INTEGER PRIMARY KEY)", i))
	}
	if err := m.browser.Reload(context.Background(), m.store); err != nil {
		t.Fatalf("reload: %v", err)
	}
	step(t, m, mouseMsg(5, 5, tea.MouseButtonLeft, tea.MouseActionPress)) // table 2
	if m.sidebarCursor != 2 {
		t.Errorf("sidebarCursor = %d, want 2", m.sidebarCursor)
	}
}

func TestModel_ClickSelectsMainRow(t *testing.T) {
	t.Setenv("PICKLOCK_CONFIG_DIR", t.TempDir())
	m := connect(t)
	for i := 0; i < 10; i++ {
		execSQL(t, m, fmt.Sprintf("INSERT INTO users (name, email) VALUES ('u%d','u%d@t.com')", i, i))
	}
	press(t, m, "2")                                                       // open users (10 rows)
	step(t, m, mouseMsg(50, 7, tea.MouseButtonLeft, tea.MouseActionPress)) // data row 2
	if m.browser.Cursor != 2 {
		t.Errorf("cursor = %d, want 2", m.browser.Cursor)
	}
	step(t, m, mouseMsg(50, 9, tea.MouseButtonLeft, tea.MouseActionPress)) // data row 4
	if m.browser.Cursor != 4 {
		t.Errorf("cursor = %d, want 4", m.browser.Cursor)
	}
}

func TestModel_ClickSelectsResultRow(t *testing.T) {
	t.Setenv("PICKLOCK_CONFIG_DIR", t.TempDir())
	m := connect(t)
	m.editor.Data = &store.TabularData{Columns: []string{"c"}, Rows: make([][]string, 60)}
	m.editorScreen.ResetResult()

	step(t, m, mouseMsg(50, 25, tea.MouseButtonLeft, tea.MouseActionPress)) // first result row
	if m.editorScreen.ResultCursor() != 0 {
		t.Errorf("resultCursor = %d, want 0", m.editorScreen.ResultCursor())
	}
	step(t, m, mouseMsg(50, 26, tea.MouseButtonLeft, tea.MouseActionPress)) // second result row
	if m.editorScreen.ResultCursor() != 1 {
		t.Errorf("resultCursor = %d, want 1", m.editorScreen.ResultCursor())
	}
}

func TestModel_StructureMode(t *testing.T) {
	m := connect(t)
	pressAlt(t, m, "2") // main
	press(t, m, "i")
	if !m.structure {
		t.Fatal("structure should be on after i")
	}
	v := m.View()
	if !strings.Contains(v, "Columns") {
		t.Errorf("structure not shown in the main pane: %q", v)
	}
	pressKey(t, m, "esc")
	if m.structure {
		t.Fatal("structure should be off after esc")
	}
}

func TestModel_EditorExecutesQuery(t *testing.T) {
	m := connect(t)
	pressAlt(t, m, "3") // editor
	if m.focus != screens.FocusEditor {
		t.Fatalf("setup: focus = %v, want editor", m.focus)
	}

	press(t, m, "SELECT * FROM users")
	pressKey(t, m, "ctrl+r")

	if m.loading {
		t.Fatal("query did not finish: loading stayed true")
	}
	tab, ok := m.editor.Data.(*store.TabularData)
	if !ok || tab == nil {
		t.Fatalf("editor without result after running")
	}
	if len(tab.Rows) != 2 {
		t.Errorf("Rows = %d, want 2", len(tab.Rows))
	}
	v := m.View()
	if !strings.Contains(v, "Alice") {
		t.Errorf("View does not show the Alice row: %q", v)
	}
}

func TestModel_EditorEmptyBufferShowsNotice(t *testing.T) {
	m := connect(t)
	pressAlt(t, m, "3")
	pressKey(t, m, "ctrl+r") // empty buffer

	if m.loading {
		t.Fatal("an empty buffer must not run a query")
	}
	if m.editor == nil || m.editor.Error != "write a query first" {
		t.Fatalf("expected 'write a query first', got %+v", m.editor)
	}
}

func TestModel_EditorShowsError(t *testing.T) {
	m := connect(t)
	pressAlt(t, m, "3")
	press(t, m, "SELEC broken")
	pressKey(t, m, "ctrl+r")

	if m.editor == nil || m.editor.Error == "" {
		t.Fatalf("expected an SQL error in the editor")
	}
	if m.editor.Data != nil {
		t.Error("there should be no result with an error")
	}
}

func TestModel_EditorHistoryNavigation(t *testing.T) {
	m := connect(t)
	pressAlt(t, m, "3")

	press(t, m, "SELECT 1")
	pressKey(t, m, "ctrl+r")
	pressKey(t, m, "ctrl+l") // clear before the next query
	press(t, m, "SELECT 2")
	pressKey(t, m, "ctrl+r")
	pressKey(t, m, "ctrl+l") // clear to navigate the history

	pressKey(t, m, "up")
	if m.editor.Buffer != "SELECT 2" {
		t.Errorf("Buffer = %q, want SELECT 2 (most recent query)", m.editor.Buffer)
	}
	pressKey(t, m, "up")
	if m.editor.Buffer != "SELECT 1" {
		t.Errorf("Buffer = %q, want SELECT 1", m.editor.Buffer)
	}
}

func TestModel_AutoRefreshAfterInsert(t *testing.T) {
	m := connect(t)

	// sidebar focus default: quick-select users (second alphabetically)
	press(t, m, "2")
	if m.browser.ActiveTable != "users" {
		t.Fatalf("setup: ActiveTable = %q, want users", m.browser.ActiveTable)
	}

	pressAlt(t, m, "3") // editor
	press(t, m, "INSERT INTO users (name, email) VALUES ('Carol','c@t.com')")
	pressKey(t, m, "ctrl+r")
	tab, ok := m.editor.Data.(*store.TabularData)
	if !ok || tab.Affected != 1 {
		t.Fatalf("insert did not run: %+v", m.editor)
	}

	// a write query auto-refreshes the open table without pressing r
	if m.browser.TotalRows != 3 {
		t.Errorf("TotalRows = %d, want 3 after auto-refresh", m.browser.TotalRows)
	}
	v := m.View()
	if !strings.Contains(v, "Carol") {
		t.Errorf("View does not show the Carol row: %q", v)
	}
}

func TestModel_AutoRefreshAfterInsertReturning(t *testing.T) {
	m := connect(t)

	press(t, m, "2") // open users
	if m.browser.ActiveTable != "users" {
		t.Fatalf("setup: ActiveTable = %q, want users", m.browser.ActiveTable)
	}

	pressAlt(t, m, "3")
	press(t, m, "INSERT INTO users (name, email) VALUES ('Dana','d@t.com') RETURNING id")
	pressKey(t, m, "ctrl+r")
	tab, ok := m.editor.Data.(*store.TabularData)
	if !ok || len(tab.Columns) == 0 {
		t.Fatalf("INSERT RETURNING did not produce a table: %+v", m.editor)
	}

	// RETURNING returns rows (Affected = -1) but the data changed: the browser
	// must still auto-refresh.
	if m.browser.TotalRows != 3 {
		t.Errorf("TotalRows = %d, want 3 after auto-refresh (RETURNING)", m.browser.TotalRows)
	}
}

func TestModel_ToggleSidebar(t *testing.T) {
	m := connect(t)
	press(t, m, "2") // open users (has rows)
	if m.browser.ActiveTable != "users" {
		t.Fatalf("setup: ActiveTable = %q, want users", m.browser.ActiveTable)
	}
	if !m.showSidebar {
		t.Fatal("setup: showSidebar should be true")
	}

	pressAlt(t, m, "b")
	if m.showSidebar {
		t.Error("showSidebar should be false after alt+b")
	}
	v := m.View()
	if !strings.Contains(v, "Alice") {
		t.Errorf("main pane should still render with the sidebar hidden: %q", v)
	}

	pressAlt(t, m, "b")
	if !m.showSidebar {
		t.Error("showSidebar should be true after the second alt+b")
	}
}

func TestModel_ToggleMain(t *testing.T) {
	m := connect(t)
	press(t, m, "2") // open users
	if !m.showMain {
		t.Fatal("setup: showMain should be true")
	}

	pressAlt(t, m, "m")
	if m.showMain {
		t.Error("showMain should be false after alt+m")
	}
	if m.focus == screens.FocusMain {
		t.Errorf("focus should not stay on hidden main panel, got %v", m.focus)
	}
	v := m.View()
	if strings.Contains(v, "Alice") {
		t.Errorf("table rows should not render when main is hidden: %q", v)
	}
	if !strings.Contains(v, "SQL EDITOR") {
		t.Errorf("editor should render when main is hidden: %q", v)
	}

	pressAlt(t, m, "m")
	if !m.showMain {
		t.Error("showMain should be true after second alt+m")
	}
}

func TestModel_ToggleEditor(t *testing.T) {
	m := connect(t)
	press(t, m, "2") // open users
	if !m.showEditor {
		t.Fatal("setup: showEditor should be true")
	}

	pressAlt(t, m, "q")
	if m.showEditor {
		t.Error("showEditor should be false after alt+q")
	}
	v := m.View()
	if strings.Contains(v, "SQL EDITOR") {
		t.Errorf("editor should not render when hidden: %q", v)
	}
	if !strings.Contains(v, "Alice") {
		t.Errorf("table rows should render when editor is hidden: %q", v)
	}

	pressAlt(t, m, "q")
	if !m.showEditor {
		t.Error("showEditor should be true after second alt+q")
	}
}

func TestModel_ToggleMinimumOneGuard(t *testing.T) {
	m := connect(t)
	pressAlt(t, m, "b") // hide sidebar
	pressAlt(t, m, "q") // hide editor
	if m.showSidebar || m.showEditor || !m.showMain {
		t.Fatalf("expected only main to be visible: sidebar=%v, main=%v, editor=%v",
			m.showSidebar, m.showMain, m.showEditor)
	}

	// Try to hide the last visible panel (main)
	pressAlt(t, m, "m")
	if !m.showMain {
		t.Error("guard failed: main should remain visible when it is the only visible panel")
	}
}

func TestModel_FocusUnhidesPanel(t *testing.T) {
	m := connect(t)
	// Hide editor
	pressAlt(t, m, "q")
	if m.showEditor {
		t.Fatal("setup: editor should be hidden")
	}

	// Jump to editor with Alt+3
	pressAlt(t, m, "3")
	if !m.showEditor {
		t.Error("Alt+3 should unhide the editor")
	}
	if m.focus != screens.FocusEditor {
		t.Errorf("focus = %v, want FocusEditor", m.focus)
	}

	// Hide sidebar
	pressAlt(t, m, "b")
	if m.showSidebar {
		t.Fatal("setup: sidebar should be hidden")
	}

	// Jump to sidebar with Alt+1
	pressAlt(t, m, "1")
	if !m.showSidebar {
		t.Error("Alt+1 should unhide the sidebar")
	}
	if m.focus != screens.FocusSidebar {
		t.Errorf("focus = %v, want FocusSidebar", m.focus)
	}

	// Hide main
	pressAlt(t, m, "m")
	if m.showMain {
		t.Fatal("setup: main should be hidden")
	}

	// Jump to main with Alt+2
	pressAlt(t, m, "2")
	if !m.showMain {
		t.Error("Alt+2 should unhide the main panel")
	}
	if m.focus != screens.FocusMain {
		t.Errorf("focus = %v, want FocusMain", m.focus)
	}
}

func TestModel_CycleFocus_SkipsHiddenPanels(t *testing.T) {
	m := connect(t)
	// Hide editor
	pressAlt(t, m, "q")
	m.setFocus(screens.FocusSidebar)

	// Press Tab from Sidebar -> should focus Main
	pressKey(t, m, "tab")
	if m.focus != screens.FocusMain {
		t.Errorf("after tab: focus = %v, want FocusMain", m.focus)
	}

	// Press Tab from Main -> should skip hidden Editor and focus Sidebar
	pressKey(t, m, "tab")
	if m.focus != screens.FocusSidebar {
		t.Errorf("after second tab: focus = %v, want FocusSidebar (skipped hidden editor)", m.focus)
	}
}

func TestModel_ZoomPane(t *testing.T) {
	m := connect(t)
	press(t, m, "2") // open users table
	m.setFocus(screens.FocusMain)

	if !m.showSidebar || !m.showMain || !m.showEditor {
		t.Fatalf("setup: all panels should be visible initially")
	}

	// 1. Zoom Main pane
	pressAlt(t, m, "z")
	if m.showSidebar || !m.showMain || m.showEditor {
		t.Errorf("after Alt+Z on main: sidebar=%v, main=%v, editor=%v, want false, true, false",
			m.showSidebar, m.showMain, m.showEditor)
	}
	if !m.maximized {
		t.Error("maximized flag should be true")
	}

	// Toggle again to un-zoom
	pressAlt(t, m, "z")
	if !m.showSidebar || !m.showMain || !m.showEditor {
		t.Errorf("after second Alt+Z: sidebar=%v, main=%v, editor=%v, want true, true, true",
			m.showSidebar, m.showMain, m.showEditor)
	}
	if m.maximized {
		t.Error("maximized flag should be false")
	}

	// 2. Zoom Editor pane
	m.setFocus(screens.FocusEditor)
	pressAlt(t, m, "z")
	if m.showSidebar || m.showMain || !m.showEditor {
		t.Errorf("after Alt+Z on editor: sidebar=%v, main=%v, editor=%v, want false, false, true",
			m.showSidebar, m.showMain, m.showEditor)
	}

	// Toggle again to un-zoom
	pressAlt(t, m, "z")
	if !m.showSidebar || !m.showMain || !m.showEditor {
		t.Errorf("after un-zoom editor: sidebar=%v, main=%v, editor=%v, want true, true, true",
			m.showSidebar, m.showMain, m.showEditor)
	}

	// 3. Zoom with custom initial state (sidebar was hidden before zoom)
	pressAlt(t, m, "b") // hide sidebar
	if m.showSidebar {
		t.Fatal("setup: sidebar should be hidden")
	}
	m.setFocus(screens.FocusEditor)
	pressAlt(t, m, "z") // zoom editor
	if m.showSidebar || m.showMain || !m.showEditor {
		t.Errorf("zoomed editor: sidebar=%v, main=%v, editor=%v",
			m.showSidebar, m.showMain, m.showEditor)
	}

	pressAlt(t, m, "z") // un-zoom
	if m.showSidebar || !m.showMain || !m.showEditor {
		t.Errorf("restored custom layout: sidebar=%v (want false), main=%v (want true), editor=%v (want true)",
			m.showSidebar, m.showMain, m.showEditor)
	}
}

func TestModel_SaveAndDeleteSavedConnection(t *testing.T) {
	t.Setenv("PICKLOCK_CONFIG_DIR", t.TempDir())

	m := newModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	pressKey(t, m, "tab") // File field
	press(t, m, "/data/app.db")
	pressKey(t, m, "ctrl+s") // save

	saved, err := conn.LoadSaved()
	if err != nil {
		t.Fatalf("LoadSaved: %v", err)
	}
	if len(saved) != 1 || saved[0].Path != "/data/app.db" {
		t.Fatalf("saved = %+v, want one /data/app.db", saved)
	}

	// sqlite: File(1), Read-only(2) -> saved button(3); enter opens modal, "d" deletes it
	pressKey(t, m, "tab")
	pressKey(t, m, "tab")
	pressKey(t, m, "enter")
	press(t, m, "d")

	saved, err = conn.LoadSaved()
	if err != nil {
		t.Fatalf("LoadSaved after delete: %v", err)
	}
	if len(saved) != 0 {
		t.Errorf("saved = %+v, want 0 after delete", saved)
	}
}

func TestCorrectSize_GuardsAgainstHugeReportedSizes(t *testing.T) {
	old := termGetSize
	termGetSize = func(uintptr) (int, int, error) { return 120, 30, nil }
	t.Cleanup(func() { termGetSize = old })

	cases := []struct{ inW, inH, wantW, wantH int }{
		{120, 30, 120, 30},   // normal size passes through
		{120, 9001, 120, 30}, // conhost buffer height -> real viewport
		{9001, 120, 120, 30}, // conhost buffer width -> real viewport
		{0, 0, 120, 30},      // bogus -> re-query
	}
	for _, tc := range cases {
		w, h := correctSize(tc.inW, tc.inH)
		if w != tc.wantW || h != tc.wantH {
			t.Errorf("correctSize(%d,%d) = (%d,%d), want (%d,%d)",
				tc.inW, tc.inH, w, h, tc.wantW, tc.wantH)
		}
	}
}

func TestCorrectSize_ClampsWhenRequeryFails(t *testing.T) {
	old := termGetSize
	termGetSize = func(uintptr) (int, int, error) { return 0, 0, errors.New("no tty") }
	t.Cleanup(func() { termGetSize = old })

	w, h := correctSize(0, 0)
	if w < 10 || h < 3 {
		t.Errorf("correctSize(0,0) = (%d,%d), want sane minimums", w, h)
	}
}

func TestDefaultName(t *testing.T) {
	cfg := conn.ConnectionConfig{Driver: conn.DriverSQLite, Path: "/data/app.db"}
	if got := defaultName(cfg); got != "/data/app.db" {
		t.Errorf("sqlite defaultName = %q, want /data/app.db", got)
	}
	cfg = conn.ConnectionConfig{Driver: conn.DriverPostgres, Host: "localhost", Port: 5432, Database: "test"}
	if got := defaultName(cfg); got != "localhost:5432/test" {
		t.Errorf("postgres defaultName = %q, want localhost:5432/test (port must not collide)", got)
	}
	// two engines on different ports with the same host+db must not collide
	a := conn.ConnectionConfig{Driver: conn.DriverPostgres, Host: "localhost", Port: 5432, Database: "test"}
	b := conn.ConnectionConfig{Driver: conn.DriverMySQL, Host: "localhost", Port: 3306, Database: "test"}
	if defaultName(a) == defaultName(b) {
		t.Error("defaultName must include the port so different ports do not collide")
	}
}

func TestModel_ConnectToPostgres(t *testing.T) {
	host := os.Getenv("PICKLOCK_TEST_POSTGRES_HOST")
	if host == "" {
		t.Skip("PICKLOCK_TEST_POSTGRES_HOST not set")
	}

	m := newModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// Connect through the full form: postgres engine, host, user, pass, database
	pressKey(t, m, "right") // sqlite -> postgres
	pressKey(t, m, "tab")   // focus: File (hidden) -> Host
	press(t, m, host)
	pressKey(t, m, "tab") // Port
	pressKey(t, m, "tab") // User
	press(t, m, os.Getenv("PICKLOCK_TEST_POSTGRES_USER"))
	pressKey(t, m, "tab") // Password
	press(t, m, os.Getenv("PICKLOCK_TEST_POSTGRES_PASSWORD"))
	pressKey(t, m, "tab") // Database
	press(t, m, os.Getenv("PICKLOCK_TEST_POSTGRES_DATABASE"))
	pressKey(t, m, "enter")

	if m.screen != ScreenWorkspace {
		t.Fatalf("screen = %d, want ScreenWorkspace. connectErr=%q", m.screen, m.connect.Error())
	}
	if m.browser == nil || len(m.browser.Tables) == 0 {
		t.Fatalf("browser without tables")
	}
	_ = m.View()
}

// blockingStore blocks Browse until its unblock channel is closed.
type blockingStore struct {
	store.DataSource
	unblock chan struct{}
}

func (s *blockingStore) Browse(ctx context.Context, req store.BrowseRequest) (store.BrowseResponse, error) {
	select {
	case <-ctx.Done():
		return store.BrowseResponse{}, ctx.Err()
	case <-s.unblock:
	}
	return s.DataSource.Browse(ctx, req)
}

// TestModel_PageNavigationRunsAsync verifies that a page change is applied in
// the background: the model reports navigating while it is in flight and the
// new page is swapped in when it lands.
func TestModel_PageNavigationRunsAsync(t *testing.T) {
	m := connect(t)
	for i := 0; i < 60; i++ {
		execSQL(t, m, fmt.Sprintf("INSERT INTO users (name, email) VALUES ('u%d','u%d@t.com')", i, i))
	}
	press(t, m, "2") // open users
	pressAlt(t, m, "2")

	unblock := make(chan struct{})
	m.store = &blockingStore{DataSource: m.store, unblock: unblock}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m = updated.(*Model)
	if !m.navigating {
		t.Fatal("navigating should be true while the page is loading")
	}

	// let the in-flight operation complete
	close(unblock)
	if out := cmd(); out != nil {
		if batch, ok := out.(tea.BatchMsg); ok {
			for _, sub := range batch {
				if sub == nil {
					continue
				}
				if subMsg := sub(); subMsg != nil {
					step(t, m, subMsg)
				}
			}
		} else {
			step(t, m, out)
		}
	}
	if m.navigating {
		t.Error("navigating should be false after the page lands")
	}
	if m.browser.Page != 1 {
		t.Errorf("Page = %d, want 1", m.browser.Page)
	}
}

// TestModel_EscCancelsNavigation verifies that Esc aborts an in-flight
// navigation and that a superseded navigation is dropped.
func TestModel_EscCancelsNavigation(t *testing.T) {
	m := connect(t)
	press(t, m, "2") // open users
	pressAlt(t, m, "2")

	unblock := make(chan struct{})
	m.store = &blockingStore{DataSource: m.store, unblock: unblock}

	// start a navigation; it blocks in the background
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m = updated.(*Model)
	if !m.navigating {
		t.Fatal("setup: navigating should be true")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(*Model)
	if m.navigating {
		t.Error("navigating should be false after Esc")
	}

	// let the cancelled op finish; its cancelled result must not move pages
	close(unblock)
	if m.browser.Page != 0 {
		t.Errorf("Page = %d, want 0 (cancelled navigation must not move pages)", m.browser.Page)
	}
}

func TestModel_ConnectIsAsync(t *testing.T) {
	m := newModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	db := createTestDB(t)
	pressKey(t, m, "tab") // focus the File field
	press(t, m, db)
	pressKey(t, m, "enter") // connect

	if m.screen != ScreenWorkspace {
		t.Fatalf("screen = %d, want ScreenWorkspace", m.screen)
	}
	if m.connecting {
		t.Error("connecting should be false after the load lands")
	}
	if m.browser == nil {
		t.Fatal("browser should be loaded")
	}
}

func TestModel_CopyQueryShortcut(t *testing.T) {
	m := connect(t)
	pressAlt(t, m, "3") // focus editor
	m.editorScreen.SetValue("SELECT * FROM users")

	pressAlt(t, m, "c")
	if m.exported != "query copied to clipboard" {
		t.Errorf("exported = %q, want 'query copied to clipboard'", m.exported)
	}

	m.exported = ""
	m.editorScreen.SetValue("")
	pressAlt(t, m, "c")
	if m.warn != "no query to copy" {
		t.Errorf("warn = %q, want 'no query to copy'", m.warn)
	}
}

func TestModel_CopyQueryIgnoredOutsideEditor(t *testing.T) {
	m := connect(t)
	m.editorScreen.SetValue("SELECT * FROM users")

	// 1. Focus Main pane (panel 2)
	pressAlt(t, m, "2")
	if m.focus != screens.FocusMain {
		t.Fatalf("focus = %v, want FocusMain", m.focus)
	}

	pressAlt(t, m, "c")
	if m.exported != "" || m.warn != "" || m.err != "" {
		t.Errorf("Alt+C in FocusMain should be ignored, got exported=%q, warn=%q, err=%q",
			m.exported, m.warn, m.err)
	}

	// 2. Focus Sidebar pane (panel 1)
	pressAlt(t, m, "1")
	if m.focus != screens.FocusSidebar {
		t.Fatalf("focus = %v, want FocusSidebar", m.focus)
	}

	pressAlt(t, m, "c")
	if m.exported != "" || m.warn != "" || m.err != "" {
		t.Errorf("Alt+C in FocusSidebar should be ignored, got exported=%q, warn=%q, err=%q",
			m.exported, m.warn, m.err)
	}
}

func TestModel_FlashMessageAutoClear(t *testing.T) {
	m := connect(t)

	// Test warning auto-clear
	m.setWarn("transient warning")
	if m.warn != "transient warning" {
		t.Errorf("warn = %q, want 'transient warning'", m.warn)
	}
	// Simulate timer tick
	step(t, m, clearMessageMsg{seq: m.msgSeq})
	if m.warn != "" {
		t.Errorf("warn should be cleared after timer tick, got: %q", m.warn)
	}

	// Test success/exported auto-clear
	m.setSuccess("transient success")
	if m.exported != "transient success" {
		t.Errorf("exported = %q, want 'transient success'", m.exported)
	}
	step(t, m, clearMessageMsg{seq: m.msgSeq})
	if m.exported != "" {
		t.Errorf("exported should be cleared after timer tick, got: %q", m.exported)
	}

	// Test error auto-clear
	m.setError("transient error")
	if m.err != "transient error" {
		t.Errorf("err = %q, want 'transient error'", m.err)
	}
	step(t, m, clearMessageMsg{seq: m.msgSeq})
	if m.err != "" {
		t.Errorf("err should be cleared after timer tick, got: %q", m.err)
	}

	// Stale timer sequence should NOT clear a newer message
	m.setWarn("new warning")
	staleSeq := m.msgSeq - 1
	step(t, m, clearMessageMsg{seq: staleSeq})
	if m.warn != "new warning" {
		t.Errorf("stale timer cleared newer message: warn = %q", m.warn)
	}
}

func TestModel_MouseDragSelectQuery(t *testing.T) {
	m := connect(t)
	m.editorScreen.SetValue("SELECT id, name\nFROM users")

	// Drag inside editor textarea (content starts at wy=18 -> msg.Y=20):
	step(t, m, mouseMsg(30, 20, tea.MouseButtonLeft, tea.MouseActionPress))
	step(t, m, mouseMsg(36, 20, tea.MouseButtonLeft, tea.MouseActionMotion))
	step(t, m, mouseMsg(36, 20, tea.MouseButtonLeft, tea.MouseActionRelease))

	if m.exported != "selection copied to clipboard" {
		t.Errorf("exported = %q, want 'selection copied to clipboard'", m.exported)
	}
}

func TestModel_HorizontalScroll_LeftRightKeysAndMouse(t *testing.T) {
	m := connect(t)
	// Select 'users' table which has 3 columns: id, name, email
	pressAlt(t, m, "1") // focus sidebar
	pressKey(t, m, "down")
	pressKey(t, m, "enter")
	if m.browser.ActiveTable != "users" {
		t.Fatalf("setup: ActiveTable = %q, want users", m.browser.ActiveTable)
	}

	// Focus main pane
	pressAlt(t, m, "2")
	if m.focus != screens.FocusMain {
		t.Fatalf("focus = %v, want FocusMain", m.focus)
	}

	if m.colScroll != 0 {
		t.Errorf("initial colScroll = %d, want 0", m.colScroll)
	}

	// Pressing left when at 0 should stay at 0
	pressKey(t, m, "left")
	if m.colScroll != 0 {
		t.Errorf("colScroll = %d, want 0 (clamped left)", m.colScroll)
	}

	// Pressing right advances colScroll
	pressKey(t, m, "right")
	if m.colScroll != 1 {
		t.Errorf("colScroll = %d, want 1 after right arrow", m.colScroll)
	}

	// Pressing right again
	pressKey(t, m, "right")
	if m.colScroll != 2 {
		t.Errorf("colScroll = %d, want 2 after second right arrow", m.colScroll)
	}

	// Pressing left goes back
	pressKey(t, m, "left")
	if m.colScroll != 1 {
		t.Errorf("colScroll = %d, want 1 after left arrow", m.colScroll)
	}

	// Horizontal wheel right over main pane (wx=40, wy=5)
	step(t, m, mouseMsg(40, 5, tea.MouseButtonWheelRight, tea.MouseActionPress))
	if m.colScroll != 2 {
		t.Errorf("colScroll = %d, want 2 after MouseButtonWheelRight", m.colScroll)
	}

	// Horizontal wheel left over main pane
	step(t, m, mouseMsg(40, 5, tea.MouseButtonWheelLeft, tea.MouseActionPress))
	if m.colScroll != 1 {
		t.Errorf("colScroll = %d, want 1 after MouseButtonWheelLeft", m.colScroll)
	}

	// Switching tables resets colScroll to 0
	pressAlt(t, m, "1") // focus sidebar
	pressKey(t, m, "up")
	pressKey(t, m, "enter")
	if m.colScroll != 0 {
		t.Errorf("colScroll = %d, want 0 after opening new table", m.colScroll)
	}
}

func TestModel_EditorHorizontalScroll_KeysAndMouse(t *testing.T) {
	m := connect(t)
	pressAlt(t, m, "3") // focus editor
	if m.focus != screens.FocusEditor {
		t.Fatalf("focus = %v, want FocusEditor", m.focus)
	}

	m.editorScreen.SetValue("SELECT id, name, email, name AS extra1, email AS extra2 FROM users")
	pressKey(t, m, "ctrl+r") // run query

	if m.editor == nil || m.editor.Data == nil {
		t.Fatal("query should have executed and returned data")
	}

	if m.editorScreen.ColScroll() != 0 {
		t.Errorf("initial editor colScroll = %d, want 0", m.editorScreen.ColScroll())
	}

	// Pressing Alt+Left at col 0 should stay at 0
	step(t, m, tea.KeyMsg{Type: tea.KeyLeft, Alt: true})
	if m.editorScreen.ColScroll() != 0 {
		t.Errorf("editor colScroll = %d, want 0", m.editorScreen.ColScroll())
	}

	// Pressing Alt+Right advances editor colScroll
	step(t, m, tea.KeyMsg{Type: tea.KeyRight, Alt: true})
	if m.editorScreen.ColScroll() != 1 {
		t.Errorf("editor colScroll = %d, want 1 after Alt+Right", m.editorScreen.ColScroll())
	}

	// Pressing Alt+Right again advances
	step(t, m, tea.KeyMsg{Type: tea.KeyRight, Alt: true})
	if m.editorScreen.ColScroll() != 2 {
		t.Errorf("editor colScroll = %d, want 2 after second Alt+Right", m.editorScreen.ColScroll())
	}

	// Pressing Alt+Left goes back
	step(t, m, tea.KeyMsg{Type: tea.KeyLeft, Alt: true})
	if m.editorScreen.ColScroll() != 1 {
		t.Errorf("editor colScroll = %d, want 1 after Alt+Left", m.editorScreen.ColScroll())
	}

	// Horizontal wheel right over editor results pane (wy=24)
	step(t, m, mouseMsg(40, 24, tea.MouseButtonWheelRight, tea.MouseActionPress))
	if m.editorScreen.ColScroll() != 2 {
		t.Errorf("editor colScroll = %d, want 2 after MouseButtonWheelRight", m.editorScreen.ColScroll())
	}

	// Horizontal wheel left over editor results pane
	step(t, m, mouseMsg(40, 24, tea.MouseButtonWheelLeft, tea.MouseActionPress))
	if m.editorScreen.ColScroll() != 1 {
		t.Errorf("editor colScroll = %d, want 1 after MouseButtonWheelLeft", m.editorScreen.ColScroll())
	}

	// Running a new query resets colScroll to 0
	m.editorScreen.SetValue("SELECT id FROM users")
	pressKey(t, m, "ctrl+r")
	if m.editorScreen.ColScroll() != 0 {
		t.Errorf("editor colScroll = %d, want 0 after running new query", m.editorScreen.ColScroll())
	}
}
