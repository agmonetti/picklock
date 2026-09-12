package screens

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/agmonetti/picklock/internal/conn"
)

func TestConnScreen_SQLiteOnlyShowsPath(t *testing.T) {
	c := NewConnScreen(nil)
	c.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// sqlite by default: File + Read-only toggle
	if len(c.fieldsVisible()) != 2 {
		t.Fatalf("fieldsVisible = %d, want 2 (File + Read-only)", len(c.fieldsVisible()))
	}
	if c.fieldsVisible()[0].label != "File" {
		t.Errorf("field = %q, want File", c.fieldsVisible()[0].label)
	}
	if c.fieldsVisible()[1].label != "Read-only" || !c.fieldsVisible()[1].isToggle {
		t.Errorf("second field = %+v, want Read-only toggle", c.fieldsVisible()[1])
	}
}

func TestConnScreen_NetworkShowsAllFields(t *testing.T) {
	c := NewConnScreen(nil)
	c.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	c.cycleDriver(true) // sqlite -> postgres

	if len(c.fieldsVisible()) != 7 {
		t.Fatalf("fieldsVisible = %d, want 7 (Host..Database + Read-only + SSL)", len(c.fieldsVisible()))
	}
	// the Read-only toggle is now available for every engine
	ro := c.fieldsVisible()[5]
	if ro.label != "Read-only" || !ro.isToggle {
		t.Errorf("field 5 = %+v, want Read-only toggle", ro)
	}
	// password masked
	if c.fields[4].input.EchoMode != textinput.EchoPassword {
		t.Error("Password should be masked for network engines")
	}
}

func TestConnScreen_ToggleReadOnly(t *testing.T) {
	c := NewConnScreen(nil)
	c.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	c.fields[0].input.SetValue("/data/app.db")

	c.focus = 2 // Read-only toggle (field 2 of sqlite)
	c.Update(tea.KeyMsg{Type: tea.KeySpace})
	if !c.cfg().ReadOnly {
		t.Error("ReadOnly should be true after toggling")
	}
	c.Update(tea.KeyMsg{Type: tea.KeySpace})
	if c.cfg().ReadOnly {
		t.Error("ReadOnly should be false after toggling twice")
	}
}

func TestConnScreen_SSLModeInConfig(t *testing.T) {
	c := NewConnScreen(nil)
	c.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	c.cycleDriver(true) // postgres
	// SSL field is the last of postgres (index 6 in fieldsVisible)
	vis := c.fieldsVisible()
	if len(vis) != 7 || vis[6].label != "SSL" {
		t.Fatalf("expected SSL as seventh field, got %+v", vis)
	}
	vis[6].input.SetValue("require")
	if cfg := c.cfg(); cfg.SSLMode != "require" {
		t.Errorf("SSLMode = %q, want require", cfg.SSLMode)
	}
}

func TestConnScreen_ValidateSSLMode(t *testing.T) {
	c := NewConnScreen(nil)
	c.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	c.cycleDriver(true)                     // postgres
	c.fields[1].input.SetValue("localhost") // required host
	vis := c.fieldsVisible()
	vis[6].input.SetValue("bogus")
	if err := c.validate(); err == nil {
		t.Error("expected error for invalid sslmode")
	}
	vis[6].input.SetValue("verify-full")
	if err := c.validate(); err != nil {
		t.Errorf("validate = %v, want nil", err)
	}
}

func TestConnScreen_DriverCyclesAll(t *testing.T) {
	c := NewConnScreen(nil)
	seen := map[conn.Driver]bool{}
	for range conn.RelationalDrivers {
		seen[c.driver()] = true
		c.cycleDriver(true)
	}
	c.toggleParadigm()
	for range conn.NonRelationalDrivers {
		seen[c.driver()] = true
		c.cycleDriver(true)
	}
	if len(seen) != len(conn.Drivers) {
		t.Errorf("drivers seen = %d, want %d", len(seen), len(conn.Drivers))
	}
}

func TestConnScreen_ToggleParadigm(t *testing.T) {
	c := NewConnScreen(nil)
	if !conn.IsRelational(c.driver()) {
		t.Fatalf("expected initial driver %q to be relational", c.driver())
	}
	// Space when focus == 0 toggles paradigm
	c.focus = 0
	c.Update(tea.KeyMsg{Type: tea.KeySpace})
	if conn.IsRelational(c.driver()) {
		t.Fatalf("expected driver %q to be non-relational after Space toggle", c.driver())
	}
	if c.driver() != conn.DriverMongo {
		t.Errorf("expected first non-relational to be mongo, got %q", c.driver())
	}
	// Toggle back
	c.Update(tea.KeyMsg{Type: tea.KeySpace})
	if !conn.IsRelational(c.driver()) {
		t.Fatalf("expected driver %q to be relational after second toggle", c.driver())
	}
}

func TestConnScreen_FieldHelper(t *testing.T) {
	c := NewConnScreen(nil)
	c.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	if c.field("File") == nil || c.field("Read-only") == nil {
		t.Fatal("expected File and Read-only fields to exist")
	}
	if c.field("nonexistent") != nil {
		t.Fatal("expected nil for an unknown label")
	}

	// password is masked only for network engines
	if c.driver() != conn.DriverSQLite {
		if c.field("Password").input.EchoMode != textinput.EchoPassword {
			t.Error("Password should be masked for network engines")
		}
	}
}

func TestConnScreen_SSLAvailableForAllNetworkEngines(t *testing.T) {
	c := NewConnScreen(nil)
	c.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	// Check relational network engines
	for range conn.RelationalDrivers {
		if c.driver() != conn.DriverSQLite {
			found := false
			for _, f := range c.fieldsVisible() {
				if f.label == "SSL" {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s: SSL field missing", c.driver())
			}
		}
		c.cycleDriver(true)
	}
	// Check non-relational network engines
	c.toggleParadigm()
	for range conn.NonRelationalDrivers {
		found := false
		for _, f := range c.fieldsVisible() {
			if f.label == "SSL" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s: SSL field missing", c.driver())
		}
		c.cycleDriver(true)
	}
}

func TestConnScreen_DeleteSavedConnection(t *testing.T) {
	c := NewConnScreen([]conn.SavedConnection{
		{Name: "local", Driver: conn.DriverSQLite, Path: "/data/app.db"},
	})
	c.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// sqlite: engine(0) + File(1) + Read-only(2) -> saved button(3)
	for i := 0; i < 3; i++ {
		c.nextFocus()
	}
	if c.focus != c.savedFocus() {
		t.Fatalf("focus = %d, want saved %d", c.focus, c.savedFocus())
	}

	// Press Enter on the saved button to open the modal
	c.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !c.ShowSaved() {
		t.Fatal("expected saved connections modal to be open")
	}

	updated, cmd := c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	c = updated
	if cmd == nil {
		t.Fatal("expected a DeleteConnectionMsg cmd")
	}
	msg := cmd()
	if msg == nil {
		t.Fatal("expected a DeleteConnectionMsg")
	}
	dm, ok := msg.(DeleteConnectionMsg)
	if !ok || dm.Name != "local" {
		t.Errorf("msg = %#v, want DeleteConnectionMsg{Name: local}", msg)
	}
}

func TestConnScreen_Validate(t *testing.T) {
	c := NewConnScreen(nil)
	c.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// sqlite without path -> error
	if err := c.validate(); err == nil {
		t.Error("expected error: sqlite without path")
	}
	c.fields[0].input.SetValue("/data/app.db")
	if err := c.validate(); err != nil {
		t.Errorf("validate = %v, want nil", err)
	}
}

func TestConnScreen_CycleDriverUpdatesConfig(t *testing.T) {
	c := NewConnScreen(nil)
	c.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	c.cycleDriver(true) // postgres
	cfg := c.cfg()
	if cfg.Driver != conn.DriverPostgres || cfg.Port != 5432 {
		t.Errorf("cfg = %+v, want postgres:5432", cfg)
	}
}

func TestConnScreen_ValuesSurviveDriverSwitch(t *testing.T) {
	c := NewConnScreen(nil)
	c.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	c.field("File").input.SetValue("/data/app.db")
	c.field("Host").input.SetValue("db.example.com")
	c.field("Port").input.SetValue("5433")

	c.cycleDriver(true) // sqlite -> postgres
	if got := c.field("Host").input.Value(); got != "db.example.com" {
		t.Errorf("Host after switch = %q, want preserved", got)
	}
	if got := c.field("Port").input.Value(); got != "5433" {
		t.Errorf("Port after switch = %q, want preserved", got)
	}

	c.cycleDriver(false) // postgres -> sqlite
	if got := c.field("File").input.Value(); got != "/data/app.db" {
		t.Errorf("File after switch back = %q, want preserved", got)
	}
}

func TestConnScreen_ValidatePort(t *testing.T) {
	c := NewConnScreen(nil)
	c.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	c.cycleDriver(true) // postgres
	c.field("Host").input.SetValue("localhost")

	c.field("Port").input.SetValue("not-a-number")
	if err := c.validate(); err == nil {
		t.Error("expected error for a non-numeric port")
	}
	c.field("Port").input.SetValue("99999")
	if err := c.validate(); err == nil {
		t.Error("expected error for a port out of range")
	}
	c.field("Port").input.SetValue("")
	if err := c.validate(); err != nil {
		t.Errorf("empty port (use default) = %v, want nil", err)
	}
	c.field("Port").input.SetValue("5432")
	if err := c.validate(); err != nil {
		t.Errorf("valid port = %v, want nil", err)
	}
}

func TestConnScreen_SaveValidatesBeforeSaving(t *testing.T) {
	c := NewConnScreen(nil)
	c.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// empty sqlite path: ctrl+s must not emit a SaveConnectionMsg
	updated, cmd := c.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	c = updated
	if cmd != nil {
		t.Fatal("ctrl+s with invalid form must not emit a save command")
	}
	if c.err == "" {
		t.Error("expected a visible error on invalid save")
	}
}

// centerDelta returns twice the horizontal center of a line minus the ideal
// center of a `width`-wide terminal (59.5 for 120), so centered content is 0.
func centerDelta(line string, width int) (int, bool) {
	plain := ansi.Strip(line)
	trimmed := strings.TrimSpace(plain)
	if trimmed == "" {
		return 0, false
	}
	start := strings.Index(plain, trimmed)
	w := runewidth.StringWidth(trimmed)
	center := start + (w-1)/2
	return 2*center - (width - 1), true
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func TestConnScreen_ArrowsMoveCaretInField(t *testing.T) {
	c := NewConnScreen(nil)
	c.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// focus on the File field (sqlite: engine(0) + File(1))
	c.focus = 1
	c.applyFocus()
	f := c.field("File")
	f.input.SetValue("abcd")
	updated, _ := f.input.Update(tea.KeyMsg{Type: tea.KeyLeft})
	f.input = updated
	if p := f.input.Position(); p != 3 {
		t.Fatalf("left arrow on a field should move the caret, position = %d, want 3", p)
	}
	updated, _ = f.input.Update(tea.KeyMsg{Type: tea.KeyRight})
	f.input = updated
	if p := f.input.Position(); p != 4 {
		t.Fatalf("right arrow on a field should move the caret, position = %d, want 4", p)
	}
	// the screen must not swallow the arrow when a field has the focus
	c.fields[0].input = f.input
	c.focus = 1
	c.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if p := c.field("File").input.Position(); p != 3 {
		t.Errorf("ConnScreen must forward left arrow to the focused field, position = %d, want 3", p)
	}
}

func TestConnScreen_PortResetsToNewEngineDefault(t *testing.T) {
	c := NewConnScreen(nil)
	c.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// sqlite -> postgres: the port was empty (sqlite has no port), so it lands
	// on the postgres default placeholder
	c.cycleDriver(true)
	if got := c.field("Port").input.Value(); got != "" {
		t.Errorf("postgres default port value = %q, want empty (placeholder %q)",
			got, c.field("Port").input.Placeholder)
	}

	// postgres -> mysql: the port is still empty, so the mysql default applies
	c.field("Port").input.SetValue("5432") // postgres default typed
	c.cycleDriver(true)                    // -> mysql (default 3306)
	if got := c.field("Port").input.Value(); got != "" {
		t.Errorf("after cycling with the old default, port = %q, want empty", got)
	}
	c.field("Port").input.SetValue("1234") // custom port
	c.cycleDriver(true)                    // -> mariadb
	if got := c.field("Port").input.Value(); got != "1234" {
		t.Errorf("custom port should survive the switch, got %q", got)
	}
}

func TestConnScreen_ContentHorizontallyCentered(t *testing.T) {
	firstLogoLine := strings.TrimSpace(strings.Split(logoASCII, "\n")[0])
	for _, w := range []int{80, 120, 160} {
		w := w
		c := NewConnScreen(nil)
		out := c.View(w, 30)

		var logoOK bool
		var groupLeft, boxRight = -1, -1
		for _, l := range strings.Split(out, "\n") {
			plain := ansi.Strip(l)
			trimmed := strings.TrimSpace(plain)
			switch {
			case strings.HasPrefix(trimmed, firstLogoLine): // logo line 1
				d, ok := centerDelta(l, w)
				if !ok {
					continue
				}
				logoOK = true
				if d < -3 || d > 3 {
					t.Errorf("width %d: logo center off by %d cols: %q", w, d, l)
				}
			case strings.HasPrefix(trimmed, "Engine"): // the engine selector row
				// count only the leading padding; TrimSpace also strips the
				// trailing pad, which would double-count the group's width
				groupLeft = runewidth.StringWidth(plain) - runewidth.StringWidth(strings.TrimLeft(plain, " "))
				boxRight = runewidth.StringWidth(strings.TrimRight(plain, " ")) - 1
			}
		}
		if !logoOK {
			t.Fatalf("width %d: logo line not found in the connect screen", w)
		}
		if groupLeft < 0 || boxRight < 0 {
			t.Fatalf("width %d: form field row not found in the connect screen", w)
		}

		// The whole label+box group is the visual anchor: the label sits left
		// of the box and both stay on the terminal center axis.
		groupCenter2 := groupLeft + boxRight
		if d := absInt(groupCenter2 - (w - 1)); d > 1 {
			t.Errorf("width %d: form group center off by %d cols (group %d..%d)", w, d, groupLeft, boxRight)
		}
	}
}

func TestLogoLinesAreUniform(t *testing.T) {
	lines := strings.Split(logoASCII, "\n")
	w := runewidth.StringWidth(lines[0])
	for i, l := range lines {
		if got := runewidth.StringWidth(l); got != w {
			t.Errorf("logo line %d is %d cells wide, want %d (uniform, so centering does not skew)", i, got, w)
		}
	}
}

func TestConnScreen_MouseClickFocusesField(t *testing.T) {
	c := NewConnScreen(nil)
	out := c.View(80, 30)

	// verify the geometry the test relies on: sqlite = logo + File + Read-only
	if len(c.fieldsVisible()) != 2 {
		t.Fatalf("fieldsVisible = %d, want 2", len(c.fieldsVisible()))
	}
	_ = out

	// row offsets within the 80x30 render (logo shown -> engine at y=16)
	if got := c.HitTest(40, 16).kind; got != clickEngine {
		t.Errorf("row 16: kind = %v, want engine", got)
	}
	f := c.HitTest(40, 17)
	if f.kind != clickField || f.idx != 0 {
		t.Errorf("row 17: %+v, want field 0 (File)", f)
	}
	if got := c.HitTest(40, 18).kind; got != clickField {
		t.Errorf("row 18: kind = %v, want field (Read-only toggle)", got)
	}
	if got := c.HitTest(40, 20).kind; got != clickConnect {
		t.Errorf("row 20: kind = %v, want connect button", got)
	}
	if got := c.HitTest(40, 5).kind; got != clickNone {
		t.Errorf("row 5 (logo): kind = %v, want none", got)
	}

	// clicking the File field focuses it
	c.Activate(c.HitTest(40, 17))
	if c.focus != 1 {
		t.Errorf("focus = %d, want 1 (File)", c.focus)
	}

	// clicking the toggle flips the checkbox
	c.Activate(c.HitTest(40, 18))
	if !c.fieldsVisible()[1].checked {
		t.Error("Read-only should be checked after clicking it")
	}

	// clicking the engine row moves the focus back to the selector
	c.Activate(c.HitTest(40, 16))
	if c.focus != 0 {
		t.Errorf("focus = %d, want 0 (engine)", c.focus)
	}

	// clicking Connect with an empty form shows the validation error
	if cmd := c.Activate(c.HitTest(40, 20)); cmd != nil {
		t.Errorf("connect with empty form must not return a command, got %v", cmd)
	}
	if c.err == "" {
		t.Error("expected a validation error after clicking Connect on an empty form")
	}
}

func mustCmdNil(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd != nil {
		t.Errorf("unexpected command returned: %v", cmd)
	}
}

func TestConnScreen_MouseClickSavedConnects(t *testing.T) {
	c := NewConnScreen([]conn.SavedConnection{
		{Name: "dev", Driver: conn.DriverSQLite, Path: "/tmp/dev.db"},
	})
	c.View(80, 30)

	// In the main form, find the "Saved connections" button clickable row
	var openBtnRow int
	for _, cl := range c.clicks {
		if cl.kind == clickOpenSaved {
			openBtnRow = cl.y
		}
	}
	if openBtnRow == 0 {
		t.Fatal("no clickOpenSaved clickable recorded on main form")
	}

	// Click the button to open the modal
	c.Activate(c.HitTest(40, openBtnRow))
	if !c.ShowSaved() {
		t.Fatal("expected modal to be open after clicking saved button")
	}

	// In the modal, render and find the saved connection item row
	c.View(80, 30)
	var itemRow int
	for _, cl := range c.clicks {
		if cl.kind == clickSaved && cl.idx == 0 {
			itemRow = cl.y
		}
	}
	if itemRow == 0 {
		t.Fatal("no clickSaved clickable recorded in modal")
	}

	cmd := c.Activate(c.HitTest(40, itemRow))
	if cmd == nil {
		t.Fatal("clicking a saved connection in modal must return a connect command")
	}
	msg := cmd()
	if msg == nil {
		t.Fatal("connect command produced no message")
	}
	cm, ok := msg.(ConnectMsg)
	if !ok {
		t.Fatalf("message = %T, want ConnectMsg", msg)
	}
	if cm.Cfg.Path != "/tmp/dev.db" {
		t.Errorf("cfg.Path = %q, want /tmp/dev.db", cm.Cfg.Path)
	}
}

func TestConnScreen_LabelsLeftAligned(t *testing.T) {
	out := NewConnScreen(nil).View(80, 30)

	// every label text must start at the same column regardless of its length
	labelCol := -1
	for _, l := range strings.Split(out, "\n") {
		plain := ansi.Strip(l)
		trimmed := strings.TrimSpace(plain)
		for _, want := range []string{"Engine", "File", "Read-only"} {
			if strings.HasPrefix(trimmed, want+" ") || trimmed == want {
				col := runewidth.StringWidth(plain) - runewidth.StringWidth(strings.TrimLeft(plain, " "))
				if labelCol == -1 {
					labelCol = col
				} else if col != labelCol {
					t.Errorf("label %q starts at col %d, want %d", want, col, labelCol)
				}
			}
		}
	}
	if labelCol == -1 {
		t.Fatal("no label row found")
	}
}

func TestConnScreen_EnterAtEngineSelectorConnects(t *testing.T) {
	c := NewConnScreen(nil)
	c.fields[0].input.SetValue("/tmp/test.db")
	c.focus = 0 // focus on engine selector
	_, cmd := c.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("pressing Enter at focus=0 should return a connect cmd")
	}
	msg := cmd()
	cm, ok := msg.(ConnectMsg)
	if !ok {
		t.Fatalf("expected ConnectMsg, got %T", msg)
	}
	if cm.Cfg.Path != "/tmp/test.db" {
		t.Errorf("Path = %q, want /tmp/test.db", cm.Cfg.Path)
	}
}

func TestConnScreen_MultipleSavedConnectionsModal(t *testing.T) {
	savedConns := make([]conn.SavedConnection, 10)
	for i := 0; i < 10; i++ {
		savedConns[i] = conn.SavedConnection{
			Name:   fmt.Sprintf("conn-%02d", i+1),
			Driver: conn.DriverSQLite,
			Path:   fmt.Sprintf("/data/db%d.db", i+1),
		}
	}
	c := NewConnScreen(savedConns)
	c.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// Main view shows the button with total count
	mainOut := ansi.Strip(c.View(100, 30))
	if !strings.Contains(mainOut, "Saved connections (10)") {
		t.Fatalf("expected main view to show 'Saved connections (10)', got:\n%s", mainOut)
	}

	// Press 's' to open the modal
	c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if !c.ShowSaved() {
		t.Fatal("expected modal to be open after pressing 's'")
	}

	// View in modal shows the modal box with numbered items
	modalOut := ansi.Strip(c.View(100, 30))
	if !strings.Contains(modalOut, "Saved Connections (10)") {
		t.Errorf("expected modal header 'Saved Connections (10)', got:\n%s", modalOut)
	}
	if !strings.Contains(modalOut, ">  1. conn-01") {
		t.Errorf("expected active first item '>  1. conn-01', got:\n%s", modalOut)
	}

	// Down/j moves down
	c.Update(tea.KeyMsg{Type: tea.KeyDown})
	if c.savedIdx != 1 {
		t.Errorf("savedIdx = %d, want 1", c.savedIdx)
	}
	c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if c.savedIdx != 2 {
		t.Errorf("savedIdx = %d, want 2", c.savedIdx)
	}

	// Up/k moves up
	c.Update(tea.KeyMsg{Type: tea.KeyUp})
	if c.savedIdx != 1 {
		t.Errorf("savedIdx = %d, want 1", c.savedIdx)
	}
	c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if c.savedIdx != 0 {
		t.Errorf("savedIdx = %d, want 0", c.savedIdx)
	}

	// 'G' (End) moves to last item
	c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	if c.savedIdx != 9 {
		t.Errorf("savedIdx = %d, want 9 (last item)", c.savedIdx)
	}

	// 'g' (Home) moves to first item
	c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	if c.savedIdx != 0 {
		t.Errorf("savedIdx = %d, want 0", c.savedIdx)
	}

	// Number key '3' jumps to 3rd item (index 2)
	c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	if c.savedIdx != 2 {
		t.Errorf("savedIdx after '3' = %d, want 2", c.savedIdx)
	}

	// Enter connects to selected item
	_, enterCmd := c.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if enterCmd == nil {
		t.Fatal("expected enterCmd on Enter in modal")
	}
	cm, ok := enterCmd().(ConnectMsg)
	if !ok || cm.Cfg.Path != "/data/db3.db" {
		t.Fatalf("expected ConnectMsg with path /data/db3.db, got %#v", cm)
	}

	// 'd' deletes selected item
	_, delCmd := c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if delCmd == nil {
		t.Fatal("expected delCmd on 'd' in modal")
	}
	dm, ok := delCmd().(DeleteConnectionMsg)
	if !ok || dm.Name != "conn-03" {
		t.Fatalf("expected DeleteConnectionMsg for conn-03, got %#v", dm)
	}

	// 'Esc' closes the modal
	c.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if c.ShowSaved() {
		t.Fatal("expected modal to close after Esc")
	}
}

func TestConnScreen_TabToSavedButtonAndOpen(t *testing.T) {
	c := NewConnScreen([]conn.SavedConnection{
		{Name: "c1", Driver: conn.DriverSQLite, Path: "/data/c1.db"},
		{Name: "c2", Driver: conn.DriverSQLite, Path: "/data/c2.db"},
	})
	c.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// SQLite: 0: Engine -> Tab -> 1: File -> Tab -> 2: Read-only -> Tab -> 3: Saved button -> Tab -> 0: Engine
	c.Update(tea.KeyMsg{Type: tea.KeyTab}) // 1
	c.Update(tea.KeyMsg{Type: tea.KeyTab}) // 2
	c.Update(tea.KeyMsg{Type: tea.KeyTab}) // 3: Saved button

	if !c.isSavedBtnFocused() {
		t.Fatalf("expected isSavedBtnFocused() to be true at focus = %d", c.focus)
	}

	// Enter on saved button opens modal
	c.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !c.ShowSaved() {
		t.Fatal("expected modal to open after Enter on saved button")
	}

	// Close modal with 'q'
	c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if c.ShowSaved() {
		t.Fatal("expected modal to close after 'q'")
	}
}

func TestConnScreen_SetSavedClosesModalWhenEmpty(t *testing.T) {
	c := NewConnScreen([]conn.SavedConnection{
		{Name: "c1", Driver: conn.DriverSQLite, Path: "/data/c1.db"},
		{Name: "c2", Driver: conn.DriverSQLite, Path: "/data/c2.db"},
	})
	c.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	c.OpenSaved()
	c.savedIdx = 1

	// Delete c2 (1 item remains)
	c.SetSaved([]conn.SavedConnection{
		{Name: "c1", Driver: conn.DriverSQLite, Path: "/data/c1.db"},
	})
	if !c.ShowSaved() || c.savedIdx != 0 {
		t.Fatalf("expected modal open and savedIdx=0, got show=%v, idx=%d", c.ShowSaved(), c.savedIdx)
	}

	// Delete c1 (0 items remain)
	c.SetSaved(nil)
	if c.ShowSaved() {
		t.Fatal("expected modal to close automatically when saved becomes empty")
	}
}

func TestConnScreen_UpDownNavigatesFormFields(t *testing.T) {
	c := NewConnScreen(nil)
	c.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// 0: Engine -> Down -> 1: File -> Down -> 2: Read-only -> Up -> 1: File -> Up -> 0: Engine
	c.Update(tea.KeyMsg{Type: tea.KeyDown})
	if c.focus != 1 {
		t.Fatalf("focus after Down from Engine = %d, want 1", c.focus)
	}
	c.Update(tea.KeyMsg{Type: tea.KeyDown})
	if c.focus != 2 {
		t.Fatalf("focus after Down from File = %d, want 2", c.focus)
	}
	c.Update(tea.KeyMsg{Type: tea.KeyUp})
	if c.focus != 1 {
		t.Fatalf("focus after Up from Read-only = %d, want 1", c.focus)
	}
	c.Update(tea.KeyMsg{Type: tea.KeyUp})
	if c.focus != 0 {
		t.Fatalf("focus after Up from File = %d, want 0", c.focus)
	}
}
