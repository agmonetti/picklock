package screens

import (
	"fmt"
	"strings"

	"github.com/clipperhouse/uax29/v2/graphemes"
	"github.com/mattn/go-runewidth"

	"github.com/agmonetti/picklock/internal/browser"
	"github.com/agmonetti/picklock/internal/store"
	"github.com/agmonetti/picklock/internal/tui/styles"
)

// SidebarWindow returns the visible window of the sidebar list.
func SidebarWindow(cursor, height int) (offset, visible int) {
	offset = 0
	if cursor >= height {
		offset = cursor - height + 1
	}
	visible = height
	return
}

// RenderSidebar renders the catalog list with grouped semantic objects.
func RenderSidebar(b *browser.Browser, cursor, width, height int) string {
	if b == nil || width < 6 || height < 1 {
		return ""
	}
	items := b.Items
	if len(items) == 0 && len(b.Tables) > 0 {
		items = make([]store.CatalogItem, len(b.Tables))
		for i, name := range b.Tables {
			items[i] = store.CatalogItem{Name: name, Kind: store.CatalogItemTable}
		}
	}
	if len(items) == 0 {
		return ""
	}
	if cursor >= len(items) {
		cursor = len(items) - 1
	}
	if cursor < 0 {
		cursor = 0
	}
	showGroups := sidebarHasGroups(items)

	type sidebarLine struct {
		text      string
		itemIndex int
		group     bool
	}
	lines := make([]sidebarLine, 0, len(items)+2)
	lastGroup := ""
	for i, item := range items {
		if showGroups && item.Group != "" && item.Group != lastGroup {
			lines = append(lines, sidebarLine{text: item.Group, itemIndex: -1, group: true})
			lastGroup = item.Group
		}
		badge := ""
		if item.Badge != "" {
			badge = " [" + item.Badge + "]"
		}
		lines = append(lines, sidebarLine{text: truncate(item.Name+badge, width-2), itemIndex: i})
	}

	cursorLine := 0
	for i, line := range lines {
		if line.itemIndex == cursor {
			cursorLine = i
			break
		}
	}
	offset := cursorLine - height + 1
	if offset < 0 {
		offset = 0
	}

	var sb strings.Builder
	for i := offset; i < len(lines) && i < offset+height; i++ {
		line := lines[i]
		if line.group {
			sb.WriteString(styles.StyleHeader.Render(line.text))
		} else {
			switch {
			case line.itemIndex == cursor:
				sb.WriteString(styles.StyleSidebarActive.Render("> " + line.text))
			case items[line.itemIndex].Name == b.ActiveTable:
				sb.WriteString(styles.StyleSidebarActiveTable.Render("  " + line.text))
			default:
				sb.WriteString(styles.StyleSidebarItem.Render("  " + line.text))
			}
		}
		if i+1 < len(lines) && i+1 < offset+height {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// SidebarItemAtLine maps a rendered sidebar line to a catalog item index.
func SidebarItemAtLine(b *browser.Browser, cursor, height, line int) int {
	if b == nil || line < 0 || height < 1 {
		return -1
	}
	items := b.Items
	if len(items) == 0 {
		items = make([]store.CatalogItem, len(b.Tables))
		for i, name := range b.Tables {
			items[i] = store.CatalogItem{Name: name, Kind: store.CatalogItemTable}
		}
	}
	if len(items) == 0 {
		return -1
	}
	cursor = maxSidebarCursor(cursor, len(items))
	showGroups := sidebarHasGroups(items)
	type sidebarLine struct{ itemIndex int }
	lines := make([]sidebarLine, 0, len(items)+2)
	lastGroup := ""
	for i, item := range items {
		if showGroups && item.Group != "" && item.Group != lastGroup {
			lines = append(lines, sidebarLine{itemIndex: -1})
			lastGroup = item.Group
		}
		lines = append(lines, sidebarLine{itemIndex: i})
	}
	cursorLine := 0
	for i, entry := range lines {
		if entry.itemIndex == cursor {
			cursorLine = i
			break
		}
	}
	offset := cursorLine - height + 1
	if offset < 0 {
		offset = 0
	}
	idx := offset + line
	if idx < 0 || idx >= len(lines) || lines[idx].itemIndex < 0 {
		return -1
	}
	return lines[idx].itemIndex
}

func sidebarHasGroups(items []store.CatalogItem) bool {
	first := ""
	for _, item := range items {
		if item.Group == "" {
			continue
		}
		if item.Group == "RELATIONS" {
			return true
		}
		if first == "" {
			first = item.Group
			continue
		}
		if item.Group != first {
			return true
		}
	}
	return false
}

func maxSidebarCursor(cursor, n int) int {
	if cursor < 0 {
		return 0
	}
	if cursor >= n {
		return n - 1
	}
	return cursor
}

// TableWindow returns the visible window of a data table.
func TableWindow(rows, cursor, height int) (start, visible int) {
	visible = height - 2 // leaves room for the header and separator
	if visible > rows {
		visible = rows
	}
	if visible <= 0 {
		return 0, 0
	}
	start = 0
	if cursor >= visible {
		start = cursor - visible + 1
	}
	return
}

// ListWindow returns the visible window of a line-based list without headers.
func ListWindow(rows, cursor, height int) (start, visible int) {
	visible = height
	if visible > rows {
		visible = rows
	}
	if visible <= 0 {
		return 0, 0
	}
	start = 0
	if cursor >= visible {
		start = cursor - visible + 1
	}
	return
}

const colSep = " | "
const colSepW = 3

// RenderMainBrowser renders data from any DataView.
// colScroll is the index of the first visible column for tabular data.
func RenderMainBrowser(b *browser.Browser, colScroll, width, height int) (out string) {
	if b == nil {
		return styles.StyleHeaderDim.Render("no connection")
	}
	if len(b.Tables) == 0 {
		noun := "items"
		if b.ItemNoun != "" {
			noun = b.ItemNoun + "s"
		}
		return styles.StyleHeaderDim.Render(fmt.Sprintf("no %s — use the editor to create data", noun))
	}
	if b.Data == nil {
		if len(b.Columns) > 0 {
			cols := make([]string, len(b.Columns))
			for i, c := range b.Columns {
				cols[i] = c.Name
			}
			if len(b.Rows) == 0 {
				return renderEmptyTable(cols, b.Cursor, colScroll, width)
			}
			return RenderDataTableWithNulls(cols, b.Rows, b.Nulls, b.Cursor, colScroll, width, height)
		}
		return styles.StyleHeaderDim.Render("select an item")
	}
	if summary := store.IncompletenessSummary(b.Data); summary != "" {
		height--
		defer func() { out += "\n" + styles.StyleHeaderDim.Render(truncate("INCOMPLETE: "+summary, width)) }()
	}

	switch v := b.Data.(type) {
	case *store.TabularData:
		cols := v.Columns
		if len(cols) == 0 {
			cols = make([]string, len(b.Columns))
			for i, c := range b.Columns {
				cols[i] = c.Name
			}
		}
		if len(v.Rows) == 0 {
			return renderEmptyTable(cols, b.Cursor, colScroll, width)
		}
		return renderDataTable(cols, v.Rows, v.Nulls, v.Incomplete, 0, b.Cursor, colScroll, width, height)

	case *store.DocumentData:
		return RenderDocumentList(v, b.Cursor, width, height)

	case *store.KeyValueData:
		return RenderKeyValue(v, b.Cursor, width, height)

	case *store.GraphData:
		return RenderGraph(v, b.Cursor, width, height)

	case *store.RawTextData:
		return RenderRawText(v, width, height)

	default:
		// Fallback for Tabular convenience fields
		if len(b.Columns) > 0 {
			cols := make([]string, len(b.Columns))
			for i, c := range b.Columns {
				cols[i] = c.Name
			}
			if len(b.Rows) == 0 {
				return renderEmptyTable(cols, b.Cursor, colScroll, width)
			}
			return RenderDataTableWithNulls(cols, b.Rows, b.Nulls, b.Cursor, colScroll, width, height)
		}
		return styles.StyleHeaderDim.Render("unsupported view format")
	}
}

// renderEmptyTable draws an empty table while keeping the column header and
// separator above the empty-state box, per the documented design ("An empty
// table keeps the header and the separator and draws a centered empty-table
// ASCII box"). When no column names are known (e.g. a view without schema
// metadata), only the box is drawn.
func renderEmptyTable(cols []string, cursor, colScroll, width int) string {
	if len(cols) == 0 {
		return RenderEmptyTable(cols, width)
	}
	return RenderDataTable(cols, nil, cursor, colScroll, width, 2) + "\n\n" + RenderEmptyTable(cols, width)
}

// RenderDocumentList renders a list of BSON/JSON documents.
func RenderDocumentList(docs *store.DocumentData, cursor, width, height int) string {
	if len(docs.Documents) == 0 {
		return styles.StyleHeaderDim.Render("( 0 documents )")
	}
	start, visible := ListWindow(len(docs.Documents), cursor, height)
	var sb strings.Builder
	for i := 0; i < visible; i++ {
		idx := start + i
		if idx >= len(docs.Documents) {
			break
		}
		d := docs.Documents[idx]
		summary := d.Summary
		if summary == "" {
			summary = d.RawJSON
		}
		if d.Incomplete != nil {
			summary = presentIncomplete(summary, *d.Incomplete, width)
		}
		line := fmt.Sprintf("[%s] %s", truncate(d.ID, width), truncate(summary, width))
		line = truncate(line, width-4)
		if idx == cursor {
			sb.WriteString(styles.StyleCursor.Render("> "+line) + "\n")
		} else {
			sb.WriteString(styles.StyleSidebarItem.Render("  "+line) + "\n")
		}
	}
	return sb.String()
}

// RenderKeyValue renders Redis Key-Value entries.
func RenderKeyValue(kv *store.KeyValueData, cursor, width, height int) string {
	if kv.Type == "string" {
		var sb strings.Builder
		sb.WriteString(styles.StyleHeaderDim.Render(fmt.Sprintf("Key: %s · Type: string · TTL: %s", kv.Key, kv.TTL)) + "\n\n")
		raw := &store.RawTextData{}
		if len(kv.Entries) > 0 {
			raw.Text, raw.Incomplete = kv.Entries[0].Value, kv.Entries[0].Incomplete
		}
		sb.WriteString(RenderRawText(raw, width, height-2) + "\n")
		return sb.String()
	}

	cols := []string{"INDEX", "VALUE"}
	hasExtra := false
	for _, e := range kv.Entries {
		if e.Extra != "" {
			hasExtra = true
			break
		}
	}
	if hasExtra {
		cols = append(cols, "EXTRA")
	}

	rows := make([][]string, len(kv.Entries))
	var incomplete map[store.CellPosition]store.IncompleteCell
	for i, e := range kv.Entries {
		if hasExtra {
			rows[i] = []string{e.Index, e.Value, e.Extra}
		} else {
			rows[i] = []string{e.Index, e.Value}
		}
		if e.Incomplete != nil {
			if incomplete == nil {
				incomplete = make(map[store.CellPosition]store.IncompleteCell)
			}
			incomplete[store.CellPosition{Row: i, Column: 1}] = *e.Incomplete
		}
	}
	header := styles.StyleHeaderDim.Render(fmt.Sprintf("Key: %s · Type: %s · TTL: %s", kv.Key, kv.Type, kv.TTL))
	table := renderDataTable(cols, rows, nil, incomplete, 0, cursor, 0, width, height-2)
	return header + "\n" + table
}

// RenderGraph renders Neo4j nodes and incident relationships.
func RenderGraph(g *store.GraphData, cursor, width, height int) string {
	if len(g.Nodes) == 0 {
		return styles.StyleHeaderDim.Render("( 0 nodes )")
	}
	start, visible := ListWindow(len(g.Nodes), cursor, height)
	var sb strings.Builder
	for i := range visible {
		idx := start + i
		n := &g.Nodes[idx]
		labels := ":" + strings.Join(n.Labels, ":")
		props := graphPropertiesSummary(n.Properties, n.Incomplete, width)
		line := fmt.Sprintf("(%s %s) %s [%d edges]", truncate(n.ID, width), truncate(labels, width), props, len(n.Incident))
		line = truncate(line, width-4)
		if idx == cursor {
			sb.WriteString(styles.StyleCursor.Render("> "+line) + "\n")
		} else {
			sb.WriteString(styles.StyleSidebarItem.Render("  "+line) + "\n")
		}
	}
	return sb.String()
}

func graphPropertiesSummary(properties map[string]string, incomplete map[string]store.IncompleteCell, width int) string {
	var props strings.Builder
	for key, value := range properties {
		if cell, ok := incomplete[key]; ok {
			value = presentIncomplete(value, cell, width)
		}
		fmt.Fprintf(&props, "%s: %q ", truncate(key, width), truncate(value, width))
		if cappedCellWidth(props.String(), width) == width {
			return truncate(props.String(), width)
		}
	}
	for key, cell := range incomplete {
		if _, ok := properties[key]; !ok {
			fmt.Fprintf(&props, "%s: %s ", truncate(key, width), truncate(cell.Label(), width))
			if cappedCellWidth(props.String(), width) == width {
				break
			}
		}
	}
	return truncate(props.String(), width)
}

// RenderRawText renders plain diagnostic text.
func RenderRawText(raw *store.RawTextData, width, height int) string {
	// Do not split or copy the hidden suffix just to draw the visible lines.
	text := raw.Text
	if raw.Incomplete != nil {
		text = presentIncomplete(text, *raw.Incomplete, width-2)
	}
	var sb strings.Builder
	for i := range height {
		line, rest, found := strings.Cut(text, "\n")
		sb.WriteString(truncate(line, width-2))
		if !found || i+1 == height {
			break
		}
		sb.WriteByte('\n')
		text = rest
	}
	return sb.String()
}

// RenderDataTable renders columns + rows with cursor row highlighted.
// colScroll is the index of the first visible column (0 = no horizontal scroll).
func RenderDataTable(cols []string, rows [][]string, cursor, colScroll, width, height int) string {
	return RenderDataTableWithNulls(cols, rows, nil, cursor, colScroll, width, height)
}

// RenderDataTableWithNulls renders columns + rows with null indicators from the nulls mask.
func RenderDataTableWithNulls(cols []string, rows [][]string, nulls [][]bool, cursor, colScroll, width, height int) string {
	return renderDataTable(cols, rows, nulls, nil, 0, cursor, colScroll, width, height)
}

// RenderTabularData preserves absolute metadata positions while the editor scrolls.
func RenderTabularData(data *store.TabularData, rowOffset, cursor, colScroll, width, height int) string {
	rowOffset = min(max(rowOffset, 0), len(data.Rows))
	var nulls [][]bool
	if rowOffset < len(data.Nulls) {
		nulls = data.Nulls[rowOffset:]
	}
	return renderDataTable(data.Columns, data.Rows[rowOffset:], nulls, data.Incomplete, rowOffset, cursor, colScroll, width, height)
}

func renderDataTable(cols []string, rows [][]string, nulls [][]bool, incomplete map[store.CellPosition]store.IncompleteCell, rowOffset, cursor, colScroll, width, height int) string {
	if len(cols) == 0 {
		return ""
	}

	naturalW := colNaturalWidths(cols, rows)
	for pos, cell := range incomplete {
		row := pos.Row - rowOffset
		if row < 0 || row >= len(rows) || pos.Column < 0 || pos.Column >= len(cols) {
			continue
		}
		if row < len(nulls) && pos.Column < len(nulls[row]) && nulls[row][pos.Column] {
			continue
		}
		// Measure only enough prefix to fill the bounded presentation column.
		markerWidth := cappedCellWidth(cell.Label(), colMaxW)
		presentationWidth := markerWidth
		if cell.State != store.CellDeferred && pos.Column < len(rows[row]) && markerWidth+1 < colMaxW {
			presentationWidth += 1 + cappedCellWidth(rows[row][pos.Column], colMaxW-markerWidth-1)
		}
		naturalW[pos.Column] = max(naturalW[pos.Column], presentationWidth)
	}
	visStart, visWidths, hasLeft, hasRight := colScrollWindow(naturalW, colScroll, width)
	visCols := cols[visStart : visStart+len(visWidths)]

	start, visible := TableWindow(len(rows), cursor, height)

	var sb strings.Builder
	sb.WriteString(renderHeaderScrolled(visCols, visWidths, hasLeft, hasRight) + "\n")
	sb.WriteString(renderSep(visCols, visWidths, hasLeft) + "\n")

	for i := 0; i < visible; i++ {
		row := start + i
		if row >= len(rows) {
			break
		}
		visRow := rows[row][visStart : visStart+len(visWidths)]
		var visNulls []bool
		if row < len(nulls) && nulls[row] != nil && visStart < len(nulls[row]) {
			end := visStart + len(visWidths)
			if end > len(nulls[row]) {
				end = len(nulls[row])
			}
			visNulls = nulls[row][visStart:end]
		}
		sb.WriteString(renderRowWithMetadata(visRow, visNulls, visWidths, row == cursor, hasLeft, incomplete, row+rowOffset, visStart) + "\n")
	}
	return sb.String()
}

// Prefix the marker before a bounded value prefix: cropping can never leave an
// unlabelled preview, and presentation never stores another copy of the payload.
func presentIncomplete(value string, cell store.IncompleteCell, width int) string {
	label := truncate(cell.Label(), width)
	if cell.State == store.CellDeferred {
		return label
	}
	remaining := width - cappedCellWidth(label, width) - 1
	if remaining <= 0 {
		return label
	}
	return label + " " + truncate(value, remaining)
}

// colNaturalWidths computes the natural (uncompressed) width of each column,
// capped at colMaxW. It scans up to 100 rows for content width.
func colNaturalWidths(cols []string, rows [][]string) []int {
	n := len(cols)
	widths := make([]int, n)
	for i, c := range cols {
		widths[i] = cappedCellWidth(c, colMaxW)
	}
	maxRows := len(rows)
	if maxRows > 100 {
		maxRows = 100
	}
	for r := 0; r < maxRows; r++ {
		for i, cell := range rows[r] {
			if i >= n {
				break
			}
			if widths[i] == colMaxW {
				continue
			}
			if w := cappedCellWidth(cell, colMaxW); w > widths[i] {
				widths[i] = w
			}
		}
	}
	for i := range widths {
		if widths[i] < colMinW {
			widths[i] = colMinW
		}
	}
	return widths
}

// colScrollWindow determines which columns are visible starting at colScroll,
// returning the actual start index, their widths, and whether there are hidden
// columns to the left or right.
func colScrollWindow(naturalW []int, colScroll, width int) (start int, widths []int, hasLeft, hasRight bool) {
	n := len(naturalW)
	if n == 0 {
		return 0, nil, false, false
	}
	// Reserve 2 chars for scroll indicator if needed (◀ / ▶ each take 1 rune = ~2 cells on some terminals)
	// We use "◀ " / " ▶" as prefixes/suffixes: each costs 2 visual chars in header.
	if colScroll < 0 {
		colScroll = 0
	}
	if colScroll >= n {
		colScroll = n - 1
	}

	hasLeft = colScroll > 0
	// When there's a left indicator, we need 2 extra chars
	leftReserve := 0
	if hasLeft {
		leftReserve = 2
	}

	var visible []int
	used := leftReserve
	for i := colScroll; i < n; i++ {
		w := naturalW[i]
		sep := 0
		if i > colScroll {
			sep = colSepW
		}

		rightReserve := 0
		if i < n-1 {
			rightReserve = 2
		}

		if used+sep+w+rightReserve > width {
			if len(visible) > 0 {
				break
			}
			budget := width - leftReserve - rightReserve
			if budget < 1 {
				budget = 1
			}
			visible = append(visible, budget)
			break
		}
		used += sep + w
		visible = append(visible, w)
	}
	if len(visible) == 0 && colScroll < n {
		visible = []int{1}
	}

	lastVisible := colScroll + len(visible) - 1
	hasRight = lastVisible < n-1

	return colScroll, visible, hasLeft, hasRight
}

func renderHeaderScrolled(cells []string, widths []int, hasLeft, hasRight bool) string {
	var sb strings.Builder
	if hasLeft {
		sb.WriteString(styles.StyleBorder.Render("◀ "))
	}
	for i, c := range cells {
		if i >= len(widths) {
			break
		}
		sb.WriteString(styles.StyleColHeader.Render(pad(truncate(c, widths[i]), widths[i])))
		if i < len(cells)-1 {
			sb.WriteString(styles.StyleBorder.Render(colSep))
		}
	}
	if hasRight {
		sb.WriteString(styles.StyleBorder.Render(" ▶"))
	}
	return sb.String()
}

func renderSep(cells []string, widths []int, hasLeft bool) string {
	var sb strings.Builder
	if hasLeft {
		sb.WriteString("--")
	}
	for i, w := range widths {
		if i >= len(cells) {
			break
		}
		sb.WriteString(strings.Repeat("-", w))
		if i < len(cells)-1 {
			sb.WriteString("-+-")
		}
	}
	return styles.StyleBorder.Render(sb.String())
}

const colMaxW = 36
const colMinW = 4

func renderRowWithMetadata(cells []string, nulls []bool, widths []int, selected bool, hasLeft bool, incomplete map[store.CellPosition]store.IncompleteCell, row, colOffset int) string {
	var sb strings.Builder
	if hasLeft {
		if selected {
			sb.WriteString(styles.StyleCursor.Render("  "))
		} else {
			sb.WriteString("  ")
		}
	}
	for i, cell := range cells {
		if i >= len(widths) {
			break
		}
		isNull := i < len(nulls) && nulls[i]
		if info, ok := incomplete[store.CellPosition{Row: row, Column: i + colOffset}]; ok && !isNull {
			cell = presentIncomplete(cell, info, widths[i])
		}
		sb.WriteString(renderCellText(cell, isNull, widths[i], selected))
		if i < len(cells)-1 {
			if selected {
				sb.WriteString(styles.StyleCursor.Render(colSep))
			} else {
				sb.WriteString(styles.StyleBorder.Render(colSep))
			}
		}
	}
	return sb.String()
}

func renderCellText(cell string, isNull bool, width int, selected bool) string {
	if width <= 0 {
		return ""
	}
	if isNull {
		if width == 1 {
			if selected {
				return styles.NullCellSelected()
			}
			return styles.NullCell()
		}
		padding := strings.Repeat(" ", width-1)
		if selected {
			return styles.NullCellSelected() + styles.StyleCursor.Render(padding)
		}
		return styles.NullCell() + padding
	}

	text := truncate(cell, width)
	text = pad(text, width)
	if selected {
		text = styles.StyleCursor.Render(text)
	}
	return text
}

// truncate visits only the visible prefix plus one grapheme of lookahead.
// Measure clusters with runewidth to keep the same terminal-width policy as pad.
// Styling is applied by callers after cropping, so ANSI styles are not cut.
func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	g := graphemes.FromString(s)
	width, tailEnd := 0, 0
	for g.Next() {
		width += runewidth.StringWidth(g.Value())
		if width > max {
			return s[:tailEnd] + "…"
		}
		if width <= max-1 {
			tailEnd = g.End()
		}
	}
	return s
}

func cappedCellWidth(s string, max int) int {
	width := 0
	g := graphemes.FromString(s)
	for g.Next() {
		width += runewidth.StringWidth(g.Value())
		if width >= max {
			return max
		}
	}
	return width
}

func pad(s string, w int) string {
	if d := w - runewidth.StringWidth(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// RenderEmptyTable renders the centered ASCII-art empty state drawing.
func RenderEmptyTable(cols []string, width int) string {
	if width < 12 {
		width = 12
	}
	caption := "( 0 rows returned )"
	inner := runewidth.StringWidth(caption)
	if inner > width-3 {
		inner = width - 3
	}
	if inner < 6 {
		inner = 6
	}
	center := func(s string) string {
		pad := (width - runewidth.StringWidth(s)) / 2
		if pad < 0 {
			pad = 0
		}
		return strings.Repeat(" ", pad) + s
	}
	head := "+" + strings.Repeat("-", inner) + "+"
	mid := "|" + strings.Repeat(" ", inner) + "|"
	return styles.StyleBorder.Render(center(head)) + "\n" +
		styles.StyleBorder.Render(center(mid)) + "\n" +
		styles.StyleBorder.Render(center(head)) + "\n\n" +
		styles.StyleHeaderDim.Render(center(caption))
}

// RenderRowDetail wraps already-loaded values only as far as the viewport needs.
func RenderRowDetail(title string, cols, vals []string, cursor, scroll, width, height int) string {
	return RenderRowDetailWithMetadata(title, cols, vals, nil, nil, cursor, scroll, width, height)
}

// RenderRowDetailWithMetadata keeps NULL, empty, preview and deferred distinct.
// The NULL mask is authoritative; an absent entry is not NULL.
func RenderRowDetailWithMetadata(title string, cols, vals []string, nulls []bool, incomplete map[int]store.IncompleteCell, cursor, scroll, width, height int) string {
	if width < 4 {
		width = 40
	}
	contentW := width - 4
	if contentW < 4 {
		contentW = 4
	}

	var b strings.Builder
	b.WriteString(styles.StyleHeader.Render(title))
	b.WriteString("\n")
	if len(cols) == 0 {
		b.WriteString(styles.StyleHeaderDim.Render("  no columns"))
		return b.String()
	}
	if scroll < 0 {
		scroll = 0
	}
	if height <= 0 {
		return ""
	}
	limit := scroll + height

	var lines []string
	lines = append(lines, "")
	for i := range cols {
		name := cols[i]
		val := ""
		if i < len(vals) {
			val = vals[i]
		}
		isNull := i < len(nulls) && nulls[i]
		if cell, ok := incomplete[i]; ok && !isNull {
			val = presentIncomplete(val, cell, contentW-4)
		}
		if i == cursor {
			lines = append(lines, styles.StyleSidebarActive.Render("> ")+styles.StyleColHeader.Render(name+":"))
		} else {
			lines = append(lines, "  "+styles.StyleColHeader.Render(name+":"))
		}
		if len(lines) >= limit {
			break
		}
		if isNull {
			lines = append(lines, "    "+styles.NullCell())
		} else {
			wrapped := wrapCellLines(val, contentW-4, limit-len(lines))
			for _, wl := range wrapped {
				lines = append(lines, "    "+wl)
			}
		}
		lines = append(lines, "")
		if len(lines) >= limit {
			break
		}
	}

	// Deep scrolling still walks the prefix, but never wraps the hidden suffix.
	if scroll > len(lines) {
		scroll = len(lines)
	}
	end := scroll + height
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[scroll:end], "\n")
}

// WrapCells splits a string into lines that do not exceed max width.
func WrapCells(s string, max int) []string {
	return wrapCellLines(s, max, -1)
}

// A negative limit requests all lines (used for field navigation/hit testing).
func wrapCellLines(s string, max, limit int) []string {
	if limit == 0 {
		return nil
	}
	if max <= 0 {
		return []string{""}
	}
	var out []string
	lineStart, lineW := 0, 0
	g := graphemes.FromString(s)
	for g.Next() {
		w := runewidth.StringWidth(g.Value())
		if lineW+w > max && lineW > 0 {
			out = append(out, s[lineStart:g.Start()])
			if limit > 0 && len(out) == limit {
				return out
			}
			lineStart = g.Start()
			lineW = 0
		}
		lineW += w
	}
	if lineStart < len(s) {
		out = append(out, s[lineStart:])
	}
	if len(out) == 0 {
		out = []string{""}
	}
	return out
}
