package prefs

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefault(t *testing.T) {
	want := Prefs{
		QueryTimeoutSeconds: 60, BrowsePageSize: 50, PreviewBytes: 256,
		ValueBlockBytes: 65536, EditorMaxCellBytes: 65536,
		EditorMaxResultBytes: 16777216, EditorMaxRows: 10000,
	}
	if got := Default(); got != want {
		t.Fatalf("Default() = %+v, want %+v", got, want)
	}
	if err := want.Validate(); err != nil {
		t.Fatalf("default preferences are invalid: %v", err)
	}
	p := Default()
	if p.QueryTimeout() != QueryTimeoutDefault {
		t.Errorf("QueryTimeout() = %d, want %d", p.QueryTimeout(), QueryTimeoutDefault)
	}
}

func TestQueryTimeoutNormalizesInvalid(t *testing.T) {
	for _, n := range []int{0, -5, MaxQueryTimeout + 1, math.MaxInt} {
		p := Prefs{QueryTimeoutSeconds: n}
		if p.QueryTimeout() != QueryTimeoutDefault {
			t.Errorf("QueryTimeout() = %d, want default for %d", p.QueryTimeout(), n)
		}
	}
}

func TestLoadMissingReturnsDefault(t *testing.T) {
	t.Setenv("PICKLOCK_CONFIG_DIR", t.TempDir())
	p, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p != Default() {
		t.Errorf("Load() = %+v, want defaults", p)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	t.Setenv("PICKLOCK_CONFIG_DIR", t.TempDir())

	p := Default()
	p.QueryTimeoutSeconds = 120
	p.BrowsePageSize = 25
	p.PreviewBytes = 512
	p.ValueBlockBytes = 32768
	p.EditorMaxCellBytes = 32768
	p.EditorMaxResultBytes = 1048576
	p.EditorMaxRows = 500
	p.SidebarWidth = 30
	p.EditorHeight = 18
	if err := p.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != p {
		t.Errorf("Load() = %+v, want %+v", got, p)
	}
	limits := got.QueryLimits()
	if limits.MaxRows != p.EditorMaxRows || limits.MaxCellBytes != p.EditorMaxCellBytes || limits.MaxResultBytes != p.EditorMaxResultBytes {
		t.Errorf("QueryLimits() = %+v, does not match %+v", limits, p)
	}
}

func TestDefaultLayoutIsAuto(t *testing.T) {
	p := Default()
	if p.SidebarWidth != 0 || p.EditorHeight != 0 {
		t.Errorf("default layout = %d/%d, want 0 (auto)", p.SidebarWidth, p.EditorHeight)
	}
}

func TestSaveWrites0600(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PICKLOCK_CONFIG_DIR", dir)

	p := Default()
	p.QueryTimeoutSeconds = 30
	if err := p.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "picklock", "prefs.json"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm = %o, want 0600", perm)
	}
}

func TestLoadOldFilesUsesDefaults(t *testing.T) {
	for _, directory := range []string{"picklock", "relm"} {
		t.Run(directory, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("PICKLOCK_CONFIG_DIR", dir)
			path := filepath.Join(dir, directory, "prefs.json")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(`{"query_timeout_seconds":90,"sidebar_width":31,"editor_height":12}`), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			want := Default()
			want.QueryTimeoutSeconds = 90
			want.SidebarWidth, want.EditorHeight = 31, 12
			if got != want {
				t.Errorf("Load old preferences = %+v, want %+v", got, want)
			}
		})
	}
}

func TestValidateRejectsInvalidLimits(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Prefs)
	}{
		{"timeout zero", func(p *Prefs) { p.QueryTimeoutSeconds = 0 }},
		{"timeout too large", func(p *Prefs) { p.QueryTimeoutSeconds = MaxQueryTimeout + 1 }},
		{"page zero", func(p *Prefs) { p.BrowsePageSize = 0 }},
		{"page overflow", func(p *Prefs) { p.BrowsePageSize = math.MaxInt }},
		{"preview zero", func(p *Prefs) { p.PreviewBytes = 0 }},
		{"block zero", func(p *Prefs) { p.ValueBlockBytes = 0 }},
		{"preview exceeds block", func(p *Prefs) { p.PreviewBytes = p.ValueBlockBytes + 1 }},
		{"cell zero", func(p *Prefs) { p.EditorMaxCellBytes = 0 }},
		{"result zero", func(p *Prefs) { p.EditorMaxResultBytes = 0 }},
		{"cell exceeds result", func(p *Prefs) { p.EditorMaxCellBytes = p.EditorMaxResultBytes + 1 }},
		{"rows zero", func(p *Prefs) { p.EditorMaxRows = 0 }},
		{"rows overflow", func(p *Prefs) { p.EditorMaxRows = math.MaxInt }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Default()
			tt.change(&p)
			if err := p.Validate(); err == nil {
				t.Fatal("Validate accepted invalid preferences")
			}
			// Save must reject invalid preferences before creating a file.
			dir := t.TempDir()
			t.Setenv("PICKLOCK_CONFIG_DIR", dir)
			if err := p.Save(); err == nil {
				t.Fatal("Save accepted invalid preferences")
			}
			if _, err := os.Stat(filepath.Join(dir, "picklock", "prefs.json")); !os.IsNotExist(err) {
				t.Fatalf("invalid Save created a file: %v", err)
			}
		})
	}
	p := Default()
	p.PreviewBytes = p.ValueBlockBytes
	p.EditorMaxCellBytes = p.EditorMaxResultBytes
	if err := p.Validate(); err != nil {
		t.Errorf("equal related limits rejected: %v", err)
	}
}

func TestLoadInvalidReturnsSafeDefaults(t *testing.T) {
	for _, data := range []string{
		`{"query_timeout_seconds":90,"preview_bytes":0}`,
		`{"query_timeout_seconds":90,"browse_page_size":-1}`,
		`{"query_timeout_seconds":90,"value_block_bytes":128}`,
		`{"query_timeout_seconds":90,"editor_max_result_bytes":1}`,
		`{"query_timeout_seconds":90,"editor_max_rows":-1}`,
		`{"query_timeout_seconds":90,"preview_bytes":999999999999999999999999999}`,
		`{"query_timeout_seconds":90,"preview_bytes":"invalid"}`,
		`{"query_timeout_seconds":90,`,
	} {
		t.Run(data, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("PICKLOCK_CONFIG_DIR", dir)
			path := filepath.Join(dir, "picklock", "prefs.json")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := Load()
			if err == nil {
				t.Fatal("Load accepted invalid file")
			}
			if got != Default() {
				t.Errorf("Load returned unsafe/partial preferences: %+v", got)
			}
		})
	}
}

func TestInvalidSavePreservesExistingFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PICKLOCK_CONFIG_DIR", dir)
	p := Default()
	if err := p.Save(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "picklock", "prefs.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	p.EditorMaxRows = -1
	if err := p.Save(); err == nil {
		t.Fatal("invalid Save succeeded")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("invalid Save changed existing preferences")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(after, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"query_timeout_seconds", "browse_page_size", "preview_bytes", "value_block_bytes", "editor_max_cell_bytes", "editor_max_result_bytes", "editor_max_rows"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("missing persisted key %s", key)
		}
	}
	if strings.Contains(string(after), "filter") || strings.Contains(string(after), "column") {
		t.Fatal("session-only selection was persisted")
	}
}
