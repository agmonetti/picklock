package editor

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/agmonetti/picklock/internal/conn"
)

// historyPath returns the path of the persistent query history file.
func historyPath() (string, error) {
	dir, err := conn.ConfigDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, "picklock", "history.json")
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	legacy := filepath.Join(dir, "relm", "history.json")
	if _, err := os.Stat(legacy); err == nil {
		return legacy, nil
	}
	return p, nil
}

// LoadHistory reads the queries persisted across sessions. It returns nil when
// the file does not exist or cannot be parsed: a corrupt or missing history
// must never break the editor, it just starts empty.
func LoadHistory() []string {
	path, err := historyPath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var items []string
	if err := json.Unmarshal(data, &items); err != nil {
		return nil
	}
	return items
}

// SaveHistory writes the queries to disk with 0600 permissions. The calling
// side is responsible for the ring cap and deduplication (History.Push already
// applies them to the passed items).
func SaveHistory(items []string) error {
	path, err := historyPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
