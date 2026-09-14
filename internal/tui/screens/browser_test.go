package screens

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/agmonetti/picklock/internal/browser"
	"github.com/agmonetti/picklock/internal/store"
	"github.com/mattn/go-runewidth"
)

func init() {
	// The tests run without a TTY; force a color profile so lipgloss emits
	// ANSI escapes and the cursor highlight is detectable in the output.
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(true)
}

func TestRenderDataTable_HighlightsOnlyCursorRow(t *testing.T) {
	cols := []string{"a", "b", "c"}
	rows := [][]string{{"1", "2", "3"}, {"4", "5", "6"}, {"7", "8", "9"}}

	out := RenderDataTable(cols, rows, 1, 0, 40, 10)
	lines := strings.Split(out, "\n")

	// lines[0] = header, lines[1] = separator, lines[2..4] = the three rows
	if hasBackground(lines[2]) {
		t.Errorf("row 0 must not be highlighted: %q", lines[2])
	}
	if !hasBackground(lines[3]) {
		t.Errorf("row 1 (cursor) must be highlighted: %q", lines[3])
	}
	if hasBackground(lines[4]) {
		t.Errorf("row 2 must not be highlighted: %q", lines[4])
	}
}

func TestRenderDataTable_NoSelectionHighlightsNothing(t *testing.T) {
	cols := []string{"a", "b", "c"}
	rows := [][]string{{"1", "2", "3"}, {"4", "5", "6"}}

	out := RenderDataTable(cols, rows, -1, 0, 40, 10)
	// lines[0] is the column header (styled by design); the separator is a
	// foreground-only line; the data rows must not carry a selection background
	for i, line := range strings.Split(out, "\n") {
		if i <= 1 {
			continue
		}
		if hasBackground(line) {
			t.Errorf("line %d must not be highlighted with cursor=-1: %q", i, line)
		}
	}
}

// hasBackground reports whether an ANSI string sets a background color.
func hasBackground(s string) bool {
	return strings.Contains(s, "48;")
}

func TestRenderMainBrowser_EmptyTabularDataShowsColumns(t *testing.T) {
	// The real browse flow always sets Data to a TabularData, even when the
	// table has zero rows: the empty table must still draw its column header.
	b := &browser.Browser{
		Tables:      []string{"users"},
		ActiveTable: "users",
		Data: &store.TabularData{
			Columns: []string{"id", "name", "email"},
			Rows:    nil,
		},
		Columns: []store.Column{
			{Name: "id", Type: "INTEGER", PK: true},
			{Name: "name", Type: "TEXT"},
			{Name: "email", Type: "TEXT"},
		},
		PageSize: 50,
	}

	out := RenderMainBrowser(b, 0, 96, 20)
	foundHeader := false
	foundHint := false
	for _, l := range strings.Split(out, "\n") {
		s := ansi.Strip(l)
		if strings.Contains(s, "id") && strings.Contains(s, "name") && strings.Contains(s, "email") {
			foundHeader = true
		}
		if strings.Contains(s, "0 rows returned") {
			foundHint = true
		}
	}
	if !foundHeader {
		t.Error("empty table must still show the column headers")
	}
	if !foundHint {
		t.Error("empty table must show the '( 0 rows returned )' hint")
	}
}

func TestRenderMainBrowser_EmptyTabularDataFallsBackToSchemaColumns(t *testing.T) {
	// Cassandra SELECT * on an empty table can come back without column
	// metadata; the renderer must fall back to the schema columns (b.Columns).
	b := &browser.Browser{
		Tables:      []string{"users"},
		ActiveTable: "users",
		Data: &store.TabularData{
			Columns: nil, // empty result set carried no column metadata
			Rows:    nil,
		},
		Columns: []store.Column{
			{Name: "id", Type: "INT", PK: true},
			{Name: "name", Type: "TEXT"},
		},
		PageSize: 50,
	}

	out := RenderMainBrowser(b, 0, 96, 20)
	foundHeader := false
	foundHint := false
	for _, l := range strings.Split(out, "\n") {
		s := ansi.Strip(l)
		if strings.Contains(s, "id") && strings.Contains(s, "name") {
			foundHeader = true
		}
		if strings.Contains(s, "0 rows returned") {
			foundHint = true
		}
	}
	if !foundHeader {
		t.Error("empty table without result metadata must still show the schema column headers")
	}
	if !foundHint {
		t.Error("empty table must show the '( 0 rows returned )' hint")
	}
}

func TestRenderMainBrowser_EmptyTabularDataWithoutAnyColumnsShowsOnlyHint(t *testing.T) {
	// No schema available (e.g. a view that has neither result metadata nor
	// an inspection view): only the empty-state box is drawn, no dead header.
	b := &browser.Browser{
		Tables:      []string{"users"},
		ActiveTable: "users",
		Data: &store.TabularData{
			Columns: nil,
			Rows:    nil,
		},
		PageSize: 50,
	}

	out := RenderMainBrowser(b, 0, 96, 20)
	if !strings.Contains(out, "0 rows returned") {
		t.Error("empty table must show the '( 0 rows returned )' hint")
	}
}

func TestRenderSidebar_ScrollsToCursor(t *testing.T) {
	b := &browser.Browser{ActiveTable: "table_005"}
	for i := 0; i < 200; i++ {
		b.Tables = append(b.Tables, fmt.Sprintf("table_%03d", i))
	}

	// cursor near the top: no scrolling, window 0..4, cursor at bottom of window
	out := RenderSidebar(b, 5, 20, 5)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("lines = %d, want 5", len(lines))
	}
	if !strings.Contains(lines[4], ">") || !strings.Contains(lines[4], "table_005") {
		t.Errorf("cursor line = %q, want >table_005 at last visible line", lines[4])
	}
	if !strings.Contains(lines[0], "table_001") {
		t.Errorf("first visible line = %q, want table_001", lines[0])
	}

	// cursor far away: scroll so it stays visible at bottom of window (146..150)
	out = RenderSidebar(b, 150, 20, 5)
	lines = strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("lines = %d, want 5 (scrolled)", len(lines))
	}
	if !strings.Contains(lines[4], ">") || !strings.Contains(lines[4], "table_150") {
		t.Errorf("last visible line = %q, want >table_150", lines[4])
	}
	if !strings.Contains(lines[0], "table_146") {
		t.Errorf("first visible line = %q, want table_146", lines[0])
	}

	// cursor at the very end: window shows last 5 tables, cursor at bottom
	out = RenderSidebar(b, 199, 20, 5)
	lines = strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 5 || !strings.Contains(lines[4], "table_199") || !strings.Contains(lines[4], ">") {
		t.Errorf("last table lines = %q, want 5 lines ending in >table_199", strings.Join(lines, "|"))
	}
	if !strings.Contains(lines[0], "table_195") {
		t.Errorf("first visible line = %q, want table_195", lines[0])
	}
}

func TestRenderSidebar_MarksOpenedTable(t *testing.T) {
	b := &browser.Browser{ActiveTable: "table_002"}
	for i := 0; i < 10; i++ {
		b.Tables = append(b.Tables, fmt.Sprintf("table_%03d", i))
	}

	// cursor on 0, opened table 002: it must be shown without the ">"
	out := RenderSidebar(b, 0, 20, 10)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if !strings.Contains(lines[0], ">") {
		t.Errorf("cursor line must have '>': %q", lines[0])
	}
	if strings.Contains(lines[2], ">") {
		t.Errorf("opened table line must not have '>': %q", lines[2])
	}
}

func TestRenderDataTable_CursorStaysVisible(t *testing.T) {
	cols := []string{"id"}
	rows := make([][]string, 0, 10)
	for i := 0; i < 10; i++ {
		rows = append(rows, []string{string(rune('a' + i))})
	}

	// height=6 -> 4 visible rows (6 minus 2 for header/separator). Total lines produced must be 6.
	out := RenderDataTable(cols, rows, 0, 0, 20, 6)
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 6 {
		t.Fatalf("total lines = %d, want 6", len(lines))
	}
	data := dataLines(out, 6)
	if len(data) != 4 {
		t.Fatalf("data rows = %d, want 4", len(data))
	}
	if data[0] != "a" || data[3] != "d" {
		t.Errorf("cursor 0 should show rows a-d only: %v", data)
	}

	// Cursor 3 (last visible row in initial window): window 0..3 (rows a-d)
	out = RenderDataTable(cols, rows, 3, 0, 20, 6)
	data = dataLines(out, 6)
	if len(data) != 4 {
		t.Fatalf("data rows = %d, want 4", len(data))
	}
	if data[0] != "a" || data[3] != "d" {
		t.Errorf("cursor 3 should show rows a-d: %v", data)
	}

	// Cursor 4: window must displace by 1 so row 4 (e) is visible at bottom (rows b-e)
	out = RenderDataTable(cols, rows, 4, 0, 20, 6)
	data = dataLines(out, 6)
	if len(data) != 4 {
		t.Fatalf("data rows = %d, want 4", len(data))
	}
	if data[0] != "b" || data[3] != "e" {
		t.Errorf("cursor 4 should show rows b-e: %v", data)
	}

	// Cursor 7: window must scroll so row 7 (h) is visible (rows e-h).
	out = RenderDataTable(cols, rows, 7, 0, 20, 6)
	data = dataLines(out, 6)
	if len(data) != 4 {
		t.Fatalf("data rows = %d, want 4", len(data))
	}
	if data[0] != "e" || data[3] != "h" {
		t.Errorf("cursor 7 should show rows e-h: %v", data)
	}
}

func TestColWidths_ShrinksToFitManyColumns(t *testing.T) {
	cols := make([]string, 0, 12)
	for i := 0; i < 12; i++ {
		cols = append(cols, "col012345") // 9 wide
	}
	rows := [][]string{cols} // each cell as wide as its header

	widths := colWidths(cols, rows, 70)
	total := (len(widths) - 1) * colSepW
	for _, w := range widths {
		total += w
	}
	if total > 70 {
		t.Errorf("widths %v sum to %d, want <= 70", widths, total)
	}
	for _, w := range widths {
		if w < 1 {
			t.Errorf("width %d below the 1-char minimum", w)
		}
	}

	// with a tiny width every column keeps shrinking to a 1-cell minimum; the
	// table may still not fit when even n + (n-1)*3 separators exceeds the pane
	widths = colWidths(cols, rows, 40)
	total = (len(widths) - 1) * colSepW
	for _, w := range widths {
		total += w
	}
	minTotal := len(widths) + (len(widths)-1)*colSepW
	if total != minTotal {
		t.Errorf("widths %v sum to %d, want the 1-cell minimum %d", widths, total, minTotal)
	}
	for _, w := range widths {
		if w < 1 {
			t.Errorf("width %d below the 1-char minimum", w)
		}
	}
}

func dataLines(out string, height int) []string {
	lines := ansiStripLines(strings.TrimSuffix(out, "\n"))
	data := make([]string, 0, len(lines))
	for i, l := range lines {
		if i == 0 || i == 1 {
			continue // header + separator
		}
		data = append(data, strings.TrimSpace(l))
	}
	return data
}

func TestColWidths_KeepsNaturalWhenItFits(t *testing.T) {
	cols := []string{"id", "name"}
	rows := [][]string{{"1", "alice"}}
	widths := colWidths(cols, rows, 40)
	if widths[0] != 2 || widths[1] != 5 {
		t.Errorf("widths = %v, want [2 5]", widths)
	}
}

func TestColWidths_CapsAndDistributes(t *testing.T) {
	cols := []string{"id", "name", "body"}
	rows := [][]string{{"1", "alice", strings.Repeat("x", 100)}}
	widths := colWidths(cols, rows, 40)
	if len(widths) != 3 {
		t.Fatalf("widths = %v", widths)
	}
	if widths[2] > colMaxW {
		t.Errorf("body width = %d, want <= %d", widths[2], colMaxW)
	}
	total := (len(widths) - 1) * colSepW
	for _, w := range widths {
		total += w
	}
	if total > 40 {
		t.Errorf("total = %d, want <= 40", total)
	}
	// the narrow columns keep their natural width; the long one absorbs the rest
	if widths[0] != 2 {
		t.Errorf("id width = %d, want 2 (kept natural)", widths[0])
	}
	if widths[2] <= widths[1] {
		t.Errorf("body width %d should be the largest", widths[2])
	}
}

func TestColWidths_CapsSingleHugeColumn(t *testing.T) {
	cols := []string{"id", "huge"}
	rows := [][]string{{"1", strings.Repeat("x", 500)}}
	widths := colWidths(cols, rows, 200)
	if widths[1] != colMaxW {
		t.Errorf("huge width = %d, want %d", widths[1], colMaxW)
	}
}

func TestRenderRowDetail_ShowsFullValue(t *testing.T) {
	val := strings.Repeat("z", 100)
	out := RenderRowDetail("users", []string{"id", "text"}, []string{"1", val}, 0, 0, 40, 20)
	// the value is wrapped, so every character must still be present
	if got := strings.Count(out, "z"); got != 100 {
		t.Errorf("found %d 'z', want 100 (full value)", got)
	}
	if !strings.Contains(out, "id") || !strings.Contains(out, "text") {
		t.Error("column names must appear")
	}
}

func TestRenderRowDetail_HighlightsCursor(t *testing.T) {
	out := RenderRowDetail("users", []string{"id", "text"}, []string{"1", "hello"}, 1, 0, 40, 20)
	plain := ansi.Strip(out)
	if !strings.Contains(plain, "> text:") {
		t.Errorf("expected cursor '> text:' in plain output: %q", plain)
	}
	if !strings.Contains(plain, "  id:") {
		t.Errorf("expected unselected '  id:' in plain output: %q", plain)
	}
}

func TestRenderRowDetail_Scrolls(t *testing.T) {
	val := strings.Repeat("y", 100)
	out0 := RenderRowDetail("t", []string{"id", "body"}, []string{"1", val}, 0, 0, 40, 3)
	out1 := RenderRowDetail("t", []string{"id", "body"}, []string{"1", val}, 0, 5, 40, 3)
	if out0 == out1 {
		t.Error("scrolling should change the visible content")
	}
}

func ansiStripLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		out = append(out, ansi.Strip(l))
	}
	return out
}

func TestRenderDataTable_HorizontalScroll(t *testing.T) {
	cols := []string{"id", "col_alpha", "col_beta", "col_gamma"}
	rows := [][]string{
		{"1", "alpha_val", "beta_val", "gamma_val"},
		{"2", "alpha_two", "beta_two", "gamma_two"},
	}

	// Width 25 only fits ~2 columns.
	// At colScroll = 0: id and col_alpha should be visible.
	out0 := RenderDataTable(cols, rows, 0, 0, 25, 10)
	plain0 := ansi.Strip(out0)
	if !strings.Contains(plain0, "id") || !strings.Contains(plain0, "col_alpha") {
		t.Errorf("colScroll=0 should show 'id' and 'col_alpha': %s", plain0)
	}
	if strings.Contains(plain0, "col_beta") {
		t.Errorf("colScroll=0 should not show 'col_beta' in narrow width: %s", plain0)
	}

	// At colScroll = 1: col_alpha and col_beta should be visible, id should not.
	out1 := RenderDataTable(cols, rows, 0, 1, 25, 10)
	plain1 := ansi.Strip(out1)
	if strings.Contains(plain1, "id |") {
		t.Errorf("colScroll=1 should not show 'id' column: %s", plain1)
	}
	if !strings.Contains(plain1, "col_alpha") {
		t.Errorf("colScroll=1 should show 'col_alpha': %s", plain1)
	}
}

func TestRenderDataTable_ScrollIndicators(t *testing.T) {
	cols := []string{"col1", "col2", "col3", "col4", "col5"}
	rows := [][]string{
		{"v1", "v2", "v3", "v4", "v5"},
	}

	// colScroll=0 with narrow width (20) has more columns to right -> should contain ▶
	out0 := RenderDataTable(cols, rows, 0, 0, 20, 10)
	plain0 := ansi.Strip(out0)
	if !strings.Contains(plain0, "▶") {
		t.Errorf("colScroll=0 with remaining cols should show ▶: %s", plain0)
	}
	if strings.Contains(plain0, "◀") {
		t.Errorf("colScroll=0 should not show ◀: %s", plain0)
	}

	// colScroll=2 in the middle -> should show both ◀ and ▶
	outMid := RenderDataTable(cols, rows, 0, 2, 15, 10)
	plainMid := ansi.Strip(outMid)
	if !strings.Contains(plainMid, "◀") {
		t.Errorf("colScroll=2 should show ◀: %s", plainMid)
	}
	if !strings.Contains(plainMid, "▶") {
		t.Errorf("colScroll=2 with remaining cols should show ▶: %s", plainMid)
	}

	// colScroll at the last column -> should show ◀ and no ▶
	outEnd := RenderDataTable(cols, rows, 0, 4, 20, 10)
	plainEnd := ansi.Strip(outEnd)
	if !strings.Contains(plainEnd, "◀") {
		t.Errorf("colScroll at end should show ◀: %s", plainEnd)
	}
	if strings.Contains(plainEnd, "▶") {
		t.Errorf("colScroll at end should not show ▶: %s", plainEnd)
	}
}

func TestColScrollWindow_BoundsAndClamping(t *testing.T) {
	naturalW := []int{10, 10, 10}

	// colScroll < 0 clamped to 0
	start, widths, hasLeft, _ := colScrollWindow(naturalW, -5, 50)
	if start != 0 || len(widths) != 3 || hasLeft {
		t.Errorf("negative colScroll should clamp to 0: got start=%d, len=%d, hasLeft=%v", start, len(widths), hasLeft)
	}

	// colScroll >= len clamped to last
	start, widths, hasLeft, hasRight := colScrollWindow(naturalW, 10, 50)
	if start != 2 || len(widths) != 1 || !hasLeft || hasRight {
		t.Errorf("out-of-bounds colScroll should clamp: got start=%d, len=%d, hasLeft=%v, hasRight=%v", start, len(widths), hasLeft, hasRight)
	}
}

func TestRenderDataTable_RightIndicatorDoesNotWrapOrExceedWidth(t *testing.T) {
	// 8 columns resembling empleado_con_skill
	cols := []string{
		"id_skill",
		"deporte_empleado",
		"nombre_empleado",
		"id_empleado",
		"apellido_empleado",
		"ciudad_empleado",
		"pais_empleado",
		"federated",
	}
	rows := [][]string{
		{
			"00000000-0000-0000-0000-000000000001",
			"Futbol",
			"Agustin",
			"11111111-1111-1111-1111-111111111111",
			"Monetti",
			"Lobos",
			"Argentina",
			"true",
		},
	}

	// Compute natural widths:
	// id_skill (36) + 3 + deporte_empleado (16) + 3 + nombre_empleado (15) + 3 +
	// id_empleado (36) + 3 + apellido_empleado (17) + 3 + ciudad_empleado (15) = 151.
	// If width is 151, the 6 columns take exactly 151.
	// Adding " ▶" (2 chars) would make 153 > 151.
	// colScrollWindow must NOT include ciudad_empleado in this width, so header fits <= 151.
	width := 151
	out := RenderDataTable(cols, rows, 0, 0, width, 10)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")

	// lines[0]: header
	// lines[1]: separator
	// lines[2]: row 0
	if len(lines) != 3 {
		t.Fatalf("expected exactly 3 lines (header, sep, row), got %d lines: %v", len(lines), lines)
	}

	headerPlain := ansi.Strip(lines[0])
	if !strings.HasSuffix(headerPlain, " ▶") {
		t.Errorf("header should end with ' ▶', got: %q", headerPlain)
	}

	for idx, line := range lines {
		plain := ansi.Strip(line)
		w := runewidth.StringWidth(plain)
		if w > width {
			t.Errorf("line %d width %d exceeds max width %d: %q", idx, w, width, plain)
		}
	}

	// Also verify that inside boxed() (which wraps overflowing lines), no wrap occurs
	box := boxed(out, "test", width+4, 10, false)
	boxLines := strings.Split(box, "\n")
	// Line 0 is top border, Line 1 is header. If wrapped, Line 2 would contain " ▶" alone.
	for i, l := range boxLines {
		plain := strings.TrimSpace(ansi.Strip(l))
		if plain == "▶" || plain == "│ ▶" {
			t.Errorf("box wrapped '▶' onto its own line %d: %s", i, l)
		}
	}
}

func TestRenderDataTable_LeftIndicatorAlignment(t *testing.T) {
	cols := []string{"col0", "col1", "col2", "col3"}
	rows := [][]string{
		{"val0", "val1", "val2", "val3"},
	}

	// colScroll=1 hasLeft=true
	out := RenderDataTable(cols, rows, 0, 1, 30, 10)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines, got %d", len(lines))
	}

	header := ansi.Strip(lines[0])
	sep := ansi.Strip(lines[1])
	row := ansi.Strip(lines[2])

	if !strings.HasPrefix(header, "◀ ") {
		t.Errorf("header should start with '◀ ', got %q", header)
	}
	if !strings.HasPrefix(sep, "--") {
		t.Errorf("sep should start with '--', got %q", sep)
	}
	if !strings.HasPrefix(row, "  ") {
		t.Errorf("row should start with '  ', got %q", row)
	}
}

func TestRenderDataTable_NullAlignment(t *testing.T) {
	cols := []string{"id", "email", "is_active", "name", "password", "phone", "role", "surname", "username"}
	rows := [][]string{
		{"1", "buyerdemo@mail.com", "1", "Ana", "$2a$10$2y4HxHVDZnjgPyyLlDTNEOGaqoWUabcdef123", "", "BUYER", "Compradora", "buyerdemo"},
		{"2", "sellerdemo@mail.com", "0", "Luis", "$2a$10$fY7oAN0uShWAeyVNl.pGYehUvoIvabcdef123", "", "SELLER", "Vendedor", "sellerdemo"},
	}
	nulls := [][]bool{
		{false, false, false, false, false, true, false, false, false},
		{false, false, false, false, false, true, false, false, false},
	}

	for _, cursor := range []int{-1, 0, 1} {
		out := RenderDataTableWithNulls(cols, rows, nulls, cursor, 0, 200, 10)
		lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
		if len(lines) != 4 {
			t.Fatalf("cursor %d: expected 4 lines (header, sep, row0, row1), got %d: %v", cursor, len(lines), lines)
		}

		headerPlain := ansi.Strip(lines[0])
		sepPlain := ansi.Strip(lines[1])
		row0Plain := ansi.Strip(lines[2])
		row1Plain := ansi.Strip(lines[3])

		wHeader := runewidth.StringWidth(headerPlain)
		wSep := runewidth.StringWidth(sepPlain)
		wRow0 := runewidth.StringWidth(row0Plain)
		wRow1 := runewidth.StringWidth(row1Plain)

		if wRow0 != wHeader || wRow1 != wHeader || wSep != wHeader {
			t.Errorf("cursor %d: line width mismatch: header=%d, sep=%d, row0=%d, row1=%d", cursor, wHeader, wSep, wRow0, wRow1)
		}

		// Find separator positions in header (using runes for unicode-safe visual column alignment)
		rHeader := []rune(headerPlain)
		rRow0 := []rune(row0Plain)
		rRow1 := []rune(row1Plain)

		var headerSepIndices []int
		for i := 0; i < len(rHeader)-2; i++ {
			if string(rHeader[i:i+3]) == " | " {
				headerSepIndices = append(headerSepIndices, i)
			}
		}

		// Verify every separator position matches in row0 and row1
		for _, idx := range headerSepIndices {
			if idx+3 > len(rRow0) || string(rRow0[idx:idx+3]) != " | " {
				t.Errorf("cursor %d row0: missing separator ' | ' at rune index %d: line=%q", cursor, idx, row0Plain)
			}
			if idx+3 > len(rRow1) || string(rRow1[idx:idx+3]) != " | " {
				t.Errorf("cursor %d row1: missing separator ' | ' at rune index %d: line=%q", cursor, idx, row1Plain)
			}
		}

		// Verify NULL glyph is present in plain row text
		if !strings.Contains(row0Plain, "∅") || !strings.Contains(row1Plain, "∅") {
			t.Errorf("cursor %d: expected NULL glyph ∅ in rows: row0=%q, row1=%q", cursor, row0Plain, row1Plain)
		}
	}
}

func TestRenderDataTable_DistinguishesNullAndEmptyString(t *testing.T) {
	cols := []string{"id", "notes"}
	rows := [][]string{
		{"1", ""}, // SQL NULL
		{"2", ""}, // empty string VARCHAR
	}
	nulls := [][]bool{
		{false, true},
		{false, false},
	}

	out := RenderDataTableWithNulls(cols, rows, nulls, -1, 0, 50, 10)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines, got %d", len(lines))
	}

	row0Plain := ansi.Strip(lines[2])
	row1Plain := ansi.Strip(lines[3])

	// row 0 should contain the NULL symbol ∅
	if !strings.Contains(row0Plain, "∅") {
		t.Errorf("row 0 (NULL) should contain ∅: %q", row0Plain)
	}

	// row 1 (empty string) must NOT contain ∅
	if strings.Contains(row1Plain, "∅") {
		t.Errorf("row 1 (empty string) must NOT contain ∅: %q", row1Plain)
	}

	// Both rows must have identical total width
	if runewidth.StringWidth(row0Plain) != runewidth.StringWidth(row1Plain) {
		t.Errorf("row width mismatch: row0=%d, row1=%d", runewidth.StringWidth(row0Plain), runewidth.StringWidth(row1Plain))
	}
}

func TestRenderRowDetail_NullValue(t *testing.T) {
	out := RenderRowDetail("users", []string{"id", "phone"}, []string{"1", ""}, 1, 0, 40, 20)
	plain := ansi.Strip(out)
	if !strings.Contains(plain, "∅") {
		t.Errorf("detail view for NULL field must contain ∅: %q", plain)
	}
}
func TestRenderSidebar_GroupsRelationTables(t *testing.T) {
	b := &browser.Browser{
		Items: []store.CatalogItem{
			{Name: "discounts", Kind: store.CatalogItemTable, Group: "TABLES"},
			{Name: "discount_products", Kind: store.CatalogItemRelation, Group: "RELATIONS", Badge: "relation"},
		},
		ActiveTable: "discounts",
	}
	out := RenderSidebar(b, 0, 40, 5)
	if !strings.Contains(out, "TABLES") || !strings.Contains(out, "RELATIONS") {
		t.Fatalf("sidebar groups missing: %q", out)
	}
	if !strings.Contains(out, "discount_products [relation]") {
		t.Errorf("relation item missing: %q", out)
	}
	if got := SidebarItemAtLine(b, 0, 5, 3); got != 1 {
		t.Errorf("relation line maps to item %d, want 1", got)
	}
}
