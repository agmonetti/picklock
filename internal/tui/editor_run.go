package tui

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/agmonetti/picklock/internal/editor"
	"github.com/agmonetti/picklock/internal/tui/screens"
)

// handleEditorKeys handles keys when the editor has the focus.
func (m *Model) handleEditorKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.setFocus(screens.FocusMain)
		return m, nil
	case key.Matches(msg, m.keys.Execute):
		if m.loading {
			return m, nil
		}
		if strings.TrimSpace(m.editorScreen.Value()) == "" {
			m.editor.Data = nil
			m.editor.Error = "write a query first"
			return m, nil
		}
		return m, m.executeEditor()
	case key.Matches(msg, m.keys.ClearInput):
		m.editor.Clear()
		m.editorScreen.SetValue("")
		return m, nil
	case key.Matches(msg, m.keys.CopyQuery):
		return m, m.copyQueryToClipboard()
	case key.Matches(msg, m.keys.ScrollColLeft) || (msg.Type == tea.KeyLeft && msg.Alt):
		m.scrollEditorColLeft()
		return m, nil
	case key.Matches(msg, m.keys.ScrollColRight) || (msg.Type == tea.KeyRight && msg.Alt):
		m.scrollEditorColRight()
		return m, nil
	case key.Matches(msg, m.keys.Up) && !m.loading && m.editorScreen.AtBoundary(true) &&
		(m.editorScreen.Value() == "" || m.editor.History.InNavigation()):
		m.editor.Buffer = m.editor.History.Prev()
		m.editorScreen.SetValue(m.editor.Buffer)
		m.editorScreen.FocusStart()
		return m, nil
	case key.Matches(msg, m.keys.Down) && !m.loading && m.editorScreen.AtBoundary(false) &&
		m.editor.History.InNavigation():
		m.editor.Buffer = m.editor.History.Next()
		m.editorScreen.SetValue(m.editor.Buffer)
		m.editorScreen.Focus()
		return m, nil
	}

	// any other key stops navigating the history
	m.editor.History.Reset()
	updated, cmd := m.editorScreen.Update(msg)
	m.editorScreen = updated
	m.editor.Buffer = m.editorScreen.Value()
	return m, cmd
}

// executeEditor runs the current query in the background and shows a spinner.
func (m *Model) executeEditor() tea.Cmd {
	if m.store == nil {
		return nil
	}
	buf := m.editorScreen.Value()
	line := m.editorScreen.Line()
	st := m.store
	m.queryID++
	token := m.queryID
	timeout := time.Duration(m.prefs.QueryTimeout()) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	m.cancelQuery()
	m.cancel = cancel
	m.loading = true
	m.exported = ""
	return tea.Batch(
		func() tea.Msg {
			ed := editor.New()
			ed.Buffer = buf
			err := ed.ExecuteAt(ctx, st, line)
			if err != nil {
				ed.Error = friendlyErr(err).Error()
			}
			return editorDoneMsg{ed: ed, err: err, token: token}
		},
		m.spinner.Tick,
	)
}

// cancelQuery cancels a running query, if any.
func (m *Model) cancelQuery() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
}

func friendlyErr(err error) error {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return errors.New("query timed out")
	case errors.Is(err, context.Canceled):
		return errors.New("query cancelled")
	}
	return err
}
