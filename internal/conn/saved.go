package conn

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// SavedConnection is a persisted connection for reuse.
type SavedConnection struct {
	Name     string `json:"name"`
	Driver   Driver `json:"driver"`
	Path     string `json:"path,omitempty"`
	ReadOnly bool   `json:"read_only,omitempty"`
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`
	Database string `json:"database,omitempty"`
	SSLMode  string `json:"ssl_mode,omitempty"`
}

// ToConfig converts a saved connection to a ConnectionConfig.
func (s SavedConnection) ToConfig() ConnectionConfig {
	cfg := ConnectionConfig{
		Driver:   s.Driver,
		Name:     s.Name,
		Path:     s.Path,
		ReadOnly: s.ReadOnly,
		Host:     s.Host,
		Port:     s.Port,
		User:     s.User,
		Password: s.Password,
		Database: s.Database,
		SSLMode:  s.SSLMode,
	}
	if cfg.Port == 0 {
		cfg.Port = DefaultPort(cfg.Driver)
	}
	return cfg
}

// FromConfig creates a SavedConnection from a config.
func FromConfig(cfg ConnectionConfig) SavedConnection {
	return SavedConnection{
		Name:     cfg.Name,
		Driver:   cfg.Driver,
		Path:     cfg.Path,
		ReadOnly: cfg.ReadOnly,
		Host:     cfg.Host,
		Port:     cfg.Port,
		User:     cfg.User,
		Password: cfg.Password,
		Database: cfg.Database,
		SSLMode:  cfg.SSLMode,
	}
}

// ConfigDir returns the picklock config directory. PICKLOCK_CONFIG_DIR overrides the
// OS config dir (also used by the tests). RELM_CONFIG_DIR is supported as fallback.
func ConfigDir() (string, error) {
	if dir := os.Getenv("PICKLOCK_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	if dir := os.Getenv("RELM_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	return os.UserConfigDir()
}

// savedPath returns the path of the saved connections file.
func savedPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, "picklock", "connections.json")
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	legacy := filepath.Join(dir, "relm", "connections.json")
	if _, err := os.Stat(legacy); err == nil {
		return legacy, nil
	}
	return p, nil
}

// LoadSaved reads the saved connections. Returns an empty list if it does not exist.
func LoadSaved() ([]SavedConnection, error) {
	path, err := savedPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var conns []SavedConnection
	if err := json.Unmarshal(data, &conns); err != nil {
		return nil, err
	}
	return conns, nil
}

// SaveSaved writes the saved connections with 0600 permissions.
func SaveSaved(conns []SavedConnection) error {
	path, err := savedPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(conns, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// SaveNamed adds or updates a connection in the list by name.
func SaveNamed(conns []SavedConnection, cfg ConnectionConfig) []SavedConnection {
	sc := FromConfig(cfg)
	for i := range conns {
		if conns[i].Name == sc.Name {
			conns[i] = sc
			return conns
		}
	}
	return append(conns, sc)
}
