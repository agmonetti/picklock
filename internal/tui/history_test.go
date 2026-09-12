package tui

import (
	"strings"
	"testing"

	"github.com/agmonetti/picklock/internal/editor"
	"github.com/agmonetti/picklock/internal/tui/screens"
)

func TestModel_QueryHistoryPersistsAcrossSessions(t *testing.T) {
	t.Setenv("PICKLOCK_CONFIG_DIR", t.TempDir()) // isolated from the other tests' queries
	m := connect(t)
	pressAlt(t, m, "3")
	if m.focus != screens.FocusEditor {
		t.Fatalf("focus = %v, want editor", m.focus)
	}
	press(t, m, "SELECT 42")
	pressKey(t, m, "ctrl+r")

	// the query was written to history.json
	items := editor.LoadHistory()
	if len(items) != 1 || items[0] != "SELECT 42" {
		t.Fatalf("persisted history = %v, want [SELECT 42]", items)
	}

	// a fresh model (new session) preloads it
	m2 := newModel(t)
	got := m2.editor.History.Items()
	if len(got) != 1 || got[0] != "SELECT 42" {
		t.Fatalf("fresh model history = %v, want [SELECT 42]", got)
	}
}

func TestModel_QueryHistoryNavigableAfterReload(t *testing.T) {
	t.Setenv("PICKLOCK_CONFIG_DIR", t.TempDir())
	m := connect(t)
	pressAlt(t, m, "3")
	press(t, m, "SELECT 7")
	pressKey(t, m, "ctrl+r")

	// Ctrl+N returns to the connect screen and back: history must survive
	pressKey(t, m, "ctrl+n")
	if m.screen != ScreenConnect {
		t.Fatalf("screen = %d, want connect", m.screen)
	}
	m2 := newModel(t)
	if !strings.Contains(strings.Join(m2.editor.History.Items(), "\n"), "SELECT 7") {
		t.Errorf("history lost across session: %v", m2.editor.History.Items())
	}
}
