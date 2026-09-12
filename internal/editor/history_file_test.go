package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// withTempConfigDir points the config dir at a temp folder so the real user's
// history is never touched by a test.
func withTempConfigDir(t *testing.T) {
	t.Helper()
	t.Setenv("PICKLOCK_CONFIG_DIR", t.TempDir())
}

func historyFilePath() string {
	return filepath.Join(os.Getenv("PICKLOCK_CONFIG_DIR"), "picklock", "history.json")
}

func TestHistoryFile_RoundTrip(t *testing.T) {
	withTempConfigDir(t)
	if got := LoadHistory(); got != nil {
		t.Fatalf("LoadHistory on empty config = %v, want nil", got)
	}

	h := NewHistory()
	for _, q := range []string{"SELECT 1", "SELECT * FROM users", "SELECT 1"} {
		h.Push(q) // never consecutive duplicates
	}
	if err := SaveHistory(h.Items()); err != nil {
		t.Fatalf("SaveHistory: %v", err)
	}

	got := LoadHistory()
	if len(got) != 3 || got[0] != "SELECT 1" || got[1] != "SELECT * FROM users" || got[2] != "SELECT 1" {
		t.Errorf("LoadHistory = %v, want the three queries in order", got)
	}

	fi, err := os.Stat(historyFilePath())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("history perms = %o, want 600", perm)
	}
}

func TestHistoryFile_CapMatchesRing(t *testing.T) {
	withTempConfigDir(t)
	h := NewHistory()
	for i := 0; i < h.Max()+20; i++ {
		h.Push(fmt.Sprintf("q%d", i))
	}
	h.Push("final")
	if h.Len() != h.Max() {
		t.Fatalf("history len = %d, want %d", h.Len(), h.Max())
	}
	if err := SaveHistory(h.Items()); err != nil {
		t.Fatalf("SaveHistory: %v", err)
	}
	if got := LoadHistory(); len(got) != h.Max() {
		t.Errorf("loaded = %d queries, want %d", len(got), h.Max())
	}
}

func TestHistoryFile_CorruptFileStartsEmpty(t *testing.T) {
	withTempConfigDir(t)
	if err := os.MkdirAll(filepath.Dir(historyFilePath()), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(historyFilePath(), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := LoadHistory(); got != nil {
		t.Errorf("LoadHistory on corrupt file = %v, want nil", got)
	}
}
