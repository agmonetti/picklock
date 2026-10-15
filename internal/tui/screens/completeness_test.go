package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/agmonetti/picklock/internal/browser"
	"github.com/agmonetti/picklock/internal/editor"
	"github.com/agmonetti/picklock/internal/store"
)

func TestBrowserMarksPartialCellsWithoutChangingPayload(t *testing.T) {
	data := &store.TabularData{
		Columns: []string{"preview", "deferred", "null", "empty"},
		Rows:    [][]string{{"prefix", "must-not-display", "ignored", ""}}, Nulls: [][]bool{{false, false, true, false}}, Affected: -1,
		Incomplete: map[store.CellPosition]store.IncompleteCell{
			{Row: 0, Column: 0}: {State: store.CellPreview},
			{Row: 0, Column: 1}: {State: store.CellDeferred},
			{Row: 0, Column: 2}: {State: store.CellDeferred},
		},
	}
	b := &browser.Browser{Tables: []string{"items"}, Data: data}
	got := ansi.Strip(RenderMainBrowser(b, 0, 120, 10))
	for _, want := range []string{"<preview>", "prefix", "<deferred>", "∅", "INCOMPLETE"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "must-not-display") || strings.Contains(got, "ignored") {
		t.Fatalf("deferred/NULL payload leaked: %q", got)
	}
	if strings.Count(got, "∅") != 1 {
		t.Fatalf("empty value confused with NULL: %q", got)
	}
	if data.Rows[0][0] != "prefix" || data.Rows[0][1] != "must-not-display" || data.Rows[0][2] != "ignored" || data.Rows[0][3] != "" {
		t.Fatalf("render modified source data: %#v", data.Rows)
	}
}

func TestEditorPreservesAbsoluteIncompletePositionsWhenScrolled(t *testing.T) {
	data := &store.TabularData{
		Columns: []string{"value"}, Rows: [][]string{{"first"}, {"second"}, {"third"}, {"fourth"}, {"last"}}, Affected: -1,
		Incomplete: map[store.CellPosition]store.IncompleteCell{{Row: 1, Column: 0}: {State: store.CellDeferred}},
	}
	screen := NewEditorScreen()
	screen.SetResultScroll(1)
	e := &editor.Editor{Data: data}
	got := ansi.Strip(screen.View(e, 100, 10))
	if !strings.Contains(got, "<deferred>") || strings.Contains(got, "second") || !strings.Contains(got, "INCOMPLETE") {
		t.Fatalf("scrolled metadata lost: %q", got)
	}
	if strings.Contains(got, "first") {
		t.Fatalf("editor did not scroll: %q", got)
	}
	if data.Rows[1][0] != "second" {
		t.Fatal("editor rewrote deferred source value")
	}
}

func TestRowDetailDistinguishesNullEmptyAndPartial(t *testing.T) {
	cols := []string{"null", "empty", "preview", "deferred"}
	vals := []string{"ignored", "", "prefix", "must-not-display"}
	metadata := map[int]store.IncompleteCell{0: {State: store.CellDeferred}, 2: {State: store.CellPreview}, 3: {State: store.CellDeferred}}
	got := ansi.Strip(RenderRowDetailWithMetadata("detail", cols, vals, []bool{true, false, false, false}, metadata, 0, 0, 100, 30))
	if strings.Count(got, "∅") != 1 || !strings.Contains(got, "<preview> prefix") || !strings.Contains(got, "<deferred>") {
		t.Fatalf("detail contracts lost: %q", got)
	}
	if strings.Contains(got, "ignored") || strings.Contains(got, "must-not-display") {
		t.Fatalf("hidden values leaked: %q", got)
	}
}

func TestNonTabularRenderMarksIncomplete(t *testing.T) {
	for _, state := range []store.CellState{store.CellPreview, store.CellDeferred} {
		cell := store.IncompleteCell{State: state}
		cases := map[string]store.DataView{
			"document":  &store.DocumentData{Documents: []store.DocumentItem{{ID: "1", Summary: "prefix", RawJSON: "prefix", Incomplete: &cell}}},
			"kv string": &store.KeyValueData{Key: "key", Type: "string", Entries: []store.KVEntry{{Value: "prefix", Incomplete: &cell}}},
			"kv hash":   &store.KeyValueData{Key: "key", Type: "hash", Entries: []store.KVEntry{{Index: "field", Value: "prefix", Incomplete: &cell}}},
			"graph":     &store.GraphData{Nodes: []store.GraphNode{{ID: "1", Properties: map[string]string{"body": "prefix"}, Incomplete: map[string]store.IncompleteCell{"body": cell}}}},
			"raw":       &store.RawTextData{Text: "prefix", Incomplete: &cell},
		}
		for name, data := range cases {
			t.Run(string(state)+"/"+name, func(t *testing.T) {
				b := &browser.Browser{Tables: []string{"items"}, Data: data}
				got := ansi.Strip(RenderMainBrowser(b, 0, 120, 10))
				if !strings.Contains(got, "<"+string(state)) || !strings.Contains(got, "INCOMPLETE") {
					t.Fatalf("missing incomplete state: %q", got)
				}
				if strings.Contains(got, "prefix") != (state == store.CellPreview) {
					t.Fatalf("state/value mismatch: %q", got)
				}
			})
		}
	}
}

func TestEmptyCutoffVisibleInBrowserAndEditor(t *testing.T) {
	status := store.ResultStatus{Reason: store.LimitResultBytes, RetainedItems: 0, RetainedBytes: 0}
	cases := []store.DataView{
		&store.TabularData{Affected: -1, Status: status}, &store.DocumentData{Status: status},
		&store.KeyValueData{Status: status}, &store.GraphData{Status: status}, &store.RawTextData{Status: status},
	}
	for _, data := range cases {
		b := &browser.Browser{Tables: []string{"items"}, Data: data}
		browserOutput := ansi.Strip(RenderMainBrowser(b, 0, 120, 10))
		editorOutput := ansi.Strip(NewEditorScreen().View(&editor.Editor{Data: data}, 120, 20))
		for _, got := range []string{browserOutput, editorOutput} {
			if !strings.Contains(got, "INCOMPLETE") || !strings.Contains(got, string(store.LimitResultBytes)) || !strings.Contains(got, "0 items") || !strings.Contains(got, "0 B") {
				t.Errorf("%T hides empty cutoff or retained-byte count: %q", data, got)
			}
		}
	}
}

func TestHiddenPreviewSuffixDoesNotChangePresentation(t *testing.T) {
	var previous string
	for _, size := range []int{1 << 10, 1 << 20} {
		value := "prefix" + strings.Repeat("z", size)
		data := &store.TabularData{
			Columns: []string{"body"}, Rows: [][]string{{value}}, Affected: -1,
			Incomplete: map[store.CellPosition]store.IncompleteCell{{Row: 0, Column: 0}: {State: store.CellPreview}},
		}
		got := RenderMainBrowser(&browser.Browser{Tables: []string{"heavy"}, Data: data}, 0, 80, 5)
		if previous != "" && got != previous {
			t.Fatal("hidden preview suffix changed bounded presentation")
		}
		if len(got) > 1024 {
			t.Fatalf("presentation retained hidden payload: %d bytes", len(got))
		}
		if data.Rows[0][0] != value {
			t.Fatal("presentation modified retained preview")
		}
		previous = got
	}
}

func TestGraphEdgeIncompletenessVisibleWithoutChangingNavigation(t *testing.T) {
	graph := &store.GraphData{
		Nodes: []store.GraphNode{{ID: "node", Properties: map[string]string{"body": "complete"}}},
		Edges: []store.GraphEdge{{ID: "edge", Incomplete: map[string]store.IncompleteCell{"body": {State: store.CellDeferred}}}},
	}
	b := &browser.Browser{Tables: []string{"graph"}, Data: graph}
	got := ansi.Strip(RenderMainBrowser(b, 0, 120, 10))
	if !strings.Contains(got, "node") || !strings.Contains(got, "INCOMPLETE") {
		t.Fatalf("edge incompleteness hidden: %q", got)
	}
}

func TestEditorCutoffStaysVisibleInShortPane(t *testing.T) {
	data := &store.TabularData{
		Columns: []string{"id"}, Rows: [][]string{{"1"}, {"2"}},
		Affected: -1, Truncated: true,
	}
	for _, height := range []int{6, 7, 10} {
		got := ansi.Strip(NewEditorScreen().View(&editor.Editor{Data: data}, 100, height))
		lines := strings.Split(got, "\n")
		visible := strings.Join(lines[:min(height, len(lines))], "\n")
		if strings.Count(visible, "INCOMPLETE") != 1 || !strings.Contains(visible, "filter/select less") || !strings.Contains(visible, "cutoff") {
			t.Fatalf("height %d hides cutoff guidance or duplicates status: %q", height, visible)
		}
	}
}
