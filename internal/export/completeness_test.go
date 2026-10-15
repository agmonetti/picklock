package export

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/agmonetti/picklock/internal/store"
)

func TestIncompleteExportsRejectBeforeWriting(t *testing.T) {
	preview := store.IncompleteCell{State: store.CellPreview, Type: "TEXT"}
	deferred := store.IncompleteCell{State: store.CellDeferred, Type: "BLOB"}
	cutoff := store.ResultStatus{Reason: store.LimitResultBytes, RetainedItems: 1, RetainedBytes: 3}
	cases := map[string]store.DataView{
		"tabular preview":   &store.TabularData{Columns: []string{"value"}, Rows: [][]string{{"pre"}}, Affected: -1, Incomplete: map[store.CellPosition]store.IncompleteCell{{Row: 0, Column: 0}: preview}},
		"tabular deferred":  &store.TabularData{Columns: []string{"value"}, Rows: [][]string{{""}}, Affected: -1, Incomplete: map[store.CellPosition]store.IncompleteCell{{Row: 0, Column: 0}: deferred}},
		"legacy row cutoff": &store.TabularData{Columns: []string{"value"}, Rows: [][]string{{"all"}}, Affected: -1, Truncated: true},
		"tabular cutoff":    &store.TabularData{Columns: []string{"value"}, Affected: -1, Status: cutoff},
		"document preview":  &store.DocumentData{Documents: []store.DocumentItem{{ID: "1", RawJSON: `{"prefix":`, Incomplete: &preview}}},
		"document deferred": &store.DocumentData{Documents: []store.DocumentItem{{ID: "1", Incomplete: &deferred}}},
		"document cutoff":   &store.DocumentData{Status: cutoff},
		"kv preview":        &store.KeyValueData{Key: "key", Entries: []store.KVEntry{{Value: "pre", Incomplete: &preview}}},
		"kv deferred":       &store.KeyValueData{Key: "key", Entries: []store.KVEntry{{Incomplete: &deferred}}},
		"kv cutoff":         &store.KeyValueData{Status: cutoff},
		"node preview":      &store.GraphData{Nodes: []store.GraphNode{{ID: "1", Properties: map[string]string{"body": "pre"}, Incomplete: map[string]store.IncompleteCell{"body": preview}}}},
		"edge deferred":     &store.GraphData{Edges: []store.GraphEdge{{ID: "1", Incomplete: map[string]store.IncompleteCell{"body": deferred}}}},
		"graph cutoff":      &store.GraphData{Status: cutoff},
		"text preview":      &store.RawTextData{Text: "pre", Incomplete: &preview},
		"text deferred":     &store.RawTextData{Incomplete: &deferred},
		"text cutoff":       &store.RawTextData{Status: cutoff},
	}
	writers := map[string]func(store.DataView, io.Writer) error{"CSV": WriteCSV, "JSON": WriteJSON}
	for name, data := range cases {
		for format, write := range writers {
			t.Run(name+"/"+format, func(t *testing.T) {
				var output bytes.Buffer
				err := write(data, &output)
				if err == nil || !strings.Contains(err.Error(), "incomplete") {
					t.Fatalf("expected incomplete-data explanation, got %v", err)
				}
				if output.Len() != 0 {
					t.Fatalf("rejected export wrote %d bytes", output.Len())
				}
			})
		}
	}
}

func TestNullOverridesIncompleteMetadataDuringExport(t *testing.T) {
	data := &store.TabularData{
		Columns: []string{"null", "empty", "literal"}, Rows: [][]string{{"ignored", "", "<deferred>"}},
		Nulls: [][]bool{{true, false, false}}, Affected: -1,
		Incomplete: map[store.CellPosition]store.IncompleteCell{{Row: 0, Column: 0}: {State: store.CellDeferred}},
	}
	var csv, jsonOutput bytes.Buffer
	if err := WriteCSV(data, &csv); err != nil {
		t.Fatal(err)
	}
	if got, want := csv.String(), "null,empty,literal\n,,<deferred>\n"; got != want {
		t.Fatalf("CSV = %q, want %q", got, want)
	}
	if err := WriteJSON(data, &jsonOutput); err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(jsonOutput.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if rows[0]["null"] != nil || rows[0]["empty"] != "" || rows[0]["literal"] != "<deferred>" {
		t.Fatalf("NULL/empty/literal contract lost: %#v", rows)
	}
	if data.Rows[0][0] != "ignored" {
		t.Fatal("export changed the source row")
	}
}

func TestCompleteJSONExportKeepsPublicShape(t *testing.T) {
	cases := map[string]struct {
		data store.DataView
		keys string
	}{
		"kv":    {&store.KeyValueData{Key: "key", Entries: []store.KVEntry{{Index: "0", Value: "complete"}}}, "Key,Type,TTL,Metadata,Entries"},
		"graph": {&store.GraphData{Nodes: []store.GraphNode{{ID: "1", Properties: map[string]string{"value": "complete"}}}}, "Nodes,Edges"},
		"raw":   {&store.RawTextData{Title: "title", Text: "complete"}, "Title,Text"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			if err := WriteJSON(tc.data, &output); err != nil {
				t.Fatal(err)
			}
			var object map[string]json.RawMessage
			if err := json.Unmarshal(output.Bytes(), &object); err != nil {
				t.Fatal(err)
			}
			keys := strings.Split(tc.keys, ",")
			if len(object) != len(keys) {
				t.Fatalf("export schema changed: %s", output.String())
			}
			for _, key := range keys {
				if _, ok := object[key]; !ok {
					t.Fatalf("missing %s: %s", key, output.String())
				}
			}
			for _, hidden := range []string{`"Incomplete"`, `"Status"`, `"RowRefs"`} {
				if strings.Contains(output.String(), hidden) {
					t.Fatalf("metadata leaked: %s", output.String())
				}
			}
		})
	}
}
