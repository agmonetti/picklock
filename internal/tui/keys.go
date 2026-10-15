package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap groups all the keybindings of the application.
type KeyMap struct {
	Quit           key.Binding
	NewSession     key.Binding
	SaveConn       key.Binding
	Settings       key.Binding
	ToggleSidebar  key.Binding
	ToggleMain     key.Binding
	ToggleEditor   key.Binding
	ZoomPane       key.Binding
	Help           key.Binding
	Switch         key.Binding
	Inspect        key.Binding
	Detail         key.Binding
	BrowseOptions  key.Binding
	Refresh        key.Binding
	Execute        key.Binding
	ClearInput     key.Binding
	LineStart      key.Binding
	LineEnd        key.Binding
	Back           key.Binding
	Export         key.Binding
	CopyQuery      key.Binding
	ScrollColLeft  key.Binding
	ScrollColRight key.Binding

	FocusSidebar key.Binding
	FocusMain    key.Binding
	FocusEditor  key.Binding

	Up       key.Binding
	Down     key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	First    key.Binding
	Last     key.Binding
}

// DefaultKeyMap returns the full keymap.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Quit:           key.NewBinding(key.WithKeys("ctrl+c", "q"), key.WithHelp("ctrl+c / q", "quit")),
		NewSession:     key.NewBinding(key.WithKeys("ctrl+n"), key.WithHelp("ctrl+n", "new connection")),
		SaveConn:       key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save connection")),
		Settings:       key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "settings")),
		ToggleSidebar:  key.NewBinding(key.WithKeys("alt+b"), key.WithHelp("alt+b", "sidebar")),
		ToggleMain:     key.NewBinding(key.WithKeys("alt+m"), key.WithHelp("alt+m", "main view")),
		ToggleEditor:   key.NewBinding(key.WithKeys("alt+q"), key.WithHelp("alt+q", "editor")),
		ZoomPane:       key.NewBinding(key.WithKeys("alt+z"), key.WithHelp("alt+z", "zoom pane")),
		Help:           key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Switch:         key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next pane")),
		Inspect:        key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "structure")),
		Detail:         key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "row detail")),
		BrowseOptions:  key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "browse options")),
		Refresh:        key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Execute:        key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "run")),
		ClearInput:     key.NewBinding(key.WithKeys("ctrl+l"), key.WithHelp("ctrl+l", "clear")),
		LineStart:      key.NewBinding(key.WithKeys("ctrl+a"), key.WithHelp("ctrl+a", "line start")),
		LineEnd:        key.NewBinding(key.WithKeys("ctrl+e"), key.WithHelp("ctrl+e", "line end")),
		Back:           key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Export:         key.NewBinding(key.WithKeys("alt+e"), key.WithHelp("alt+e", "export result")),
		CopyQuery:      key.NewBinding(key.WithKeys("alt+c", "ctrl+y"), key.WithHelp("alt+c", "copy query")),
		ScrollColLeft:  key.NewBinding(key.WithKeys("alt+left", "alt+h", "shift+left"), key.WithHelp("alt+←", "scroll col left")),
		ScrollColRight: key.NewBinding(key.WithKeys("alt+right", "alt+l", "shift+right"), key.WithHelp("alt+→", "scroll col right")),

		// Alt+1..3 works on every terminal; Ctrl+1..3 is kept for terminals
		// with CSI-u support (kitty, wezterm, ...).
		FocusSidebar: key.NewBinding(key.WithKeys("alt+1", "ctrl+1"), key.WithHelp("alt+1", "sidebar")),
		FocusMain:    key.NewBinding(key.WithKeys("alt+2", "ctrl+2"), key.WithHelp("alt+2", "main")),
		FocusEditor:  key.NewBinding(key.WithKeys("alt+3", "ctrl+3"), key.WithHelp("alt+3", "editor")),

		Up:       key.NewBinding(key.WithKeys("up", "k")),
		Down:     key.NewBinding(key.WithKeys("down", "j")),
		PageUp:   key.NewBinding(key.WithKeys("pgup", "ctrl+u")),
		PageDown: key.NewBinding(key.WithKeys("pgdown", "ctrl+d")),
		First:    key.NewBinding(key.WithKeys("g", "home")),
		Last:     key.NewBinding(key.WithKeys("G", "end")),
	}
}

// ShortHelp implements help.KeyMap.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.Quit, k.NewSession, k.Switch, k.Inspect, k.Help,
	}
}

// FullHelp implements help.KeyMap.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Quit, k.NewSession, k.Switch, k.Settings, k.FocusSidebar, k.FocusMain, k.FocusEditor},
		{k.ToggleSidebar, k.ToggleMain, k.ToggleEditor, k.ZoomPane, k.Help, k.Back, k.Inspect, k.Detail, k.BrowseOptions, k.Refresh, k.Execute, k.ClearInput, k.Export, k.CopyQuery, k.ScrollColLeft, k.ScrollColRight},
	}
}
