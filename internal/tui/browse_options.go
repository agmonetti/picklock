package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/agmonetti/picklock/internal/browser"
	"github.com/agmonetti/picklock/internal/conn"
	"github.com/agmonetti/picklock/internal/store"
	"github.com/agmonetti/picklock/internal/tui/screens"
	"github.com/agmonetti/picklock/internal/tui/styles"
)

type browseClickKind int

const (
	browseClickNone browseClickKind = iota
	browseClickNoticeExplore
	browseClickNoticeNarrow
	browseClickNoticeCancel
	browseClickOptionColumn
	browseClickOptionFilterCol
	browseClickOptionFilterOp
	browseClickOptionFilterVal
	browseClickOptionPageSize
	browseClickOptionApply
	browseClickOptionCancel
)

type browseClick struct {
	kind browseClickKind
	idx  int
	y    int
	xMin int
	xMax int
}

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
	m.browseClicks = nil
}

func (m *Model) renderBrowseDialog(width, height int) string {
	if m.browseDialog == browseDialogNotice {
		return m.renderBrowseNotice(width, height)
	}
	return m.renderBrowseOptions(width, height)
}

func (m *Model) renderBrowseNotice(width, height int) string {
	modalW := 66
	if width-4 < modalW {
		modalW = width - 4
	}
	if modalW < 40 {
		modalW = 40
	}
	innerW := modalW - 6
	if innerW < 34 {
		innerW = 34
	}

	var b strings.Builder
	tableName := ""
	if m.browsePrepared != nil {
		tableName = m.browsePrepared.Name
	}

	header := styles.StyleHeader.Render("Potentially heavy table")
	if tableName != "" {
		header += "  " + styles.StylePillTable.Render(tableName)
	}
	b.WriteString(header)
	b.WriteString("\n\n")

	b.WriteString(styles.StyleHeaderDim.Render("This table contains columns with potentially large payloads:"))
	b.WriteString("\n\n")

	heavy := m.heavyTypes()
	for _, col := range heavy {
		parts := strings.SplitN(col, " · ", 2)
		name := parts[0]
		typ := ""
		if len(parts) > 1 {
			typ = parts[1]
		}
		b.WriteString("  " + styles.StyleSidebarActive.Render("• ") +
			styles.StyleColHeader.Render(name) + " " +
			styles.StyleHeaderDim.Render("("+typ+")") + "\n")
	}

	b.WriteString("\n")
	b.WriteString(styles.StyleHeaderDim.Render("Full payloads are deferred by default to protect memory."))
	b.WriteString("\n\n")
	b.WriteString(styles.StyleBorder.Render(strings.Repeat("─", innerW)))
	b.WriteString("\n\n")

	btnExplore := styles.StyleBtnPrimary.Render("e · Explore safely")
	btnNarrow := styles.StyleBtnSecondary.Render("a · Filter / Narrow")
	btnCancel := styles.StyleBtnSecondary.Render("esc · Cancel")
	b.WriteString(btnExplore + "  " + btnNarrow + "  " + btnCancel)

	modalBox := styles.StylePaneFocus.Width(modalW).Padding(1, 2).Render(b.String())
	content := lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modalBox)

	boxH := lipgloss.Height(modalBox)
	topPad := (height - boxH) / 2
	if topPad < 0 {
		topPad = 0
	}
	leftPad := (width - modalW) / 2
	if leftPad < 0 {
		leftPad = 0
	}

	innerBaseY := 2 + topPad + 2
	buttonY := innerBaseY + 6 + len(heavy)

	wExplore := lipgloss.Width(btnExplore)
	wNarrow := lipgloss.Width(btnNarrow)
	wCancel := lipgloss.Width(btnCancel)

	x0 := 1 + leftPad + 3
	x1 := x0 + wExplore
	x2 := x1 + 2 + wNarrow
	x3 := x2 + 2 + wCancel

	m.browseClicks = []browseClick{
		{kind: browseClickNoticeExplore, y: buttonY, xMin: x0, xMax: x1},
		{kind: browseClickNoticeNarrow, y: buttonY, xMin: x1 + 2, xMax: x2},
		{kind: browseClickNoticeCancel, y: buttonY, xMin: x2 + 2, xMax: x3},
	}

	lines := strings.Split(content, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderBrowseOptions(width, height int) string {
	modalW := 74
	if width-4 < modalW {
		modalW = width - 4
	}
	if modalW < 46 {
		modalW = 46
	}
	innerW := modalW - 6
	if innerW < 40 {
		innerW = 40
	}

	cols := m.browsePreparedColumns()
	var b strings.Builder

	tableName := ""
	if m.browsePrepared != nil {
		tableName = m.browsePrepared.Name
	}

	header := styles.StyleHeader.Render("Browse options")
	if tableName != "" {
		header += "  " + styles.StylePillTable.Render(tableName)
	}
	b.WriteString(header)
	b.WriteString("\n\n")

	b.WriteString(styles.StyleFieldLabel.Render("Columns") + " " + styles.StyleHeaderDim.Render("(space toggles · none selected means all):") + "\n")

	maxColsH := 5
	if height < 24 {
		maxColsH = 3
	}
	startCol, countCol := screens.ListWindow(len(cols), m.browseColumnCursor, maxColsH)
	if countCol == 0 && len(cols) > 0 {
		countCol = len(cols)
	}

	var clicks []browseClick

	for i := startCol; i < startCol+countCol; i++ {
		col := cols[i]
		mark := "[ ]"
		if m.browseColumns[i] {
			mark = "[x]"
		}
		cursor := "  "
		colName := col.Name
		if i == m.browseColumnCursor && m.browseFormFocus == 0 {
			cursor = "> "
			colName = styles.StyleSidebarActive.Render(colName)
		} else {
			colName = styles.StyleColHeader.Render(colName)
		}
		line := fmt.Sprintf("%s%s %s %s\n", cursor, mark, colName, styles.StyleHeaderDim.Render("("+col.Type+")"))
		b.WriteString(line)
	}
	if len(cols) == 0 {
		b.WriteString(styles.StyleHeaderDim.Render("  Column selection is unavailable for this source.") + "\n")
	}

	b.WriteString("\n")
	b.WriteString(styles.StyleBorder.Render(strings.Repeat("─", innerW)))
	b.WriteString("\n\n")

	colBox := styles.StyleInputBox
	if m.browseFormFocus == 1 {
		colBox = styles.StyleInputBoxFocus
	}
	opBox := styles.StyleInputBox
	if m.browseFormFocus == 2 {
		opBox = styles.StyleInputBoxFocus
	}
	valBox := styles.StyleInputBox
	if m.browseFormFocus == 3 {
		valBox = styles.StyleInputBoxFocus
	}
	pageBox := styles.StyleInputBox
	if m.browseFormFocus == 4 {
		pageBox = styles.StyleInputBoxFocus
	}

	filterColName := browseFilterColumnName(cols, m.browseFilterColumn)
	filterOp := browseOperators[m.browseFilterOperator]
	enabledMark := "[ ] disabled"
	if m.browseFilterEnabled {
		enabledMark = "[x] active"
	}

	b.WriteString(styles.StyleFieldLabel.Render("Filter: ") +
		colBox.Render(fmt.Sprintf("[ col: %s ]", filterColName)) + "  " +
		opBox.Render(fmt.Sprintf("[ op: %s ]", filterOp)) + "  " +
		styles.StyleHeaderDim.Render(enabledMark) + "\n")

	b.WriteString(styles.StyleFieldLabel.Render("Value:  ") +
		valBox.Render(fmt.Sprintf("[ %s ]", m.browseFilterInput.View())) + "\n")

	b.WriteString(styles.StyleFieldLabel.Render("Page:   ") +
		pageBox.Render(fmt.Sprintf("[ %s rows ]", m.browsePageInput.View())) + "\n\n")

	b.WriteString(styles.StyleBorder.Render(strings.Repeat("─", innerW)))
	b.WriteString("\n\n")

	btnApply := styles.StyleBtnPrimary.Render("Enter · Apply")
	btnCancel := styles.StyleBtnSecondary.Render("esc · Cancel")
	b.WriteString(btnApply + "  " + btnCancel + "\n")
	b.WriteString(styles.StyleHeaderDim.Render("Tab next · Space toggle/cycle · ↑/↓ choose"))

	modalBox := styles.StylePaneFocus.Width(modalW).Padding(1, 2).Render(b.String())
	content := lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modalBox)

	boxH := lipgloss.Height(modalBox)
	topPad := (height - boxH) / 2
	if topPad < 0 {
		topPad = 0
	}
	leftPad := (width - modalW) / 2
	if leftPad < 0 {
		leftPad = 0
	}

	baseY := 2 + topPad + 2
	colStartY := baseY + 3
	for i := 0; i < countCol; i++ {
		clicks = append(clicks, browseClick{
			kind: browseClickOptionColumn,
			idx:  startCol + i,
			y:    colStartY + i,
		})
	}

	filterRow1Y := colStartY + countCol + 3
	clicks = append(clicks, browseClick{
		kind: browseClickOptionFilterCol,
		y:    filterRow1Y,
		xMin: 0,
		xMax: leftPad + 30,
	})
	clicks = append(clicks, browseClick{
		kind: browseClickOptionFilterOp,
		y:    filterRow1Y,
		xMin: leftPad + 31,
		xMax: width,
	})

	filterRow2Y := filterRow1Y + 1
	clicks = append(clicks, browseClick{
		kind: browseClickOptionFilterVal,
		y:    filterRow2Y,
	})

	pageRowY := filterRow2Y + 1
	clicks = append(clicks, browseClick{
		kind: browseClickOptionPageSize,
		y:    pageRowY,
	})

	btnRowY := pageRowY + 4
	wApply := lipgloss.Width(btnApply)
	xBtn0 := 1 + leftPad + 3
	xBtn1 := xBtn0 + wApply
	clicks = append(clicks, browseClick{
		kind: browseClickOptionApply,
		y:    btnRowY,
		xMin: xBtn0,
		xMax: xBtn1,
	})
	clicks = append(clicks, browseClick{
		kind: browseClickOptionCancel,
		y:    btnRowY,
		xMin: xBtn1 + 2,
		xMax: width,
	})

	m.browseClicks = clicks

	lines := strings.Split(content, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
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
	if len(m.browseClicks) == 0 {
		innerW := m.width - 2
		if innerW < 1 {
			innerW = 1
		}
		contentH := m.height - 4
		if contentH < 1 {
			contentH = 1
		}
		m.renderBrowseDialog(innerW, contentH)
	}
	for _, c := range m.browseClicks {
		if c.y == msg.Y {
			if (c.xMin > 0 || c.xMax > 0) && (msg.X < c.xMin || msg.X > c.xMax) {
				continue
			}
			switch c.kind {
			case browseClickNoticeExplore:
				return m.applyPreparedBrowse(m.defaultBrowseOptions(), m.prefs.BrowsePageSize, true)
			case browseClickNoticeNarrow:
				m.openBrowseOptions()
				return nil
			case browseClickNoticeCancel:
				m.cancelBrowseDialog()
				return nil
			case browseClickOptionColumn:
				m.browseColumnCursor = c.idx
				if c.idx >= 0 && c.idx < len(m.browseColumns) {
					m.browseColumns[c.idx] = !m.browseColumns[c.idx]
				}
				m.browseFormFocus = 0
				return nil
			case browseClickOptionFilterCol:
				m.browseFormFocus = 1
				m.browsePageInput.Blur()
				m.browseFilterInput.Blur()
				return nil
			case browseClickOptionFilterOp:
				m.browseFormFocus = 2
				m.browseFilterOperator = (m.browseFilterOperator + 1) % len(browseOperators)
				m.browsePageInput.Blur()
				m.browseFilterInput.Blur()
				return nil
			case browseClickOptionFilterVal:
				m.browseFormFocus = 3
				m.browsePageInput.Blur()
				m.browseFilterInput.Focus()
				return nil
			case browseClickOptionPageSize:
				m.browseFormFocus = 4
				m.browseFilterInput.Blur()
				m.browsePageInput.Focus()
				return nil
			case browseClickOptionApply:
				return m.submitBrowseOptions()
			case browseClickOptionCancel:
				m.cancelBrowseDialog()
				return nil
			}
		}
	}
	return nil
}
