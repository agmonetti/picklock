package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agmonetti/picklock/internal/store"
	"github.com/agmonetti/picklock/internal/tui/screens"
)

func TestExport_NothingToExportFromEditor(t *testing.T) {
	m := connect(t)
	pressAlt(t, m, "3") // editor, no query run yet
	pressAlt(t, m, "e") // export

	if m.exporting {
		t.Fatal("prompt should not open with nothing to export")
	}
	if m.err == "" || !strings.Contains(m.err, "nothing to export") {
		t.Errorf("err = %q, want a 'nothing to export' notice", m.err)
	}
}

func TestExport_EscCancels(t *testing.T) {
	m := connect(t)
	pressAlt(t, m, "3")
	press(t, m, "SELECT 1")
	pressKey(t, m, "ctrl+r")

	pressAlt(t, m, "e")
	if !m.exporting || m.focus != screens.FocusEditor {
		t.Fatalf("setup: exporting=%v focus=%v", m.exporting, m.focus)
	}
	pressKey(t, m, "esc")
	if m.exporting {
		t.Fatal("esc should close the export prompt")
	}
}

func TestExport_EditorResultToCSV(t *testing.T) {
	m := connect(t)
	pressAlt(t, m, "3")
	press(t, m, "SELECT NULL AS a, 'x' AS b")
	pressKey(t, m, "ctrl+r")
	tab, ok := m.editor.Data.(*store.TabularData)
	if !ok || len(tab.Columns) != 2 {
		t.Fatalf("setup: query did not produce a table: %+v", m.editor)
	}

	pressAlt(t, m, "e")
	if !m.exporting {
		t.Fatal("export prompt did not open")
	}
	out := filepath.Join(t.TempDir(), "out.csv")
	m.exportInput.SetValue(out)
	pressKey(t, m, "enter")

	if m.exporting {
		t.Fatal("prompt should close after a successful export")
	}
	if m.exportErr != "" {
		t.Fatalf("exportErr = %q", m.exportErr)
	}
	if m.exported == "" || !strings.Contains(m.exported, "1 row") || !strings.Contains(m.exported, out) {
		t.Errorf("exported message = %q, want '1 row' and the path", m.exported)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if want := "a,b\n,x\n"; string(got) != want {
		t.Errorf("CSV content = %q, want %q", got, want)
	}
}

func TestExport_EditorResultToJSONNulls(t *testing.T) {
	m := connect(t)
	pressAlt(t, m, "3")
	press(t, m, "SELECT NULL AS a, 'x' AS b")
	pressKey(t, m, "ctrl+r")

	pressAlt(t, m, "e")
	out := filepath.Join(t.TempDir(), "out.json")
	m.exportInput.SetValue(out)
	pressKey(t, m, "enter")

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(got, &rows); err != nil {
		t.Fatalf("unmarshal JSON: %v", err)
	}
	if len(rows) != 1 || rows[0]["a"] != nil || rows[0]["b"] != "x" {
		t.Errorf("JSON content = %s, want [{\"a\":null,\"b\":\"x\"}]", string(got))
	}
}

func TestExport_BrowserPageToCSV(t *testing.T) {
	m := connect(t)
	press(t, m, "2") // open users (id 1 Alice, 2 Bob)
	if m.browser.ActiveTable != "users" {
		t.Fatalf("setup: ActiveTable = %q, want users", m.browser.ActiveTable)
	}

	pressAlt(t, m, "e")
	if !m.exporting {
		t.Fatal("export prompt did not open")
	}
	out := filepath.Join(t.TempDir(), "table.csv")
	m.exportInput.SetValue(out)
	pressKey(t, m, "enter")

	if m.exported == "" || !strings.Contains(m.exported, "2 row") {
		t.Fatalf("exported message = %q, want 2 rows", m.exported)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := "id,name,email\n1,Alice,a@t.com\n2,Bob,b@t.com\n"
	if string(got) != want {
		t.Errorf("CSV content = %q, want %q", got, want)
	}
}

func TestExport_CreatesParentDirectories(t *testing.T) {
	m := connect(t)
	pressAlt(t, m, "3")
	press(t, m, "SELECT 1")
	pressKey(t, m, "ctrl+r")

	pressAlt(t, m, "e")
	out := filepath.Join(t.TempDir(), "nested", "sub", "dir", "out.csv")
	m.exportInput.SetValue(out)
	pressKey(t, m, "enter")

	if m.exporting {
		t.Fatal("prompt should close after successful export with directory creation")
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}
}

func TestExport_WriteErrorKeepsPromptOpen(t *testing.T) {
	m := connect(t)
	pressAlt(t, m, "3")
	press(t, m, "SELECT 1")
	pressKey(t, m, "ctrl+r")

	pressAlt(t, m, "e")
	// /dev/null/cannot-be-a-dir/out.csv will fail to create directory
	m.exportInput.SetValue("/dev/null/cannot-be-a-dir/out.csv")
	pressKey(t, m, "enter")

	if !m.exporting {
		t.Fatal("prompt must stay open after a write error")
	}
	if m.exportErr == "" {
		t.Error("expected a visible write error")
	}
}
