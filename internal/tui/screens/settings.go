package screens

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/agmonetti/picklock/internal/prefs"
	"github.com/agmonetti/picklock/internal/tui/styles"
)

// SettingsMsg is emitted when the user saves the complete preferences.
type SettingsMsg struct {
	Prefs prefs.Prefs
}

// SettingsBackMsg is emitted when the user leaves the settings screen.
type SettingsBackMsg struct{}

// SettingsScreen edits limits while preserving the workspace layout preferences.
type SettingsScreen struct {
	fields [7]field
	focus  int
	prefs  prefs.Prefs
	err    string
}

// NewSettingsScreen creates the settings form with the first input focused.
func NewSettingsScreen() *SettingsScreen {
	s := &SettingsScreen{}
	labels := [...]string{
		"query timeout (s)", "browse page rows", "preview bytes",
		"value block bytes", "editor cell (B)", "editor result (B)", "editor max rows",
	}
	for i, label := range labels {
		in := textinput.New()
		in.Cursor.BlinkSpeed = CursorBlink
		in.Prompt = " "
		in.Width = 24
		s.fields[i] = field{label: label, input: in}
	}
	s.SetPrefs(prefs.Default())
	return s
}

// settingsValues keeps the form order shared by filling and reading the inputs.
func settingsValues(p *prefs.Prefs) [7]*int {
	return [7]*int{
		&p.QueryTimeoutSeconds, &p.BrowsePageSize, &p.PreviewBytes,
		&p.ValueBlockBytes, &p.EditorMaxCellBytes, &p.EditorMaxResultBytes, &p.EditorMaxRows,
	}
}

// Focus focuses the selected field and blurs the others.
func (s *SettingsScreen) Focus() tea.Cmd {
	s.Blur()
	return s.fields[s.focus].input.Focus()
}

// Blur blurs all inputs.
func (s *SettingsScreen) Blur() {
	for i := range s.fields {
		s.fields[i].input.Blur()
	}
}

// SetPrefs replaces the limits shown in the form, retaining layout preferences.
func (s *SettingsScreen) SetPrefs(p prefs.Prefs) {
	s.prefs = p
	for i, value := range settingsValues(&p) {
		s.fields[i].input.SetValue(strconv.Itoa(*value))
	}
	s.focus = 0
	s.err = ""
	s.Focus()
}

// SetError shows an error on the screen.
func (s *SettingsScreen) SetError(err string) { s.err = err }

// FocusOnField reports whether an input has focus (used by the q/? guards).
func (s *SettingsScreen) FocusOnField() bool { return s.fields[s.focus].input.Focused() }

// Update handles navigation and saves only a fully validated set of preferences.
func (s *SettingsScreen) Update(msg tea.Msg) (*SettingsScreen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return s, func() tea.Msg { return SettingsBackMsg{} }
		case "tab", "down":
			s.focus = (s.focus + 1) % len(s.fields)
			return s, s.Focus()
		case "shift+tab", "up":
			s.focus = (s.focus + len(s.fields) - 1) % len(s.fields)
			return s, s.Focus()
		case "enter":
			p := s.prefs
			for i, value := range settingsValues(&p) {
				n, err := strconv.Atoi(strings.TrimSpace(s.fields[i].input.Value()))
				if err != nil {
					s.err = s.fields[i].label + ": enter a whole number within the supported integer range"
					s.focus = i
					s.Focus()
					return s, nil
				}
				*value = n
			}
			if err := p.Validate(); err != nil {
				s.err = err.Error()
				return s, nil
			}
			s.err = ""
			return s, func() tea.Msg { return SettingsMsg{Prefs: p} }
		}
	}

	updated, cmd := s.fields[s.focus].input.Update(msg)
	s.fields[s.focus].input = updated
	return s, cmd
}

// View renders the settings form, centered like the connection screen.
func (s *SettingsScreen) View(width, height int) string {
	var b strings.Builder
	b.WriteString(styles.StyleHeader.Render("Settings"))
	b.WriteString("\n\n")
	// Keep the focused field visible when a small terminal cannot fit all seven.
	visible := max(1, height-10)
	start := max(0, s.focus-visible+1)
	end := min(len(s.fields), start+visible)
	for i := start; i < end; i++ {
		style := styles.StyleInputBox
		if s.fields[i].input.Focused() {
			style = styles.StyleInputBoxFocus
		}
		content := s.fields[i].input.View()
		if w := lipgloss.Width(content); w < boxInner {
			content += strings.Repeat(" ", boxInner-w)
		}
		box := style.Render(fmt.Sprintf("[ %s ]", content))
		b.WriteString(fieldRow(s.fields[i].label, box, width))
		b.WriteString("\n")
	}
	b.WriteString("\nActive: page size, editor rows and query timeout.\n")
	b.WriteString("Byte limits pending E3/E5/E6; not memory protection.\n")
	b.WriteString(styles.StyleBtnPrimary.Render("Enter · Save"))
	b.WriteString("\n")
	b.WriteString(styles.StyleBtnSecondary.Render("Tab/↑↓ fields · esc back"))
	if s.err != "" {
		b.WriteString("\n\n" + styles.StyleError.Render(s.err))
	}

	content := lipgloss.NewStyle().Width(width).Align(lipgloss.Center).Render(b.String())
	if lipgloss.Height(content) > height {
		content = lipgloss.Place(width, height, lipgloss.Center, lipgloss.Top, content)
	} else {
		content = lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
	}

	lines := strings.Split(content, "\n")
	if len(lines) > height {
		content = strings.Join(lines[:height], "\n")
	}
	return content
}
