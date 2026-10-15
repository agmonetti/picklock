package tui

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/agmonetti/picklock/internal/browser"
	"github.com/agmonetti/picklock/internal/prefs"
	"github.com/agmonetti/picklock/internal/store"
)

func TestDetailCopiesOnlyCompleteFields(t *testing.T) {
	oldClipboard := clipboardWriteAll
	var copied []string
	clipboardWriteAll = func(text string) error { copied = append(copied, text); return nil }
	t.Cleanup(func() { clipboardWriteAll = oldClipboard })
	data := &store.TabularData{
		Columns: []string{"complete", "preview", "deferred", "null", "empty"},
		Rows:    [][]string{{"full-value", "prefix", "not-loaded", "ignored", ""}},
		Nulls:   [][]bool{{false, false, false, true, false}}, Affected: -1,
		Incomplete: map[store.CellPosition]store.IncompleteCell{
			{Row: 0, Column: 1}: {State: store.CellPreview},
			{Row: 0, Column: 2}: {State: store.CellDeferred},
			{Row: 0, Column: 3}: {State: store.CellDeferred},
		},
	}
	m := &Model{width: 120, height: 30}
	m.openDetailFromBrowser(&browser.Browser{Data: data})
	m.copyDetailValue()
	if len(copied) != 1 || copied[0] != "full-value" {
		t.Fatalf("complete field blocked: %#v", copied)
	}
	for _, cursor := range []int{1, 2} {
		m.detailCursor = cursor
		m.copyDetailValue()
		m.copyDetailField()
		if len(copied) != 1 || !strings.Contains(m.err, "complete value is not loaded") {
			t.Fatalf("partial field copied or unexplained: copied=%#v err=%q", copied, m.err)
		}
	}
	m.copyDetailAll()
	if len(copied) != 1 || !strings.Contains(m.err, "incomplete") {
		t.Fatalf("partial row copied: %#v, %q", copied, m.err)
	}
	m.detailCursor = 3
	m.copyDetailValue()
	m.detailCursor = 4
	m.copyDetailValue()
	if len(copied) != 3 || copied[1] != "NULL" || copied[2] != "" {
		t.Fatalf("NULL/empty copy lost: %#v", copied)
	}
	visible := ansi.Strip(m.renderActiveDetail(120, 30))
	if !strings.Contains(visible, "INCOMPLETE") || !strings.Contains(visible, "<preview> prefix") || !strings.Contains(visible, "<deferred>") {
		t.Fatalf("detail hides incompleteness: %q", visible)
	}
	if strings.Contains(visible, "not-loaded") || strings.Contains(visible, "ignored") {
		t.Fatalf("hidden payload shown: %q", visible)
	}
	if data.Rows[0][1] != "prefix" || data.Rows[0][2] != "not-loaded" {
		t.Fatal("detail rewrote source values")
	}
}

func TestDetailRejectsCutoffRowButAllowsCompleteField(t *testing.T) {
	oldClipboard := clipboardWriteAll
	calls := 0
	clipboardWriteAll = func(text string) error { calls++; return nil }
	t.Cleanup(func() { clipboardWriteAll = oldClipboard })
	for _, data := range []*store.TabularData{
		{Columns: []string{"field"}, Rows: [][]string{{"complete"}}, Affected: -1, Truncated: true},
		{Columns: []string{"field"}, Rows: [][]string{{"complete"}}, Affected: -1, Status: store.ResultStatus{Reason: store.LimitRows, RetainedItems: 1}},
	} {
		m := &Model{}
		m.openDetailFromBrowser(&browser.Browser{Data: data})
		before := calls
		m.copyDetailAll()
		if calls != before || !strings.Contains(m.err, "incomplete") {
			t.Fatalf("cutoff row copied: calls=%d err=%q", calls, m.err)
		}
		m.copyDetailValue()
		if calls != before+1 {
			t.Fatal("complete field in cutoff result blocked")
		}
	}
}

func TestNonTabularIncompleteDetailCannotCopy(t *testing.T) {
	oldClipboard := clipboardWriteAll
	calls := 0
	clipboardWriteAll = func(text string) error { calls++; return nil }
	t.Cleanup(func() { clipboardWriteAll = oldClipboard })
	for _, state := range []store.CellState{store.CellPreview, store.CellDeferred} {
		cell := store.IncompleteCell{State: state}
		cases := map[string]store.DataView{
			"document": &store.DocumentData{Documents: []store.DocumentItem{{ID: "1", RawJSON: "prefix", Incomplete: &cell}}},
			"kv":       &store.KeyValueData{Key: "key", Entries: []store.KVEntry{{Index: "field", Value: "prefix", Incomplete: &cell}}},
			"node":     &store.GraphData{Nodes: []store.GraphNode{{ID: "1", Properties: map[string]string{"body": "prefix"}, Incomplete: map[string]store.IncompleteCell{"body": cell}}}},
			"raw":      &store.RawTextData{Text: "prefix", Incomplete: &cell},
		}
		for name, data := range cases {
			t.Run(string(state)+"/"+name, func(t *testing.T) {
				m := &Model{width: 120, height: 30}
				m.openDetailFromBrowser(&browser.Browser{Data: data})
				before := calls
				m.copyDetailValue()
				m.copyDetailField()
				m.copyDetailAll()
				if calls != before || m.err == "" {
					t.Fatalf("partial detail copied: calls=%d err=%q", calls, m.err)
				}
				visible := ansi.Strip(m.renderActiveDetail(120, 30))
				if !strings.Contains(visible, "INCOMPLETE") || !strings.Contains(visible, "<"+string(state)) {
					t.Fatalf("partial detail unmarked: %q", visible)
				}
			})
		}
	}
}

func TestMouseCannotCopyIncompletePresentation(t *testing.T) {
	oldClipboard := clipboardWriteAll
	calls := 0
	clipboardWriteAll = func(text string) error { calls++; return nil }
	t.Cleanup(func() { clipboardWriteAll = oldClipboard })
	m := &Model{width: 120, height: 30}
	m.openDetailFromBrowser(&browser.Browser{Data: &store.TabularData{
		Columns: []string{"body"}, Rows: [][]string{{"prefix"}}, Affected: -1,
		Incomplete: map[store.CellPosition]store.IncompleteCell{{Row: 0, Column: 0}: {State: store.CellPreview}},
	}})
	m.dragSelecting = true
	m.dragStartLine, m.dragStartCol = 0, 0
	m.handleDetailMouse(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 20, Y: 5})
	if calls != 0 || !strings.Contains(m.err, "incomplete") {
		t.Fatalf("mouse copied presentation marker: calls=%d err=%q", calls, m.err)
	}
}

func TestMouseCannotCopyNullPresentationMarker(t *testing.T) {
	oldClipboard := clipboardWriteAll
	calls := 0
	clipboardWriteAll = func(text string) error { calls++; return nil }
	t.Cleanup(func() { clipboardWriteAll = oldClipboard })
	m := &Model{width: 120, height: 30}
	m.openDetailFromBrowser(&browser.Browser{Data: &store.TabularData{
		Columns: []string{"body"}, Rows: [][]string{{""}}, Nulls: [][]bool{{true}}, Affected: -1,
	}})
	m.dragSelecting = true
	m.handleDetailMouse(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 20, Y: 5})
	if calls != 0 || !strings.Contains(m.err, "NULL") {
		t.Fatalf("mouse copied NULL presentation marker: calls=%d err=%q", calls, m.err)
	}
}

func TestIncompleteGraphEdgesBlockWholeDetailCopy(t *testing.T) {
	oldClipboard := clipboardWriteAll
	calls := 0
	clipboardWriteAll = func(text string) error { calls++; return nil }
	t.Cleanup(func() { clipboardWriteAll = oldClipboard })
	m := &Model{width: 120, height: 30}
	m.openDetailFromBrowser(&browser.Browser{Data: &store.GraphData{
		Nodes: []store.GraphNode{{ID: "1", Properties: map[string]string{"body": "complete"}}},
		Edges: []store.GraphEdge{{ID: "2", Incomplete: map[string]store.IncompleteCell{"body": {State: store.CellDeferred}}}},
	}})
	m.copyDetailValue()
	m.copyDetailAll()
	if calls != 0 || !strings.Contains(m.err, "incomplete") {
		t.Fatalf("graph edge incompleteness ignored: calls=%d err=%q", calls, m.err)
	}
	if got := ansi.Strip(m.renderActiveDetail(120, 30)); !strings.Contains(got, "INCOMPLETE") {
		t.Fatalf("graph edge incompleteness not visible: %q", got)
	}
}

func TestSpooledCellCopyLimitAndFileSave(t *testing.T) {
	oldClipboard := clipboardWriteAll
	var copied string
	clipboardWriteAll = func(text string) error { copied = text; return nil }
	t.Cleanup(func() { clipboardWriteAll = oldClipboard })

	source, err := os.CreateTemp(t.TempDir(), "spooled")
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("complete-content")
	if _, err := source.Write(payload); err != nil {
		t.Fatal(err)
	}
	m := &Model{detailCursor: 0, detailCols: []string{"payload"}, detailCellFile: source,
		detailCellSize: int64(len(payload)), prefs: prefs.Default()}
	m.copyDetailValue()
	if copied != string(payload) {
		t.Fatalf("copied = %q, want full spooled bytes", copied)
	}

	m.prefs.EditorMaxCellBytes = len(payload) - 1
	copied = ""
	m.copyDetailValue()
	if copied != "" || !strings.Contains(m.err, "save it to a file") {
		t.Fatalf("over-limit clipboard copy = %q, error %q", copied, m.err)
	}

	dest := filepath.Join(t.TempDir(), "nested", "saved.bin")
	m.prefs.EditorMaxCellBytes = 1
	m.exportInput.SetValue(dest)
	m.detailSaving = true
	_, _ = m.handleDetailSaveKeys(tea.KeyMsg{Type: tea.KeyEnter})
	got, err := os.ReadFile(dest)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("saved bytes = %q, err=%v; want %q", got, err, payload)
	}
	_ = source.Close()
}

func TestPageExportRejectsIncompleteCells(t *testing.T) {
	m := &Model{browser: &browser.Browser{ActiveTable: "heavy", Data: &store.TabularData{
		Columns: []string{"payload"}, Rows: [][]string{{"prefix"}}, Affected: -1,
		Incomplete: map[store.CellPosition]store.IncompleteCell{{Row: 0, Column: 0}: {State: store.CellDeferred}},
	}}}
	_, _, err := m.exportSource()
	if err == nil || !strings.Contains(err.Error(), "deferred cells or previews") {
		t.Fatalf("exportSource error = %v, want explicit incomplete-page explanation", err)
	}
}

func TestQuitRemovesSpooledCellFile(t *testing.T) {
	file, err := os.CreateTemp("", "picklock-cell-*")
	if err != nil {
		t.Fatal(err)
	}
	name := file.Name()
	m := &Model{showDetail: true, detailCellFile: file, detailCellSize: 1}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if _, err := os.Stat(name); !os.IsNotExist(err) {
		t.Fatalf("spooled temp still exists after quit: stat err=%v", err)
	}
}
