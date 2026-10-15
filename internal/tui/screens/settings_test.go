package screens

import (
	"math"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/agmonetti/picklock/internal/prefs"
)

func TestSettingsScreen_ViewRenders(t *testing.T) {
	s := NewSettingsScreen()
	out := s.View(80, 24)
	for _, label := range []string{"Settings", "query timeout", "browse page rows", "preview bytes", "value block bytes", "editor cell", "editor result", "editor max rows", "pending E3/E5/E6", "not memory protection"} {
		if !strings.Contains(out, label) {
			t.Errorf("settings view missing %q: %q", label, out)
		}
	}
}

func TestSettingsScreen_EscapeEmitsBack(t *testing.T) {
	s := NewSettingsScreen()
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected a SettingsBackMsg cmd on Esc")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("missing back message")
	} else if _, ok := msg.(SettingsBackMsg); !ok {
		t.Errorf("cmd() = %#v, want SettingsBackMsg", msg)
	}
}

func TestSettingsScreen_EnterEmitsSettingsMsg(t *testing.T) {
	s := NewSettingsScreen()
	p := prefs.Default()
	p.QueryTimeoutSeconds = 90
	p.BrowsePageSize = 25
	p.PreviewBytes = 128
	p.ValueBlockBytes = 32768
	p.EditorMaxCellBytes = 1024
	p.EditorMaxResultBytes = 1048576
	p.EditorMaxRows = 500
	p.SidebarWidth, p.EditorHeight = 31, 12
	s.SetPrefs(p)
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a SettingsMsg cmd on Enter")
	}
	msg := cmd()
	sm, ok := msg.(SettingsMsg)
	if !ok {
		t.Fatalf("cmd() = %#v, want SettingsMsg", msg)
	}
	if sm.Prefs != p {
		t.Errorf("saved preferences = %+v, want %+v", sm.Prefs, p)
	}
}

func TestSettingsScreen_RejectsInvalid(t *testing.T) {
	for i := range 7 {
		for _, v := range []string{"", "abc", "0", "-3", "999999999999999999999999999"} {
			t.Run(strconv.Itoa(i)+"/"+v, func(t *testing.T) {
				s := NewSettingsScreen()
				s.fields[i].input.SetValue(v)
				_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
				if cmd != nil {
					t.Fatal("invalid value emitted a save message")
				}
				if s.err == "" {
					t.Fatal("invalid value did not show an error")
				}
				if !s.FocusOnField() {
					t.Fatal("invalid value lost form focus")
				}
			})
		}
	}
}

func TestSettingsScreen_RejectsRelationshipsAndArithmeticOverflow(t *testing.T) {
	for _, tt := range []struct {
		field int
		value int
	}{
		{0, prefs.MaxQueryTimeout + 1},
		{1, math.MaxInt},
		{2, 65537},
		{3, 255},
		{4, 16777217},
		{5, 65535},
		{6, math.MaxInt},
	} {
		s := NewSettingsScreen()
		s.fields[tt.field].input.SetValue(strconv.Itoa(tt.value))
		_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if cmd != nil || s.err == "" {
			t.Errorf("field %d=%d: expected error without save", tt.field, tt.value)
		}
		// Correcting the value allows saving without reopening the form.
		s.SetPrefs(prefs.Default())
		_, cmd = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if cmd == nil || s.err != "" {
			t.Errorf("field %d: corrected preferences failed to save", tt.field)
		}
	}
}

func TestSettingsScreen_NavigationAndEditingEveryField(t *testing.T) {
	s := NewSettingsScreen()
	for i, value := range []string{"90", "25", "128", "32768", "1024", "1048576", "500"} {
		if s.focus != i || !s.fields[i].input.Focused() {
			t.Fatalf("field %d not focused", i)
		}
		s.fields[i].input.SetValue("")
		s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)})
		s.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	if s.focus != 0 {
		t.Fatalf("Tab should wrap, focus = %d", s.focus)
	}
	s.Update(tea.KeyMsg{Type: tea.KeyUp})
	if s.focus != 6 {
		t.Fatalf("Up should wrap, focus = %d", s.focus)
	}
	if out := s.View(80, 13); !strings.Contains(out, "editor max rows") {
		t.Errorf("small view hides the focused field: %q", out)
	}
	s.Update(tea.KeyMsg{Type: tea.KeyDown})
	s.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if s.focus != 6 {
		t.Fatalf("Shift+Tab should wrap, focus = %d", s.focus)
	}
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("save failed: %s", s.err)
	}
	got := cmd().(SettingsMsg).Prefs
	want := prefs.Default()
	want.QueryTimeoutSeconds, want.BrowsePageSize, want.PreviewBytes = 90, 25, 128
	want.ValueBlockBytes, want.EditorMaxCellBytes = 32768, 1024
	want.EditorMaxResultBytes, want.EditorMaxRows = 1048576, 500
	if got != want {
		t.Errorf("typed preferences = %+v, want %+v", got, want)
	}
}

func TestSettingsScreen_FocusOnField(t *testing.T) {
	s := NewSettingsScreen()
	if !s.FocusOnField() {
		t.Error("input should be focused by default")
	}
	s.Blur()
	if s.FocusOnField() {
		t.Error("input should be blurred after Blur")
	}
	s.Focus()
	if !s.FocusOnField() {
		t.Error("Focus should restore selected input")
	}
}
