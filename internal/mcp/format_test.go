package mcp

import (
	"strings"
	"testing"

	"github.com/agmonetti/picklock/internal/store"
)

func TestFormatIncompleteValuesAcrossFormats(t *testing.T) {
	for _, state := range []store.CellState{store.CellPreview, store.CellDeferred} {
		cell := store.IncompleteCell{State: state, Type: "TEXT"}
		cases := map[string]store.DataView{
			"table":            &store.TabularData{Columns: []string{"body"}, Rows: [][]string{{"retained-prefix"}}, Affected: -1, Incomplete: map[store.CellPosition]store.IncompleteCell{{Row: 0, Column: 0}: cell}},
			"document":         &store.DocumentData{Documents: []store.DocumentItem{{ID: "1", RawJSON: "retained-prefix", Incomplete: &cell}}},
			"string":           &store.KeyValueData{Key: "key", Type: "string", Entries: []store.KVEntry{{Value: "retained-prefix", Incomplete: &cell}}},
			"hash":             &store.KeyValueData{Key: "key", Type: "hash", Entries: []store.KVEntry{{Index: "field", Value: "retained-prefix", Incomplete: &cell}}},
			"node":             &store.GraphData{Nodes: []store.GraphNode{{ID: "1", Properties: map[string]string{"body": "retained-prefix"}, Incomplete: map[string]store.IncompleteCell{"body": cell}}}},
			"missing property": &store.GraphData{Nodes: []store.GraphNode{{ID: "1", Incomplete: map[string]store.IncompleteCell{"body": cell}}}},
			"text":             &store.RawTextData{Text: "retained-prefix", Incomplete: &cell},
		}
		for name, data := range cases {
			t.Run(string(state)+"/"+name, func(t *testing.T) {
				got := formatDataView(data, false, -1, 0)
				if !strings.Contains(got, "INCOMPLETE") || !strings.Contains(got, "<"+string(state)) {
					t.Fatalf("missing explicit incomplete state: %q", got)
				}
				if name != "missing property" && strings.Contains(got, "retained-prefix") != (state == store.CellPreview) {
					t.Fatalf("preview/deferred payload contract lost: %q", got)
				}
			})
		}
	}
}

func TestFormatGraphEdgeIncompletenessIsNotHidden(t *testing.T) {
	graph := &store.GraphData{Edges: []store.GraphEdge{{
		ID: "1", Incomplete: map[string]store.IncompleteCell{"body": {State: store.CellDeferred}},
	}}}
	got := formatDataView(graph, false, -1, 0)
	if !strings.Contains(got, "INCOMPLETE") {
		t.Fatalf("incomplete edge metadata hidden: %q", got)
	}
}

func TestFormatNullEmptyAndLiteralMarkers(t *testing.T) {
	table := &store.TabularData{
		Columns: []string{"null", "empty", "literal"}, Rows: [][]string{{"ignored", "", "<deferred>"}},
		Nulls: [][]bool{{true, false, false}}, Affected: -1,
		Incomplete: map[store.CellPosition]store.IncompleteCell{{Row: 0, Column: 0}: {State: store.CellDeferred}},
	}
	got := formatDataView(table, false, -1, 0)
	if !strings.Contains(got, "NULL |  | <deferred>") || strings.Contains(got, "INCOMPLETE") || strings.Contains(got, "ignored") {
		t.Fatalf("NULL/empty/literal formatting changed: %q", got)
	}
}

func TestFormatEmptyCutoffsRemainExplicit(t *testing.T) {
	status := store.ResultStatus{Reason: store.LimitResultBytes, RetainedItems: 0, RetainedBytes: 0}
	cases := []store.DataView{
		&store.TabularData{Affected: -1, Status: status}, &store.DocumentData{Status: status},
		&store.KeyValueData{Status: status}, &store.GraphData{Status: status}, &store.RawTextData{Status: status},
	}
	for _, data := range cases {
		got := formatDataView(data, false, -1, 0)
		if !strings.Contains(got, "INCOMPLETE") || !strings.Contains(got, string(store.LimitResultBytes)) || !strings.Contains(got, "retained 0 items") {
			t.Errorf("%T hides empty cutoff: %q", data, got)
		}
	}
	legacy := formatDataView(&store.TabularData{Affected: -1, Truncated: true}, false, -1, 0)
	if !strings.Contains(legacy, "INCOMPLETE") || strings.Contains(legacy, "content bytes") {
		t.Fatalf("legacy cutoff claims unmeasured bytes: %q", legacy)
	}
}
