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

// SettingsMsg is emitted when the user saves the preferences.
type SettingsMsg struct {
	QueryTimeoutSeconds int
}

// SettingsBackMsg is emitted when the user leaves the settings screen.
type SettingsBackMsg struct{}

// SettingsScreen edits the user preferences.
type SettingsScreen struct {
	input textinput.Model
	err   string
}

// NewSettingsScreen creates the settings form with the input focused.
func NewSettingsScreen() *SettingsScreen {
	in := textinput.New()
	in.Cursor.BlinkSpeed = CursorBlink
	in.Placeholder = "60"
	in.Prompt = " "
	in.Width = 24
	s := &SettingsScreen{input: in}
	s.Focus()
	return s
}

// Focus focuses the input.
func (s *SettingsScreen) Focus() tea.Cmd { return s.input.Focus() }

// Blur blurs the input.
func (s *SettingsScreen) Blur() { s.input.Blur() }

// SetValue replaces the timeout value shown in the form.
func (s *SettingsScreen) SetValue(v string) { s.input.SetValue(v) }

// SetError shows an error on the screen.
func (s *SettingsScreen) SetError(err string) { s.err = err }

// FocusOnField reports whether the input has focus (used by the q/? guards).
func (s *SettingsScreen) FocusOnField() bool { return s.input.Focused() }

// Update handles the keys of the settings screen.
func (s *SettingsScreen) Update(msg tea.Msg) (*SettingsScreen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return s, func() tea.Msg { return SettingsBackMsg{} }
		case "enter":
			v := strings.TrimSpace(s.input.Value())
			if v == "" {
				s.err = "type a number of seconds"
				return s, nil
			}
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 || n > prefs.MaxQueryTimeout {
				s.err = "use a number between 1 and 86400"
				return s, nil
			}
			return s, func() tea.Msg { return SettingsMsg{QueryTimeoutSeconds: n} }
		}
	}

	updated, cmd := s.input.Update(msg)
	s.input = updated
	return s, cmd
}

// View renders the settings form, centered like the connection screen.
func (s *SettingsScreen) View(width, height int) string {
	style := styles.StyleInputBox
	if s.input.Focused() {
		style = styles.StyleInputBoxFocus
	}
	contentView := s.input.View()
	if w := lipgloss.Width(contentView); w < boxInner {
		contentView += strings.Repeat(" ", boxInner-w)
	}
	box := style.Render(fmt.Sprintf("[ %s ]", contentView))

	var b strings.Builder
	b.WriteString(styles.StyleHeader.Render("Settings"))
	b.WriteString("\n\n")
	b.WriteString(fieldRow("query timeout (s)", box, width))
	b.WriteString("\n\n")
	b.WriteString(styles.StyleBtnPrimary.Render("Enter · Save"))
	b.WriteString("\n")
	b.WriteString(styles.StyleBtnSecondary.Render("esc  back"))
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
