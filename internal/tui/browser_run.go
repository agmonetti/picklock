package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/agmonetti/picklock/internal/browser"
	"github.com/agmonetti/picklock/internal/store"
)

// browserDoneMsg carries the result of an asynchronous browser operation (a
// page change, an item selection, a reload or the initial connection load).
type browserDoneMsg struct {
	token int
	navID int // matches m.navID; guards against superseded/cancelled ops
	err   error
	b     *browser.Browser // the updated browser; nil on error
	load  bool             // true when it was the initial connection load
}

// runBrowserOp runs a browser mutation (select item, change page, reload) on
// a background goroutine.
func (m *Model) runBrowserOp(op func(b *browser.Browser, st store.DataSource, ctx context.Context) error) tea.Cmd {
	if m.browser == nil || m.store == nil || m.navigating {
		return nil
	}
	clone := m.browser.Clone()
	st := m.store
	token := m.queryID
	m.navID++
	navID := m.navID
	timeout := time.Duration(m.prefs.QueryTimeout()) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	m.cancelNav()
	m.navCancel = cancel
	m.navigating = true
	return tea.Batch(
		func() tea.Msg {
			if err := op(clone, st, ctx); err != nil {
				return browserDoneMsg{token: token, navID: navID, err: err}
			}
			return browserDoneMsg{token: token, navID: navID, b: clone}
		},
		m.spinner.Tick,
	)
}

// loadBrowserCmd loads the catalog and first page of a freshly connected store.
func (m *Model) loadBrowserCmd(st store.DataSource) tea.Cmd {
	token := m.queryID
	timeout := time.Duration(m.prefs.QueryTimeout()) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	m.cancelConnect()
	m.connectCancel = cancel
	m.connecting = true
	return tea.Batch(
		func() tea.Msg {
			b, err := browser.New(ctx, st)
			if err != nil {
				return browserDoneMsg{token: token, err: err, load: true}
			}
			return browserDoneMsg{token: token, b: b, load: true}
		},
		m.spinner.Tick,
	)
}

// cancelNav cancels a running browser navigation, if any.
func (m *Model) cancelNav() {
	if m.navCancel != nil {
		m.navCancel()
		m.navCancel = nil
	}
}

// cancelConnect cancels a running initial connection load, if any.
func (m *Model) cancelConnect() {
	if m.connectCancel != nil {
		m.connectCancel()
		m.connectCancel = nil
	}
}

// busy reports whether any background operation is running.
func (m *Model) busy() bool {
	return m.loading || m.navigating || m.connecting
}
