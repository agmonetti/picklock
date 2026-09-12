package tui

import (
	"context"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/agmonetti/picklock/internal/conn"
	"github.com/agmonetti/picklock/internal/store"
)

func newModelOpts(t *testing.T, opts NewOpts) *Model {
	t.Helper()
	m := New(opts)
	t.Cleanup(func() {
		if m.store != nil {
			m.store.Close()
		}
	})
	return m
}

func TestModel_InitialDSNConnects(t *testing.T) {
	cfg := conn.New(conn.DriverSQLite)
	cfg.Path = createTestDB(t)
	m := newModelOpts(t, NewOpts{InitialCfg: &cfg})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// Init runs the same way bubbletea does: it returns a cmd that must be
	// executed and its message fed back before the workspace appears.
	if cmd := m.Init(); cmd != nil {
		if out := cmd(); out != nil {
			step(t, m, out)
		}
	}
	if m.screen != ScreenWorkspace {
		t.Fatalf("screen = %d, want workspace after initial DSN", m.screen)
	}
	if m.browser == nil || len(m.browser.Tables) == 0 {
		t.Fatal("browser not loaded after initial DSN")
	}
}

func TestModel_InitialDSNErrorLandsOnForm(t *testing.T) {
	cfg := conn.New(conn.DriverSQLite)
	cfg.Path = filepath.Join(t.TempDir(), "missing.db")
	m := newModelOpts(t, NewOpts{InitialCfg: &cfg})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	if cmd := m.Init(); cmd != nil {
		if out := cmd(); out != nil {
			step(t, m, out)
		}
	}
	if m.screen != ScreenConnect {
		t.Fatalf("screen = %d, want connect after failed DSN", m.screen)
	}
	if m.connect.Error() == "" {
		t.Error("connection error not shown")
	}
}

func TestModel_GlobalReadOnlyForcesReadOnly(t *testing.T) {
	cfg := conn.New(conn.DriverSQLite)
	cfg.Path = createTestDB(t)
	m := newModelOpts(t, NewOpts{InitialCfg: &cfg, GlobalReadOnly: true})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	if cmd := m.Init(); cmd != nil {
		if out := cmd(); out != nil {
			step(t, m, out)
		}
	}
	if m.store == nil {
		t.Fatalf("store not open")
	}
	// the file itself is writable, but --read-only must open it mode=ro
	if _, err := m.store.Query().Execute(context.Background(), "CREATE TABLE blocked (id INT)", 0, 100); err == nil {
		t.Error("write must be blocked by global read-only")
	}
	if res, err := m.store.Query().Execute(context.Background(), "SELECT COUNT(*) FROM users", 0, 100); err != nil {
		t.Errorf("read in read-only mode failed: %v", err)
	} else if tab, ok := res.(*store.TabularData); !ok || len(tab.Rows) == 0 {
		t.Errorf("read in read-only mode returned unexpected: %v", res)
	}
}
