package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/agmonetti/picklock/internal/browser"
	"github.com/agmonetti/picklock/internal/editor"
	"github.com/agmonetti/picklock/internal/store"
)

func TestRenderWorkspace_ShowsPaneTitles(t *testing.T) {
	out := RenderWorkspace(sampleTestBrowser(), NewEditorScreen(), sampleTestEditor(),
		FocusSidebar, false, ComputeLayout(100, 24, true, true, true, 0, 0), 0, 0, 100, 24)

	if !strings.Contains(out, "TABLES") {
		t.Error("workspace must show the TABLES sidebar title")
	}
	if !strings.Contains(out, "SQL EDITOR") {
		t.Error("workspace must show the SQL EDITOR title")
	}
	if !strings.Contains(out, "users · 2 rows") {
		t.Errorf("main pane title missing: %q", out)
	}
	// the column header is rendered and styled (ANSI)
	lines := strings.Split(out, "\n")
	found := false
	for _, l := range lines {
		if strings.Contains(l, "id") && strings.Contains(l, "name") && strings.Contains(l, "email") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("column header missing: %q", out)
	}
}

func TestRenderWorkspace_EmptyTableShowsColumns(t *testing.T) {
	b := &browser.Browser{
		Tables:      []string{"orders", "users"},
		ActiveTable: "users",
		Columns: []store.Column{
			{Name: "id", Type: "INTEGER", PK: true},
			{Name: "name", Type: "TEXT"},
			{Name: "email", Type: "TEXT"},
		},
		Rows:      nil,
		TotalRows: 0,
		PageSize:  50,
	}
	out := RenderWorkspace(b, NewEditorScreen(), sampleTestEditor(),
		FocusMain, false, ComputeLayout(100, 24, true, true, true, 0, 0), 0, 0, 100, 24)

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

func TestRenderWorkspace_ExactHeight(t *testing.T) {
	for _, h := range []int{12, 20, 30} {
		out := RenderWorkspace(sampleTestBrowser(), NewEditorScreen(), sampleTestEditor(),
			FocusSidebar, false, ComputeLayout(100, h, true, true, true, 0, 0), 0, 0, 100, h)
		if n := len(strings.Split(out, "\n")); n != h {
			t.Errorf("height=%d: got %d lines", h, n)
		}
	}
}

func TestRenderWorkspace_ExactWidth(t *testing.T) {
	for _, w := range []int{60, 100, 150} {
		out := RenderWorkspace(sampleTestBrowser(), NewEditorScreen(), sampleTestEditor(),
			FocusSidebar, false, ComputeLayout(w, 20, true, true, true, 0, 0), 0, 0, w, 20)
		for i, line := range strings.Split(out, "\n") {
			if got := runewidth.StringWidth(ansi.Strip(line)); got != w {
				t.Errorf("width=%d: line %d is %d cols", w, i, got)
			}
		}
	}
}

func TestRenderWorkspace_HasPanelGapsAndPadding(t *testing.T) {
	out := RenderWorkspace(sampleTestBrowser(), NewEditorScreen(), sampleTestEditor(),
		FocusSidebar, false, ComputeLayout(100, 24, true, true, true, 0, 0), 0, 0, 100, 24)
	lines := strings.Split(out, "\n")

	// the sidebar and main boxes are separated by exactly one char
	top := ansi.Strip(lines[0])
	if !strings.Contains(top, "╮ ╭") {
		t.Errorf("expected a 1-char gap between the panes: %q", top)
	}
	// content is padded one char inside the border
	foundTitle := false
	for _, l := range lines {
		if s := ansi.Strip(l); strings.Contains(s, "TABLES") {
			if !strings.HasPrefix(s, "╭─ TABLES") {
				t.Errorf("sidebar title must live in the top border: %q", s)
			}
			foundTitle = true
		}
	}
	if !foundTitle {
		t.Error("sidebar title not rendered")
	}
}

func TestRenderWorkspace_WithoutSidebarSpansWidth(t *testing.T) {
	// with the sidebar hidden the output must not contain the sidebar tables
	out := RenderWorkspace(sampleTestBrowser(), NewEditorScreen(), sampleTestEditor(),
		FocusSidebar, false, ComputeLayout(100, 20, false, true, true, 0, 0), 0, 0, 100, 20)
	if strings.Contains(out, "TABLES") {
		t.Error("sidebar must be hidden")
	}
}

func TestRenderWorkspace_ToggleCombinations(t *testing.T) {
	b := sampleTestBrowser()
	es := NewEditorScreen()
	ed := sampleTestEditor()

	// Only Main visible
	lMainOnly := ComputeLayout(100, 24, false, true, false, 0, 0)
	outMainOnly := RenderWorkspace(b, es, ed, FocusMain, false, lMainOnly, 0, 0, 100, 24)
	if !strings.Contains(outMainOnly, "users") || strings.Contains(outMainOnly, "SQL EDITOR") {
		t.Errorf("main only: should have users, not SQL EDITOR: %q", outMainOnly)
	}
	if n := len(strings.Split(outMainOnly, "\n")); n != 24 {
		t.Errorf("main only: height = %d, want 24", n)
	}

	// Only Editor visible
	lEdOnly := ComputeLayout(100, 24, false, false, true, 0, 0)
	outEdOnly := RenderWorkspace(b, es, ed, FocusEditor, false, lEdOnly, 0, 0, 100, 24)
	if strings.Contains(outEdOnly, "users ·") || !strings.Contains(outEdOnly, "SQL EDITOR") {
		t.Errorf("editor only: should have SQL EDITOR, not users: %q", outEdOnly)
	}
	if n := len(strings.Split(outEdOnly, "\n")); n != 24 {
		t.Errorf("editor only: height = %d, want 24", n)
	}

	// Sidebar + Main (Editor hidden)
	lSideMain := ComputeLayout(100, 24, true, true, false, 0, 0)
	outSideMain := RenderWorkspace(b, es, ed, FocusMain, false, lSideMain, 0, 0, 100, 24)
	if !strings.Contains(outSideMain, "TABLES") || !strings.Contains(outSideMain, "users ·") || strings.Contains(outSideMain, "SQL EDITOR") {
		t.Errorf("sidebar+main: unexpected output: %q", outSideMain)
	}
	if n := len(strings.Split(outSideMain, "\n")); n != 24 {
		t.Errorf("sidebar+main: height = %d, want 24", n)
	}

	// Sidebar + Editor (Main hidden)
	lSideEd := ComputeLayout(100, 24, true, false, true, 0, 0)
	outSideEd := RenderWorkspace(b, es, ed, FocusEditor, false, lSideEd, 0, 0, 100, 24)
	if !strings.Contains(outSideEd, "TABLES") || strings.Contains(outSideEd, "users ·") || !strings.Contains(outSideEd, "SQL EDITOR") {
		t.Errorf("sidebar+editor: unexpected output: %q", outSideEd)
	}
	if n := len(strings.Split(outSideEd, "\n")); n != 24 {
		t.Errorf("sidebar+editor: height = %d, want 24", n)
	}
}

func sampleTestBrowser() *browser.Browser {
	return &browser.Browser{
		Tables:      []string{"orders", "users"},
		ActiveTable: "users",
		Columns: []store.Column{
			{Name: "id", Type: "INTEGER", PK: true},
			{Name: "name", Type: "TEXT"},
			{Name: "email", Type: "TEXT"},
		},
		Rows:      [][]string{{"1", "Alice", "alice@test.com"}, {"2", "Bob", "bob@test.com"}},
		TotalRows: 2,
		PageSize:  50,
	}
}

func sampleTestEditor() *editor.Editor {
	return &editor.Editor{Buffer: "SELECT * FROM users", History: editor.NewHistory()}
}

func TestComputeLayout_Automatic(t *testing.T) {
	l := ComputeLayout(100, 30, true, true, true, 0, 0)
	if !l.ShowSidebar {
		t.Fatal("sidebar should be shown")
	}
	if l.SidebarW != 20 { // width/5
		t.Errorf("SidebarW = %d, want 20", l.SidebarW)
	}
	if l.EditorH != 9 { // height/4 min 9
		t.Errorf("EditorH = %d, want 9", l.EditorH)
	}
	if l.MainH != 30-9-1 {
		t.Errorf("MainH = %d, want %d", l.MainH, 30-9-1)
	}
	if l.RightW != 100-20-1 {
		t.Errorf("RightW = %d, want %d", l.RightW, 100-20-1)
	}
}

func TestComputeLayout_CustomClamped(t *testing.T) {
	l := ComputeLayout(100, 30, true, true, true, 200, 1000)
	if l.SidebarW > 100-RightMinW-1 {
		t.Errorf("SidebarW = %d, not clamped to width", l.SidebarW)
	}
	if l.EditorH != 30-MainMinH-1 {
		t.Errorf("EditorH = %d, want %d", l.EditorH, 30-MainMinH-1)
	}

	l = ComputeLayout(100, 30, true, true, true, 3, 2)
	if l.SidebarW < SidebarMinW {
		t.Errorf("SidebarW = %d, below min %d", l.SidebarW, SidebarMinW)
	}
	if l.EditorH < EditorMinH {
		t.Errorf("EditorH = %d, below min %d", l.EditorH, EditorMinH)
	}
}

func TestComputeLayout_RespectsCustomValue(t *testing.T) {
	l := ComputeLayout(100, 30, true, true, true, 32, 14)
	if l.SidebarW != 32 {
		t.Errorf("SidebarW = %d, want 32", l.SidebarW)
	}
	if l.EditorH != 14 {
		t.Errorf("EditorH = %d, want 14", l.EditorH)
	}
	if l.MainH != 30-14-1 {
		t.Errorf("MainH = %d, want %d", l.MainH, 30-14-1)
	}
}

func TestComputeLayout_SidebarHiddenOnSmallWidth(t *testing.T) {
	l := ComputeLayout(50, 20, true, true, true, 20, 8)
	if l.ShowSidebar {
		t.Error("sidebar must be hidden below 60 columns")
	}
	if l.RightW != 50 {
		t.Errorf("RightW = %d, want full width", l.RightW)
	}
}

func TestComputeLayout_TogglePanels(t *testing.T) {
	// Editor hidden -> Main takes full height
	l1 := ComputeLayout(100, 30, true, true, false, 20, 10)
	if l1.EditorH != 0 || l1.MainH != 30 {
		t.Errorf("Editor hidden: EditorH = %d, MainH = %d, want 0 and 30", l1.EditorH, l1.MainH)
	}

	// Main hidden -> Editor takes full height
	l2 := ComputeLayout(100, 30, true, false, true, 20, 10)
	if l2.MainH != 0 || l2.EditorH != 30 {
		t.Errorf("Main hidden: MainH = %d, EditorH = %d, want 0 and 30", l2.MainH, l2.EditorH)
	}

	// All false -> guard defaults showMain = true
	l3 := ComputeLayout(100, 30, false, false, false, 0, 0)
	if !l3.ShowMain {
		t.Errorf("All false guard: ShowMain must be true")
	}
}

func TestComputeLayout_ExtremeResizeNeverOverflows(t *testing.T) {
	// Extreme Editor resizing up and down
	for _, h := range []int{15, 25, 40} {
		for _, edH := range []int{-50, 1, 3, 5, 20, 100, 1000} {
			l := ComputeLayout(100, h, true, true, true, 20, edH)
			if l.EditorH < EditorMinH {
				t.Errorf("h=%d edH=%d: EditorH=%d below EditorMinH %d", h, edH, l.EditorH, EditorMinH)
			}
			if l.MainH < MainMinH {
				t.Errorf("h=%d edH=%d: MainH=%d below MainMinH %d", h, edH, l.MainH, MainMinH)
			}
			if total := l.MainH + 1 + l.EditorH; total != h {
				t.Errorf("h=%d edH=%d: total vertical = %d, want %d", h, edH, total, h)
			}

			out := RenderWorkspace(sampleTestBrowser(), NewEditorScreen(), sampleTestEditor(),
				FocusMain, false, l, 0, 0, 100, h)
			if lines := len(strings.Split(out, "\n")); lines != h {
				t.Errorf("h=%d edH=%d: rendered lines = %d, want %d", h, edH, lines, h)
			}
		}
	}

	// Extreme Sidebar resizing
	for _, w := range []int{70, 100, 150} {
		for _, sideW := range []int{-50, 0, 5, 10, 50, 120, 1000} {
			l := ComputeLayout(w, 25, true, true, true, sideW, 10)
			if l.SidebarW < SidebarMinW {
				t.Errorf("w=%d sideW=%d: SidebarW=%d below SidebarMinW %d", w, sideW, l.SidebarW, SidebarMinW)
			}
			if l.RightW < RightMinW {
				t.Errorf("w=%d sideW=%d: RightW=%d below RightMinW %d", w, sideW, l.RightW, RightMinW)
			}
			if total := l.SidebarW + 1 + l.RightW; total != w {
				t.Errorf("w=%d sideW=%d: total horizontal = %d, want %d", w, sideW, total, w)
			}
		}
	}
}
