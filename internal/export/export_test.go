package export

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/agmonetti/picklock/internal/store"
)

func res(cols []string, rows [][]string, nulls [][]bool) *store.TabularData {
	return &store.TabularData{Columns: cols, Rows: rows, Nulls: nulls, Affected: -1}
}

func TestWriteCSV_Basic(t *testing.T) {
	r := res(
		[]string{"id", "name"},
		[][]string{{"1", "Alice"}, {"2", "Bob"}},
		nil,
	)
	var b bytes.Buffer
	if err := WriteCSV(r, &b); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}
	want := "id,name\n1,Alice\n2,Bob\n"
	if b.String() != want {
		t.Errorf("CSV = %q, want %q", b.String(), want)
	}
}

func TestWriteCSV_Quoting(t *testing.T) {
	r := res(
		[]string{"a", "b", "c"},
		[][]string{{"with, comma", `with "quotes"`, "line\nbreak"}},
		nil,
	)
	var b bytes.Buffer
	if err := WriteCSV(r, &b); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}
	if !strings.HasPrefix(b.String(), "a,b,c\n") {
		t.Errorf("header = %q", b.String())
	}
	body := strings.TrimPrefix(b.String(), "a,b,c\n")
	for _, want := range []string{`"with, comma"`, `"with ""quotes"""`, `"line`} {
		if !strings.Contains(body, want) {
			t.Errorf("row %q does not contain %q", body, want)
		}
	}
}

func TestWriteCSV_NullIsEmpty(t *testing.T) {
	r := res(
		[]string{"a", "b"},
		[][]string{{"x", ""}},
		[][]bool{{false, true}},
	)
	var b bytes.Buffer
	if err := WriteCSV(r, &b); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}
	if b.String() != "a,b\nx,\n" {
		t.Errorf("CSV = %q", b.String())
	}
}

func TestWriteJSON_NullAndEmptyString(t *testing.T) {
	r := res(
		[]string{"a", "b", "c"},
		[][]string{{"1", "", "text"}},
		[][]bool{{false, true, false}},
	)
	var b bytes.Buffer
	if err := WriteJSON(r, &b); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v (%q)", err, b.String())
	}
	if len(got) != 1 {
		t.Fatalf("rows = %d, want 1", len(got))
	}
	row := got[0]
	if row["a"] != "1" {
		t.Errorf("a = %#v, want string \"1\"", row["a"])
	}
	if v, ok := row["b"]; !ok || v != nil {
		t.Errorf("b = %#v, want JSON null", row["b"])
	}
	if row["c"] != "text" {
		t.Errorf("c = %#v, want \"text\"", row["c"])
	}
}

func TestWriteJSON_KeepsColumnOrder(t *testing.T) {
	r := res(
		[]string{"zeta", "alpha", "middle"},
		[][]string{{"1", "2", "3"}},
		nil,
	)
	var b bytes.Buffer
	if err := WriteJSON(r, &b); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 1 || got[0]["zeta"] != "1" || got[0]["alpha"] != "2" || got[0]["middle"] != "3" {
		t.Errorf("JSON = %s", b.String())
	}
}

func TestWriteDocumentData(t *testing.T) {
	docData := &store.DocumentData{
		Documents: []store.DocumentItem{
			{ID: "1", Summary: `name: "Alice"`, RawJSON: `{"_id":"1","name":"Alice"}`},
		},
	}
	var b bytes.Buffer
	if err := WriteJSON(docData, &b); err != nil {
		t.Fatalf("WriteJSON(docData): %v", err)
	}
	if !strings.Contains(b.String(), `"name": "Alice"`) {
		t.Errorf("doc JSON = %s", b.String())
	}
}

func TestWriteKeyValueData(t *testing.T) {
	kvData := &store.KeyValueData{
		Key:  "user:1",
		Type: "hash",
		TTL:  "800s",
		Entries: []store.KVEntry{
			{Index: "name", Value: "Alice"},
		},
	}
	var b bytes.Buffer
	if err := WriteCSV(kvData, &b); err != nil {
		t.Fatalf("WriteCSV(kvData): %v", err)
	}
	if !strings.Contains(b.String(), "name,Alice") {
		t.Errorf("kv CSV = %s", b.String())
	}
}
