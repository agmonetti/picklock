// Package prefs stores the user-editable application preferences.
package prefs

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/agmonetti/picklock/internal/conn"
	"github.com/agmonetti/picklock/internal/store"
)

// QueryTimeoutDefault is the default query timeout in seconds.
const QueryTimeoutDefault = 60

// MaxQueryTimeout is the largest accepted query timeout in seconds.
const MaxQueryTimeout = 86400

// Prefs holds the user-editable preferences.
type Prefs struct {
	// QueryTimeoutSeconds is the maximum time a query may run before cancellation.
	QueryTimeoutSeconds  int `json:"query_timeout_seconds"`
	BrowsePageSize       int `json:"browse_page_size"`
	PreviewBytes         int `json:"preview_bytes"`
	ValueBlockBytes      int `json:"value_block_bytes"`
	EditorMaxCellBytes   int `json:"editor_max_cell_bytes"`
	EditorMaxResultBytes int `json:"editor_max_result_bytes"`
	EditorMaxRows        int `json:"editor_max_rows"`

	// SidebarWidth is the preferred workspace sidebar width in columns,
	// resized with a right-click drag. 0 means automatic.
	SidebarWidth int `json:"sidebar_width,omitempty"`

	// EditorHeight is the preferred SQL editor height in rows, resized with a
	// right-click drag. 0 means automatic.
	EditorHeight int `json:"editor_height,omitempty"`
}

// Default returns the built-in preferences.
func Default() Prefs {
	limits := store.DefaultQueryLimits()
	return Prefs{
		QueryTimeoutSeconds:  QueryTimeoutDefault,
		BrowsePageSize:       50,
		PreviewBytes:         256,
		ValueBlockBytes:      64 << 10,
		EditorMaxCellBytes:   limits.MaxCellBytes,
		EditorMaxResultBytes: limits.MaxResultBytes,
		EditorMaxRows:        limits.MaxRows,
	}
}

// QueryTimeout returns the effective query timeout in seconds.
func (p Prefs) QueryTimeout() int {
	if p.QueryTimeoutSeconds <= 0 || p.QueryTimeoutSeconds > MaxQueryTimeout {
		return QueryTimeoutDefault
	}
	return p.QueryTimeoutSeconds
}

// QueryLimits returns the shared editor budget. Byte enforcement starts in E6.
func (p Prefs) QueryLimits() store.QueryLimits {
	return store.QueryLimits{
		MaxRows:        p.EditorMaxRows,
		MaxCellBytes:   p.EditorMaxCellBytes,
		MaxResultBytes: p.EditorMaxResultBytes,
	}
}

// Validate rejects unsafe limits before they can be applied or persisted.
func (p Prefs) Validate() error {
	if p.QueryTimeoutSeconds <= 0 || p.QueryTimeoutSeconds > MaxQueryTimeout {
		return fmt.Errorf("query timeout must be between 1 and %d seconds", MaxQueryTimeout)
	}
	if p.BrowsePageSize <= 0 || p.BrowsePageSize == math.MaxInt {
		return fmt.Errorf("browse page size must be positive and leave room for row lookahead")
	}
	if p.PreviewBytes <= 0 || p.ValueBlockBytes <= 0 {
		return fmt.Errorf("preview and value block bytes must be positive")
	}
	if p.PreviewBytes > p.ValueBlockBytes {
		return fmt.Errorf("preview bytes cannot exceed value block bytes")
	}
	if err := p.QueryLimits().Validate(); err != nil {
		return fmt.Errorf("editor limits: %w", err)
	}
	return nil
}

// Load merges older files with defaults. Any invalid file returns safe defaults
// and an error, because callers may intentionally continue after a load failure.
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
		return Default(), err
	}
	if err := p.Validate(); err != nil {
		return Default(), err
	}
	return p, nil
}

// Save writes the preferences with 0600 permissions.
func (p Prefs) Save() error {
	if err := p.Validate(); err != nil {
		return err
	}
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
