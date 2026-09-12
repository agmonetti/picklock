// Package screens contains the TUI screens.
package screens

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/agmonetti/picklock/internal/conn"
	"github.com/agmonetti/picklock/internal/tui/styles"
)

// ConnectMsg is emitted when the user asks to connect.
type ConnectMsg struct {
	Cfg conn.ConnectionConfig
}

// SaveConnectionMsg is emitted when the user saves the current connection.
type SaveConnectionMsg struct {
	Cfg conn.ConnectionConfig
}

// DeleteConnectionMsg is emitted when the user deletes a saved connection.
type DeleteConnectionMsg struct {
	Name string
}

// logoASCII is the connect screen logo (figlet "picklock", standard/blocks font).
const logoASCII = `       _       _     _             _     
      (_)     | |   | |           | |    
 ____  _  ____| |  _| | ___   ____| |  _ 
|  _ \| |/ ___) |_/ ) |/ _ \ / ___) |_/ )
| |_| | ( (___|  _ (| | |_| ( (___|  _ ( 
|  __/|_|\____)_| \_)\_)___/ \____)_| \_)
|_|                                      `

// field is a form label + input. If isToggle is true, the field is a boolean
// (checkbox) and input is not used.
type field struct {
	label    string
	input    textinput.Model
	isToggle bool
	checked  bool
}

// ConnScreen is the state of the connection form. The engine is selected with
// ←/→ when the focus is on the selector; Tab moves across the fields.
type ConnScreen struct {
	driverIdx int
	saved     []conn.SavedConnection
	savedIdx  int
	focus     int // 0 = engine, 1..len(fields) = fields, len(fields)+1 = saved button (if saved > 0)
	fields    []field
	err       string

	showSaved bool // whether the saved connections modal overlay is open

	width  int
	height int

	// clicks holds the clickable rows of the last View render, so the mouse
	// handler and the renderer agree on the same geometry
	clicks []clickable
}

// NewConnScreen creates the screen with the saved connections loaded.
func NewConnScreen(saved []conn.SavedConnection) *ConnScreen {
	c := &ConnScreen{saved: saved}
	c.rebuildFields()
	c.applyFocus()
	return c
}

// fieldsVisible returns the fields for the selected engine.
func (c *ConnScreen) fieldsVisible() []*field {
	drv := c.driver()
	var out []*field
	for i := range c.fields {
		f := &c.fields[i]
		switch f.label {
		case "File":
			if drv == conn.DriverSQLite {
				out = append(out, f)
			}
		case "Read-only":
			// available for every engine: SQLite opens mode=ro, the network
			// engines enforce it at the session level (see the stores)
			out = append(out, f)
		case "SSL":
			if drv != conn.DriverSQLite {
				out = append(out, f)
			}
		default: // Host, Port, User, Password, Database
			if drv != conn.DriverSQLite {
				out = append(out, f)
			}
		}
	}
	return out
}

// rebuildFields creates the persistent inputs (one per possible field). Values
// already typed are preserved so switching engines does not lose them.
func (c *ConnScreen) rebuildFields() {
	prev := map[string]field{}
	for _, f := range c.fields {
		prev[f.label] = f
	}
	mk := func(label, placeholder string) field {
		in := textinput.New()
		in.Cursor.BlinkSpeed = CursorBlink
		in.Placeholder = placeholder
		in.Prompt = " "
		in.Width = 24
		return field{label: label, input: in}
	}
	userPlaceholder := "postgres"
	dbPlaceholder := "mydb"
	switch c.driver() {
	case conn.DriverMySQL, conn.DriverMariaDB:
		userPlaceholder = "root"
		dbPlaceholder = "test"
	case conn.DriverMSSQL:
		userPlaceholder = "sa"
		dbPlaceholder = "master"
	case conn.DriverMongo:
		userPlaceholder = "admin"
		dbPlaceholder = "test"
	case conn.DriverRedis:
		userPlaceholder = ""
		dbPlaceholder = "0"
	case conn.DriverCassandra:
		userPlaceholder = "cassandra"
		dbPlaceholder = "system"
	case conn.DriverNeo4j:
		userPlaceholder = "neo4j"
		dbPlaceholder = "neo4j"
	}
	c.fields = []field{
		mk("File", "/data/app.db"),
		mk("Host", "localhost"),
		mk("Port", strconv.Itoa(conn.DefaultPort(c.driver()))),
		mk("User", userPlaceholder),
		mk("Password", ""),
		mk("Database", dbPlaceholder),
		{label: "Read-only", isToggle: true},
		mk("SSL", "prefer"),
	}
	for i := range c.fields {
		if p, ok := prev[c.fields[i].label]; ok {
			c.fields[i].input.SetValue(p.input.Value())
			c.fields[i].checked = p.checked
		}
	}
	// the password is masked only for network engines
	if c.driver() != conn.DriverSQLite {
		c.field("Password").input.EchoMode = textinput.EchoPassword
	}
	c.field("Port").input.Placeholder = strconv.Itoa(conn.DefaultPort(c.driver()))
	c.field("User").input.Placeholder = userPlaceholder
	c.field("Database").input.Placeholder = dbPlaceholder
}

// field returns the form field with the given label, or nil if it does not exist.
func (c *ConnScreen) field(label string) *field {
	for i := range c.fields {
		if c.fields[i].label == label {
			return &c.fields[i]
		}
	}
	return nil
}

// driver returns the selected engine.
func (c *ConnScreen) driver() conn.Driver { return conn.Drivers[c.driverIdx] }

// savedFocus is the focus index of the saved connections button.
func (c *ConnScreen) savedFocus() int { return len(c.fieldsVisible()) + 1 }

// isSavedBtnFocused reports whether the focus is on the saved connections button.
func (c *ConnScreen) isSavedBtnFocused() bool {
	return len(c.saved) > 0 && c.focus == c.savedFocus()
}

// applyFocus focuses the active input and blurs the rest.
func (c *ConnScreen) applyFocus() tea.Cmd {
	var cmds []tea.Cmd
	for i := range c.fields {
		c.fields[i].input.Blur()
	}
	vis := c.fieldsVisible()
	if idx := c.focus - 1; idx >= 0 && idx < len(vis) && !vis[idx].isToggle {
		cmds = append(cmds, vis[idx].input.Focus())
	}
	return tea.Batch(cmds...)
}

func (c *ConnScreen) totalFocus() int {
	if len(c.saved) > 0 {
		return len(c.fieldsVisible()) + 2
	}
	return len(c.fieldsVisible()) + 1
}

func (c *ConnScreen) nextFocus() tea.Cmd {
	total := c.totalFocus()
	if total == 0 {
		return nil
	}
	c.focus = (c.focus + 1) % total
	return c.applyFocus()
}

func (c *ConnScreen) prevFocus() tea.Cmd {
	total := c.totalFocus()
	if total == 0 {
		return nil
	}
	c.focus = (c.focus - 1 + total) % total
	return c.applyFocus()
}

// OpenSaved opens the saved connections modal overlay.
func (c *ConnScreen) OpenSaved() {
	if len(c.saved) > 0 {
		c.showSaved = true
		if c.savedIdx < 0 || c.savedIdx >= len(c.saved) {
			c.savedIdx = 0
		}
	}
}

// CloseSaved closes the saved connections modal overlay.
func (c *ConnScreen) CloseSaved() {
	c.showSaved = false
}

// ShowSaved reports whether the saved connections modal overlay is visible.
func (c *ConnScreen) ShowSaved() bool {
	return c.showSaved
}

func (c *ConnScreen) moveSaved(up bool) {
	n := len(c.saved)
	if n == 0 {
		return
	}
	if up {
		c.savedIdx = (c.savedIdx - 1 + n) % n
	} else {
		c.savedIdx = (c.savedIdx + 1) % n
	}
}

func (c *ConnScreen) pageSaved(delta int) {
	n := len(c.saved)
	if n == 0 {
		return
	}
	c.savedIdx += delta
	if c.savedIdx < 0 {
		c.savedIdx = 0
	}
	if c.savedIdx >= n {
		c.savedIdx = n - 1
	}
}

func (c *ConnScreen) toggleParadigm() tea.Cmd {
	prevDefault := conn.DefaultPort(c.driver())
	if conn.IsRelational(c.driver()) {
		c.driverIdx = len(conn.RelationalDrivers)
	} else {
		c.driverIdx = 0
	}
	c.rebuildFields()
	if p := c.field("Port"); p != nil {
		if v := p.input.Value(); v == "" || v == strconv.Itoa(prevDefault) {
			p.input.SetValue("")
		}
	}
	c.focus = 0
	return c.applyFocus()
}

func (c *ConnScreen) cycleDriver(right bool) tea.Cmd {
	prevDefault := conn.DefaultPort(c.driver())
	if conn.IsRelational(c.driver()) {
		n := len(conn.RelationalDrivers)
		curr := 0
		for i, d := range conn.RelationalDrivers {
			if d == c.driver() {
				curr = i
				break
			}
		}
		if right {
			curr = (curr + 1) % n
		} else {
			curr = (curr - 1 + n) % n
		}
		c.driverIdx = curr
	} else {
		n := len(conn.NonRelationalDrivers)
		curr := 0
		for i, d := range conn.NonRelationalDrivers {
			if d == c.driver() {
				curr = i
				break
			}
		}
		if right {
			curr = (curr + 1) % n
		} else {
			curr = (curr - 1 + n) % n
		}
		c.driverIdx = len(conn.RelationalDrivers) + curr
	}
	c.rebuildFields()
	// Reset the port when it still holds the previous engine's default (or is
	// empty), so switching engines lands on the new engine's default port
	// instead of a stale one. A custom port the user typed is preserved.
	if p := c.field("Port"); p != nil {
		if v := p.input.Value(); v == "" || v == strconv.Itoa(prevDefault) {
			p.input.SetValue("")
		}
	}
	c.focus = 0
	return c.applyFocus()
}

// cfg builds the current config of the form.
func (c *ConnScreen) cfg() conn.ConnectionConfig {
	val := func(label string) string {
		for i := range c.fields {
			if c.fields[i].label == label {
				return c.fields[i].input.Value()
			}
		}
		return ""
	}
	checked := func(label string) bool {
		for i := range c.fields {
			if c.fields[i].label == label && c.fields[i].isToggle {
				return c.fields[i].checked
			}
		}
		return false
	}
	cfg := conn.New(c.driver())
	cfg.Path = val("File")
	cfg.Host = val("Host")
	cfg.Port = parsePort(val("Port"), conn.DefaultPort(c.driver()))
	cfg.User = val("User")
	cfg.Password = val("Password")
	cfg.Database = val("Database")
	cfg.ReadOnly = checked("Read-only")
	cfg.SSLMode = val("SSL")
	return cfg
}

func parsePort(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 || n > 65535 {
		return def
	}
	return n
}

// validate checks the required fields for the engine.
func (c *ConnScreen) validate() error {
	cfg := c.cfg()
	if cfg.Driver == conn.DriverSQLite {
		if cfg.Path == "" {
			return fmt.Errorf("type the file path")
		}
	} else if cfg.Host == "" {
		return fmt.Errorf("type the host")
	}
	if cfg.Driver != conn.DriverSQLite {
		if p := c.field("Port").input.Value(); p != "" {
			if n, err := strconv.Atoi(p); err != nil || n <= 0 || n > 65535 {
				return fmt.Errorf("port must be a number between 1 and 65535")
			}
		}
	}
	if cfg.SSLMode != "" {
		switch cfg.Driver {
		case conn.DriverPostgres:
			switch cfg.SSLMode {
			case "prefer", "require", "verify-ca", "verify-full", "disable":
			default:
				return fmt.Errorf("ssl: use prefer, require, verify-ca, verify-full or disable")
			}
		default:
			// MySQL/MariaDB/SQL Server expose a simpler set
			switch cfg.SSLMode {
			case "prefer", "require", "disable":
			default:
				return fmt.Errorf("ssl: use prefer, require or disable")
			}
		}
	}
	return nil
}

// connectCmd builds the connection message.
func (c *ConnScreen) connectCmd() tea.Cmd {
	cfg := c.cfg()
	return func() tea.Msg { return ConnectMsg{Cfg: cfg} }
}

func (c *ConnScreen) connect() (tea.Cmd, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}
	return c.connectCmd(), nil
}

// Update handles keys and resizing.
func (c *ConnScreen) Update(msg tea.Msg) (*ConnScreen, tea.Cmd) {
	var cmds []tea.Cmd
	handled := false

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		c.width, c.height = msg.Width, msg.Height

	case tea.KeyMsg:
		if c.showSaved {
			switch {
			case msg.Type == tea.KeyEsc || msg.String() == "esc" || msg.String() == "q":
				c.CloseSaved()
				handled = true
			case msg.Type == tea.KeyUp || msg.String() == "up" || msg.String() == "k":
				c.moveSaved(true)
				handled = true
			case msg.Type == tea.KeyDown || msg.String() == "down" || msg.String() == "j":
				c.moveSaved(false)
				handled = true
			case msg.Type == tea.KeyPgUp || msg.String() == "pgup" || msg.String() == "ctrl+u":
				c.pageSaved(-5)
				handled = true
			case msg.Type == tea.KeyPgDown || msg.String() == "pgdown" || msg.String() == "ctrl+d":
				c.pageSaved(5)
				handled = true
			case msg.Type == tea.KeyHome || msg.String() == "home" || msg.String() == "g":
				c.savedIdx = 0
				handled = true
			case msg.Type == tea.KeyEnd || msg.String() == "end" || msg.String() == "G":
				if len(c.saved) > 0 {
					c.savedIdx = len(c.saved) - 1
				}
				handled = true
			case msg.Type == tea.KeyEnter || msg.String() == "enter":
				if cmd := c.savedCmd(); cmd != nil {
					cmds = append(cmds, cmd)
				}
				handled = true
			case msg.String() == "d":
				if len(c.saved) > 0 && c.savedIdx >= 0 && c.savedIdx < len(c.saved) {
					name := c.saved[c.savedIdx].Name
					cmds = append(cmds, func() tea.Msg {
						return DeleteConnectionMsg{Name: name}
					})
					handled = true
				}
			default:
				if len(msg.Runes) == 1 && msg.Runes[0] >= '1' && msg.Runes[0] <= '9' {
					digit := int(msg.Runes[0] - '1')
					if digit < len(c.saved) {
						c.savedIdx = digit
						handled = true
					}
				}
			}
			return c, tea.Batch(cmds...)
		}

		// Main Form Key Handling (c.showSaved == false)
		switch {
		case msg.Type == tea.KeyTab || msg.String() == "tab":
			cmds = append(cmds, c.nextFocus())
			handled = true
		case msg.Type == tea.KeyShiftTab || msg.String() == "shift+tab" || msg.String() == "backtab":
			cmds = append(cmds, c.prevFocus())
			handled = true
		case msg.Type == tea.KeyEnter || msg.String() == "enter":
			if c.isSavedBtnFocused() {
				c.OpenSaved()
			} else if c.focus == 0 {
				cmd, err := c.connect()
				if err != nil {
					c.err = err.Error()
				} else if cmd != nil {
					cmds = append(cmds, cmd)
				}
			} else if idx := c.focus - 1; idx >= 0 {
				vis := c.fieldsVisible()
				if idx < len(vis) && vis[idx].isToggle {
					vis[idx].checked = !vis[idx].checked
				} else {
					cmd, err := c.connect()
					if err != nil {
						c.err = err.Error()
					} else if cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
			}
			handled = true
		case msg.String() == "s" || msg.String() == "ctrl+o":
			if len(c.saved) > 0 && !c.FocusOnField() {
				c.OpenSaved()
				handled = true
			}
		case msg.String() == "left" || msg.String() == "right":
			if c.focus == 0 {
				cmds = append(cmds, c.cycleDriver(msg.String() == "right"))
				handled = true
			}
		case msg.String() == " " || msg.String() == "p" || msg.String() == "t":
			if c.focus == 0 {
				cmds = append(cmds, c.toggleParadigm())
				handled = true
			}
		case msg.String() == "up" || msg.String() == "down":
			if msg.String() == "up" {
				cmds = append(cmds, c.prevFocus())
			} else {
				cmds = append(cmds, c.nextFocus())
			}
			handled = true
		case msg.Type == tea.KeyCtrlS || msg.String() == "ctrl+s":
			if err := c.validate(); err != nil {
				c.err = err.Error()
			} else {
				cfg := c.cfg()
				cmds = append(cmds, func() tea.Msg { return SaveConnectionMsg{Cfg: cfg} })
			}
			handled = true
		case msg.String() == "r":
			if c.focus == 0 {
				c.reset()
				handled = true
			}
		}

		if !handled {
			vis := c.fieldsVisible()
			if idx := c.focus - 1; idx >= 0 && idx < len(vis) {
				f := vis[idx]
				if f.isToggle {
					if msg.String() == " " {
						f.checked = !f.checked
					}
				} else {
					updated, cmd := f.input.Update(msg)
					f.input = updated
					cmds = append(cmds, cmd)
				}
			}
		}
	}

	return c, tea.Batch(cmds...)
}

// reset clears the form and the error.
func (c *ConnScreen) reset() {
	for i := range c.fields {
		c.fields[i].input.SetValue("")
		c.fields[i].checked = false
	}
	c.field("Port").input.Placeholder = strconv.Itoa(conn.DefaultPort(c.driver()))
	c.err = ""
}

// ResetForm clears the form (for a new session).
func (c *ConnScreen) ResetForm() {
	c.reset()
	c.showSaved = false
}

// SetSaved updates the list of saved connections.
func (c *ConnScreen) SetSaved(saved []conn.SavedConnection) {
	c.saved = saved
	if len(c.saved) == 0 {
		c.savedIdx = 0
		c.showSaved = false
		if c.focus > len(c.fieldsVisible()) {
			c.focus = 0
			c.applyFocus()
		}
		return
	}
	if c.savedIdx >= len(c.saved) {
		c.savedIdx = len(c.saved) - 1
	}
	if c.savedIdx < 0 {
		c.savedIdx = 0
	}
}

// SetError shows a connection error on the screen.
func (c *ConnScreen) SetError(err string) { c.err = err }

// Error returns the current screen error.
func (c *ConnScreen) Error() string { return c.err }

// FieldValue returns the value of a form field by label.
func (c *ConnScreen) FieldValue(label string) string {
	for i := range c.fields {
		if c.fields[i].label == label {
			return c.fields[i].input.Value()
		}
	}
	return ""
}

// FocusOnField reports whether the focus is on a text input (not the engine,
// a toggle checkbox, or the saved list).
func (c *ConnScreen) FocusOnField() bool {
	if c.showSaved {
		return false
	}
	vis := c.fieldsVisible()
	idx := c.focus - 1
	return idx >= 0 && idx < len(vis) && !vis[idx].isToggle
}

// FocusIndex returns the current focus index: 0 = engine, 1..N = form fields,
// N+1 = the saved list.
func (c *ConnScreen) FocusIndex() int { return c.focus }

// clickKind identifies what a clickable zone of the connection screen does.
type clickKind int

const (
	clickNone      clickKind = iota
	clickEngine              // the engine selector
	clickField               // a form field (idx = field index, 0-based)
	clickConnect             // the "Enter · Connect" button
	clickOpenSaved           // the "Saved connections (N)" button on main form
	clickSaved               // a saved connection row in modal (idx = saved index)
)

// clickable is a row of the connection screen that reacts to a mouse click.
// y is the row in the final rendered space (after vertical centering).
type clickable struct {
	kind clickKind
	idx  int
	y    int
}

// View renders the lazyvim-style centered menu: logo on top and below the form
// with a button to open the saved connections modal overlay.
func (c *ConnScreen) View(width, height int) string {
	if width > 0 {
		c.width = width
	}
	if height > 0 {
		c.height = height
	}

	if c.showSaved && len(c.saved) > 0 {
		content, clicks := c.renderSavedModal(c.width, c.height)
		c.clicks = clicks
		return content
	}

	// the layout is built as plain lines here so the clickable rows can be
	// recorded with the exact same geometry the renderer uses
	var lines []string
	var clicks []clickable
	base := 0
	// hide the big ASCII logo if the terminal is too short to show the form
	if c.height >= 22 {
		lines = append(lines, strings.Split(renderLogo(c.width), "\n")...)
		lines = append(lines, "", "") // the \n\n after the logo
		base = 8
	}

	vis := c.fieldsVisible()
	form := strings.Split(c.renderForm(), "\n")

	// record the clickable rows by scanning the lines the form actually emits,
	// so the hit-test always agrees with the render no matter how the layout
	// of the form evolves
	for i, line := range form {
		trim := strings.TrimSpace(ansi.Strip(line))
		y := base + i
		switch {
		case strings.HasPrefix(trim, "Engine"):
			clicks = append(clicks, clickable{kind: clickEngine, y: y})
		case strings.HasPrefix(trim, "Enter · Connect"):
			clicks = append(clicks, clickable{kind: clickConnect, y: y})
		case strings.Contains(trim, "Saved connections"):
			clicks = append(clicks, clickable{kind: clickOpenSaved, y: y})
		default:
			for fi, f := range vis {
				if strings.HasPrefix(trim, f.label) {
					clicks = append(clicks, clickable{kind: clickField, idx: fi, y: y})
					break
				}
			}
		}
	}
	lines = append(lines, form...)

	if c.err != "" {
		lines = append(lines, "", "")
		lines = append(lines, strings.Split(styles.StyleError.Render(c.err), "\n")...)
	}

	// horizontal centering: every line that is not already the full width gets
	// an equal pad on both sides
	out := make([]string, len(lines))
	for i, l := range lines {
		if w := lipgloss.Width(l); w < c.width {
			out[i] = strings.Repeat(" ", (c.width-w)/2) + l
		} else {
			out[i] = l
		}
	}

	// vertical centering with a deterministic offset (matching the mouse hit-test)
	vpad := 0
	if len(out) < c.height {
		vpad = (c.height - len(out)) / 2
	}

	final := make([]string, 0, c.height)
	for i := 0; i < vpad; i++ {
		final = append(final, strings.Repeat(" ", c.width))
	}
	final = append(final, out...)
	for len(final) < c.height {
		final = append(final, strings.Repeat(" ", c.width))
	}
	if len(final) > c.height {
		final = final[:c.height]
	}

	for i := range clicks {
		clicks[i].y += vpad
	}
	c.clicks = clicks
	return strings.Join(final, "\n")
}

// HitTest returns the clickable under the given cell of the screen space, or
// clickNone when the click lands on nothing (empty rows, title, hints, ...).
func (c *ConnScreen) HitTest(x, y int) clickable {
	if x < 0 || x >= c.width || y < 0 || y >= c.height {
		return clickable{kind: clickNone, y: y}
	}
	for _, cl := range c.clicks {
		if cl.y == y {
			return cl
		}
	}
	return clickable{kind: clickNone, y: y}
}

// Activate applies a mouse click on the connection screen: it focuses the
// clicked field or engine, toggles a checkbox, or runs the connect command.
// It returns the tea.Cmd to run (connecting), or nil.
func (c *ConnScreen) Activate(k clickable) tea.Cmd {
	switch k.kind {
	case clickEngine:
		if c.focus == 0 {
			return c.toggleParadigm()
		}
		c.focus = 0
		return c.applyFocus()
	case clickField:
		c.focus = k.idx + 1
		cmd := c.applyFocus()
		if k.idx < len(c.fieldsVisible()) && c.fieldsVisible()[k.idx].isToggle {
			c.fieldsVisible()[k.idx].checked = !c.fieldsVisible()[k.idx].checked
		}
		return cmd
	case clickConnect:
		cmd, err := c.connect()
		if err != nil {
			c.err = err.Error()
			return nil
		}
		return cmd
	case clickOpenSaved:
		c.OpenSaved()
		return nil
	case clickSaved:
		c.savedIdx = k.idx
		return c.savedCmd()
	}
	return nil
}

// savedCmd connects to the currently selected saved connection.
func (c *ConnScreen) savedCmd() tea.Cmd {
	if c.savedIdx < 0 || c.savedIdx >= len(c.saved) {
		return nil
	}
	cfg := c.saved[c.savedIdx].ToConfig()
	return func() tea.Msg { return ConnectMsg{Cfg: cfg} }
}

// renderLogo renders the ASCII logo centered on the given width. The lines are
// first padded to a uniform width so lipgloss.Center does not skew them.
func renderLogo(width int) string {
	lines := strings.Split(logoASCII, "\n")
	maxW := 0
	for _, l := range lines {
		if w := runewidth.StringWidth(l); w > maxW {
			maxW = w
		}
	}
	for i, l := range lines {
		lines[i] = l + strings.Repeat(" ", maxW-runewidth.StringWidth(l))
	}
	return styles.StyleLogo.Width(width).Align(lipgloss.Center).Render(strings.Join(lines, "\n"))
}

// Fixed form widths: label on the left + input box.
const (
	fieldLabelW = 18
	fieldBoxW   = 32 // total width of every bracketed box, so the rows align
)

// boxInner is the content width inside the [ and ] delimiters of a field box.
const boxInner = fieldBoxW - 4

// renderField renders a single form field as a one-line bracketed box of a
// fixed width, so every row starts its box at the same column.
func (c *ConnScreen) renderField(f *field, focused bool) string {
	style := styles.StyleInputBox
	if focused {
		style = styles.StyleInputBoxFocus
	}
	if f.isToggle {
		mark := "[ ]"
		if f.checked {
			mark = "[x]"
		}
		return style.Render(fmt.Sprintf("%-*s", fieldBoxW, mark+" open read-only"))
	}
	// the textinput view carries ANSI (placeholder colors) that would inflate
	// a byte count; pad by display width so the bracketed box always ends up
	// exactly fieldBoxW wide
	content := f.input.View()
	if w := lipgloss.Width(content); w < boxInner {
		content += strings.Repeat(" ", boxInner-w)
	}
	return style.Render(fmt.Sprintf("[ %s ]", content))
}

func (c *ConnScreen) renderForm() string {
	var b strings.Builder
	b.WriteString(styles.StyleHeader.Render("Connect"))
	b.WriteString("\n\n")

	// engine selector
	b.WriteString(fieldRow("Engine", c.renderMotorSelector(), c.width))
	b.WriteString("\n")

	// fields, stacked without blank lines so they don't overflow on small terminals
	for i := range c.fieldsVisible() {
		f := c.fieldsVisible()[i]
		focused := c.focus == i+1
		b.WriteString(fieldRow(f.label, c.renderField(f, focused), c.width))
		b.WriteString("\n")
	}

	// buttons
	b.WriteString("\n")
	b.WriteString(styles.StyleBtnPrimary.Render("Enter · Connect"))
	b.WriteString("\n")
	b.WriteString(strings.Join([]string{
		styles.StyleBtnSecondary.Render("ctrl+s  save"),
		styles.StyleBtnSecondary.Render("r  clear"),
	}, "  "))
	b.WriteString("\n")

	// saved connections button (if any saved)
	if len(c.saved) > 0 {
		b.WriteString("\n")
		btnText := fmt.Sprintf("Saved connections (%d) · s / Enter", len(c.saved))
		if c.isSavedBtnFocused() {
			b.WriteString(styles.StyleInputBoxFocus.Render(fmt.Sprintf("> [ %s ]", btnText)))
		} else {
			b.WriteString(styles.StyleInputBox.Render(fmt.Sprintf("  [ %s ]", btnText)))
		}
		b.WriteString("\n")
	}

	return b.String()
}

// fieldRow renders a form row with the label left-aligned in a fixed column
// and the box right after it, so every label starts at the same cell and every
// box starts at the same cell. The whole label+box group is centered on the
// terminal width, keeping the form visually centered.
func fieldRow(label, box string, width int) string {
	lbl := styles.StyleFieldLabel.Width(fieldLabelW).Align(lipgloss.Left).Render(label)
	boxW := lipgloss.Width(box)
	total := fieldLabelW + 1 + boxW
	left := (width - total) / 2
	if left < 0 {
		left = 0
	}
	right := width - left - total
	if right < 0 {
		right = 0
	}
	return strings.Repeat(" ", left) + lbl + " " + box + strings.Repeat(" ", right)
}

// renderMotorSelector draws the engine selector as a focusable box with SQL/NoSQL toggle.
func (c *ConnScreen) renderMotorSelector() string {
	style := styles.StyleInputBox
	if c.focus == 0 {
		style = styles.StyleInputBoxFocus
	}
	tag := "SQL"
	if !conn.IsRelational(c.driver()) {
		tag = "NoSQL"
	}
	content := fmt.Sprintf("%-16s ←→/space", tag+": "+string(c.driver()))
	return style.Render(fmt.Sprintf("[ %-*s ]", boxInner, content))
}

// renderSavedModal renders the saved connections modal overlay.
func (c *ConnScreen) renderSavedModal(width, height int) (string, []clickable) {
	var clicks []clickable
	modalW := 74
	if width-4 < modalW {
		modalW = width - 4
	}
	if modalW < 40 {
		modalW = 40
	}

	innerW := modalW - 6 // border (2) + padding (4)
	if innerW < 34 {
		innerW = 34
	}

	n := len(c.saved)
	maxListH := 10
	if height-8 < maxListH {
		maxListH = height - 8
	}
	if maxListH < 3 {
		maxListH = 3
	}

	offset, count := ListWindow(n, c.savedIdx, maxListH)

	var b strings.Builder
	title := fmt.Sprintf("Saved Connections (%d)", n)
	if n > count {
		title += fmt.Sprintf("  (showing %d-%d of %d)", offset+1, offset+count, n)
	}
	b.WriteString(styles.StyleHeader.Render(title))
	b.WriteString("\n\n")

	nameW := 16
	drvW := 10
	targetW := innerW - 5 - nameW - drvW - 4
	if targetW < 12 {
		targetW = 12
	}

	for i := 0; i < count; i++ {
		idx := offset + i
		s := c.saved[idx]
		cfg := s.ToConfig()

		numStr := fmt.Sprintf("%2d. ", idx+1)
		nameStr := truncate(s.Name, nameW)
		namePadded := fmt.Sprintf("%-*s", nameW, nameStr)

		drvTag := fmt.Sprintf("[%s]", string(cfg.Driver))
		drvPadded := fmt.Sprintf("%-*s", drvW, drvTag)

		targetStr := cfg.Label()
		if cfg.ReadOnly {
			targetStr += " (ro)"
		}
		targetPadded := truncate(targetStr, targetW)

		rowText := fmt.Sprintf("%s%s  %s  %s", numStr, namePadded, drvPadded, targetPadded)

		if idx == c.savedIdx {
			b.WriteString(styles.StyleSidebarActive.Render("> " + rowText))
		} else {
			b.WriteString(styles.StyleSidebarItem.Render("  " + rowText))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(styles.StyleBorder.Render(strings.Repeat("─", innerW)))
	b.WriteString("\n")
	b.WriteString(styles.StyleFooter.Render("↑/↓ / j/k navigate · enter connect · d delete · esc back"))

	modalBox := styles.StylePaneFocus.Width(modalW).Padding(1, 2).Render(b.String())

	content := lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modalBox)

	boxH := lipgloss.Height(modalBox)
	topY := (height - boxH) / 2
	if topY < 0 {
		topY = 0
	}
	// items start: top border (1) + top padding (1) + header (1) + blank (1) = +4
	itemsStartY := topY + 4
	for i := 0; i < count; i++ {
		clicks = append(clicks, clickable{kind: clickSaved, idx: offset + i, y: itemsStartY + i})
	}

	return content, clicks
}
