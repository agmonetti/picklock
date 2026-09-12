// Package prefs stores the user-editable application preferences.
package prefs

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/agmonetti/picklock/internal/conn"
)

// QueryTimeoutDefault is the default query timeout in seconds.
const QueryTimeoutDefault = 60

// MaxQueryTimeout is the largest timeout accepted by the settings screen.
const MaxQueryTimeout = 86400

// Prefs holds the user-editable preferences.
type Prefs struct {
	// QueryTimeoutSeconds is the maximum time a query may run before being
	// cancelled. Values <= 0 fall back to the default.
	QueryTimeoutSeconds int `json:"query_timeout_seconds"`

	// SidebarWidth is the preferred workspace sidebar width in columns,
	// resized with a right-click drag. 0 means automatic.
	SidebarWidth int `json:"sidebar_width,omitempty"`

	// EditorHeight is the preferred SQL editor height in rows, resized with a
	// right-click drag. 0 means automatic.
	EditorHeight int `json:"editor_height,omitempty"`
}

// Default returns the built-in preferences.
func Default() Prefs {
	return Prefs{QueryTimeoutSeconds: QueryTimeoutDefault}
}

// QueryTimeout returns the effective query timeout in seconds.
func (p Prefs) QueryTimeout() int {
	if p.QueryTimeoutSeconds <= 0 {
		return QueryTimeoutDefault
	}
	return p.QueryTimeoutSeconds
}

// Load reads the preferences file. Returns Default() when it does not exist.
func Load() (Prefs, error) {
	p := Default()
	path, err := prefsPath()
	if err != nil {
		return p, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return p, nil
		}
		return p, err
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return p, err
	}
	// normalize invalid values back to the default
	p.QueryTimeoutSeconds = p.QueryTimeout()
	return p, nil
}

// Save writes the preferences with 0600 permissions.
func (p Prefs) Save() error {
	path, err := prefsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func prefsPath() (string, error) {
	dir, err := conn.ConfigDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, "picklock", "prefs.json")
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	legacy := filepath.Join(dir, "relm", "prefs.json")
	if _, err := os.Stat(legacy); err == nil {
		return legacy, nil
	}
	return p, nil
}
