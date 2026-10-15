package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/agmonetti/picklock/internal/browser"
	"github.com/agmonetti/picklock/internal/conn"
	"github.com/agmonetti/picklock/internal/store"
	"github.com/agmonetti/picklock/internal/tui/styles"
)

func browseSignature(item *browser.PreparedItem) string {
	if item == nil || item.Structure == nil {
		return ""
	}
	rel, ok := item.Structure.(*store.RelationalStructure)
	if !ok {
		return item.Structure.Title()
	}
	var b strings.Builder
	for _, col := range rel.Columns {
		fmt.Fprintf(&b, "c:%s:%s:%t:%t:%t;", col.Name, col.Type, col.NotNull, col.PK, col.Clustering)
	}
	for _, idx := range rel.Indexes {
		fmt.Fprintf(&b, "i:%s:%t:%t:%s;", idx.Name, idx.Unique, idx.Partial, strings.Join(idx.Columns, ","))
	}
	return b.String()
}

type browseDialog int

const (
	browseDialogNone browseDialog = iota
	browseDialogNotice
	browseDialogOptions
)

func (m *Model) heavyTypes() []string {
	if m.browsePrepared == nil {
		return nil
	}
	rel, ok := m.browsePrepared.Structure.(*store.RelationalStructure)
	if !ok {
		return nil
	}
	var found []string
	for _, col := range rel.Columns {
		t := strings.ToLower(col.Type)
		potentiallyLarge := strings.Contains(t, "text") || strings.Contains(t, "blob") ||
			strings.Contains(t, "binary") || strings.Contains(t, "bytea") ||
			strings.Contains(t, "json") || strings.Contains(t, "xml") ||
			strings.Contains(t, "clob") || strings.Contains(t, "image")
		// SQL Server exposes NVARCHAR(MAX), including JSON-in-text columns, as
		// the base type "nvarchar"; the current metadata contract omits length.
		if m.store != nil && m.store.Driver() == conn.DriverMSSQL &&
			(t == "nvarchar" || t == "varchar") {
			potentiallyLarge = true
		}
		if potentiallyLarge {
			found = append(found, col.Name+" · "+col.Type)
		}
	}
	return found
}

func (m *Model) openBrowseOptions() {
	m.err = ""
	m.browseDialog = browseDialogOptions
	m.browseFormFocus = 4
	m.browseColumnCursor = 0
	m.browseColumns = make([]bool, len(m.browsePreparedColumns()))
	m.browseFilterColumn = 0
	m.browseFilterEnabled = false
	m.browseFilterOperator = 0
	m.browseFilterInput.SetValue("")
	m.browseFilterInput.Blur()
	m.browsePageInput.SetValue(strconv.Itoa(m.prefs.BrowsePageSize))
	m.browsePageInput.Focus()
}

func (m *Model) defaultBrowseOptions() store.BrowseOptions {
	if m.browsePrepared != nil {
		if _, ok := m.browsePrepared.Structure.(*store.RelationalStructure); ok {
			return store.BrowseOptions{PreviewBytes: m.prefs.PreviewBytes}
		}
	}
	return store.BrowseOptions{}
}

func (m *Model) browsePreparedColumns() []store.Column {
	if m.browsePrepared == nil {
		return nil
	}
	if rel, ok := m.browsePrepared.Structure.(*store.RelationalStructure); ok {
		return rel.Columns
	}
	return nil
}

var browseOperators = []store.FilterOperator{store.FilterEqual, store.FilterNotEqual, store.FilterLess, store.FilterGreater, store.FilterIsNull}

func (m *Model) handleBrowseDialogKey(msg tea.KeyMsg) tea.Cmd {
	if m.browseDialog == browseDialogNotice {
		switch msg.String() {
		case "esc", "c":
			m.cancelBrowseDialog()
		case "e":
			return m.applyPreparedBrowse(m.defaultBrowseOptions(), m.prefs.BrowsePageSize, true)
		case "a":
			m.openBrowseOptions()
		}
		return nil
	}
	if m.browseDialog != browseDialogOptions {
		return nil
	}
	cols := m.browsePreparedColumns()
	if msg.Type == tea.KeyEsc {
		m.cancelBrowseDialog()
		return nil
	}
	if msg.Type == tea.KeyTab {
		m.browseFilterInput.Blur()
		m.browsePageInput.Blur()
		m.browseFormFocus = (m.browseFormFocus + 1) % 5
		switch m.browseFormFocus {
		case 3:
			m.browseFilterInput.Focus()
		case 4:
			m.browsePageInput.Focus()
		}
		return nil
	}
	switch msg.String() {
	case "up", "k":
		if m.browseFormFocus == 0 && m.browseColumnCursor > 0 {
			m.browseColumnCursor--
		}
		if m.browseFormFocus == 1 && m.browseFilterColumn > 0 {
			m.browseFilterColumn--
		}
	case "down", "j":
		if m.browseFormFocus == 0 && m.browseColumnCursor+1 < len(cols) {
			m.browseColumnCursor++
		}
		if m.browseFormFocus == 1 && m.browseFilterColumn+1 < len(cols) {
			m.browseFilterColumn++
		}
	case " ":
		if m.browseFormFocus == 0 && m.browseColumnCursor < len(m.browseColumns) {
			m.browseColumns[m.browseColumnCursor] = !m.browseColumns[m.browseColumnCursor]
		}
		if m.browseFormFocus == 2 {
			m.browseFilterOperator = (m.browseFilterOperator + 1) % len(browseOperators)
		}
		if m.browseFormFocus == 3 {
			m.browseFilterEnabled = !m.browseFilterEnabled
		}
	case "enter":
		return m.submitBrowseOptions()
	}
	if m.browseFormFocus == 4 {
		var cmd tea.Cmd
		m.browsePageInput, cmd = m.browsePageInput.Update(msg)
		return cmd
	}
	if m.browseFormFocus == 3 {
		var cmd tea.Cmd
		m.browseFilterInput, cmd = m.browseFilterInput.Update(msg)
		return cmd
	}
	return nil
}

func (m *Model) submitBrowseOptions() tea.Cmd {
	pageSize, err := strconv.Atoi(strings.TrimSpace(m.browsePageInput.Value()))
	if err != nil || pageSize < 1 {
		m.err = "page size must be a positive integer"
		return nil
	}
	cols := m.browsePreparedColumns()
	selected := make([]string, 0, len(cols))
	for i, yes := range m.browseColumns {
		if yes {
			selected = append(selected, cols[i].Name)
		}
	}
	if len(selected) == len(cols) {
		selected = nil
	}
	opts := m.defaultBrowseOptions()
	opts.Columns = selected
	if m.browseFilterEnabled {
		if len(cols) == 0 || m.browseFilterColumn >= len(cols) {
			m.err = "select a filter column"
			return nil
		}
		op := browseOperators[m.browseFilterOperator]
		f := store.BrowseFilter{Column: cols[m.browseFilterColumn].Name, Operator: op}
		if op != store.FilterIsNull {
			value := strings.TrimSpace(m.browseFilterInput.Value())
			if value == "" {
				m.err = "filter value is required"
				return nil
			}
			f.Value, err = parseBrowseFilterValue(cols[m.browseFilterColumn].Type, value)
			if err != nil {
				m.err = err.Error()
				return nil
			}
		}
		opts.Filters = []store.BrowseFilter{f}
	}
	m.err = ""
	return m.applyPreparedBrowse(opts, pageSize, true)
}

func parseBrowseFilterValue(typeName, raw string) (store.TypedValue, error) {
	base := strings.ToLower(strings.TrimSpace(typeName))
	if i := strings.IndexByte(base, '('); i >= 0 {
		base = base[:i]
	}
	unsigned := strings.Contains(base, "unsigned")
	base = strings.TrimSuffix(base, " unsigned")
	switch base {
	case "tinyint", "smallint", "mediumint", "int", "integer", "bigint",
		"int2", "int4", "int8", "serial", "smallserial", "bigserial":
		if unsigned {
			v, err := strconv.ParseUint(raw, 10, 64)
			if err != nil {
				return store.TypedValue{}, fmt.Errorf("invalid unsigned integer %q", raw)
			}
			return store.TypedValue{Value: v}, nil
		}
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return store.TypedValue{}, fmt.Errorf("invalid integer %q", raw)
		}
		return store.TypedValue{Value: v}, nil
	case "real", "float", "double", "float4", "float8", "decimal", "numeric":
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return store.TypedValue{}, fmt.Errorf("invalid number %q", raw)
		}
		return store.TypedValue{Value: v}, nil
	case "bool", "boolean", "bit":
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return store.TypedValue{}, fmt.Errorf("invalid boolean %q", raw)
		}
		return store.TypedValue{Value: v}, nil
	default:
		return store.TypedValue{Value: raw}, nil
	}
}

func (m *Model) refreshPrepared(item *browser.PreparedItem) tea.Cmd {
	m.browseRefreshCurrent = false
	return m.runBrowserOp(func(b *browser.Browser, st store.DataSource, ctx context.Context) error {
		b.Structure = item.Structure
		if rel, ok := item.Structure.(*store.RelationalStructure); ok {
			b.Columns, b.Indexes = rel.Columns, rel.Indexes
		}
		return b.Refresh(ctx, st)
	})
}

func (m *Model) applyPreparedBrowse(opts store.BrowseOptions, pageSize int, accepted bool) tea.Cmd {
	if m.browsePrepared == nil {
		return nil
	}
	prepared := m.browsePrepared
	m.browseOpenIndex, m.browseOpenPending = m.browseTargetIndex, !m.browseInitial && (m.browser == nil || prepared.Name != m.browser.ActiveTable)
	m.browseRefreshCurrent = false
	m.pendingBrowseAcceptance = ""
	if accepted {
		m.pendingBrowseAcceptance = browseSignature(prepared)
	}
	m.clearBrowseDialog()
	return m.runBrowserOp(func(b *browser.Browser, st store.DataSource, ctx context.Context) error {
		return b.OpenPrepared(ctx, prepared, opts, pageSize, st)
	})
}

func (m *Model) cancelBrowseDialog() {
	m.clearBrowseDialog()
	m.browseForceOptions = false
	m.sidebarCursor = m.browsePreviousIndex
}

func (m *Model) clearBrowseDialog() {
	m.browseDialog = browseDialogNone
	m.browsePrepared = nil
	m.browseInitial = false
	m.browseFilterInput.Blur()
	m.browsePageInput.Blur()
}

func (m *Model) renderBrowseDialog(width int) string {
	var b strings.Builder
	if m.browseDialog == browseDialogNotice {
		fmt.Fprintf(&b, "%s\n\nTable %q contains potentially heavy columns:\n", styles.StyleHeader.Render("Explore table"), m.browsePrepared.Name)
		for _, col := range m.heavyTypes() {
			fmt.Fprintf(&b, "  %s\n", col)
		}
		b.WriteString("\n[e] Explore safely   [a] Narrow columns / filter   [c] Cancel")
		return styles.StyleOuterMargin.Render(b.String())
	}
	cols := m.browsePreparedColumns()
	fmt.Fprintf(&b, "%s · %s\n\n", styles.StyleHeader.Render("Browse options"), m.browsePrepared.Name)
	b.WriteString("Columns (space toggles; none selected means all):\n")
	for i, col := range cols {
		mark := "[ ]"
		if m.browseColumns[i] {
			mark = "[x]"
		}
		cursor := "  "
		if i == m.browseColumnCursor {
			cursor = "> "
		}
		fmt.Fprintf(&b, "%s%s %s (%s)\n", cursor, mark, col.Name, col.Type)
	}
	if len(cols) == 0 {
		b.WriteString("  Column selection is unavailable for this source.\n")
	}
	fmt.Fprintf(&b, "\nFilter column: %s   operator: %s   enabled: %t\n", browseFilterColumnName(cols, m.browseFilterColumn), browseOperators[m.browseFilterOperator], m.browseFilterEnabled)
	fmt.Fprintf(&b, "Filter value: %s\n", m.browseFilterInput.View())
	fmt.Fprintf(&b, "Rows per page: %s\n\n", m.browsePageInput.View())
	b.WriteString("Tab changes control · ↑/↓ choose · space toggle/cycle · Enter apply · Esc cancel")
	return styles.StyleOuterMargin.Render(b.String())
}

func browseFilterColumnName(cols []store.Column, i int) string {
	if i < 0 || i >= len(cols) {
		return "(none)"
	}
	return cols[i].Name
}
func (m *Model) handleBrowseDialogMouse(msg tea.MouseMsg) tea.Cmd {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return nil
	}
	if m.browseDialog == browseDialogNotice {
		if msg.Y-2 != 4+len(m.heavyTypes()) {
			return nil
		}
		switch {
		case msg.X < m.width/3:
			return m.applyPreparedBrowse(m.defaultBrowseOptions(), m.prefs.BrowsePageSize, true)
		case msg.X < 2*m.width/3:
			m.openBrowseOptions()
		default:
			m.cancelBrowseDialog()
		}
		return nil
	}
	cols := m.browsePreparedColumns()
	rows, extra := len(cols), 0
	if rows == 0 {
		rows, extra = 1, 1
	}
	y := msg.Y - 2
	switch {
	case y >= 3 && y < 3+len(cols):
		m.browseColumnCursor = y - 3
		m.browseColumns[m.browseColumnCursor] = !m.browseColumns[m.browseColumnCursor]
		m.browseFormFocus = 0
	case y == 4+rows+extra:
		m.browseFormFocus = 1
	case y == 5+rows+extra:
		m.browseFormFocus = 3
		m.browsePageInput.Blur()
		m.browseFilterInput.Focus()
	case y == 6+rows+extra:
		m.browseFormFocus = 4
		m.browseFilterInput.Blur()
		m.browsePageInput.Focus()
	case y == 8+rows+extra:
		if msg.X < m.width/2 {
			return m.submitBrowseOptions()
		}
		m.cancelBrowseDialog()
	}
	return nil
}
