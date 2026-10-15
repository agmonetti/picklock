package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/agmonetti/picklock/internal/browser"
	"github.com/agmonetti/picklock/internal/store"
)

// browserDoneMsg carries the result of an asynchronous browser operation.
type browserDoneMsg struct {
	token int
	navID int
	err   error
	b     *browser.Browser
	load  bool
}

type preparedDoneMsg struct {
	token   int
	navID   int
	err     error
	b       *browser.Browser
	item    *browser.PreparedItem
	index   int
	initial bool
	load    bool
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

// loadBrowserCmd loads the catalog and inspects the first item without fetching rows.
func (m *Model) loadBrowserCmd(st store.DataSource) tea.Cmd {
	token := m.queryID
	timeout := time.Duration(m.prefs.QueryTimeout()) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	m.cancelConnect()
	m.connectCancel = cancel
	m.connecting = true
	return tea.Batch(func() tea.Msg {
		b, err := browser.New(ctx, st)
		if err != nil {
			return preparedDoneMsg{token: token, err: err, load: true}
		}
		if len(b.Items) == 0 {
			return preparedDoneMsg{token: token, b: b, load: true}
		}
		item, err := b.PrepareItem(ctx, b.Items[0].Name, st)
		return preparedDoneMsg{token: token, err: err, b: b, item: item, index: 0, initial: true, load: true}
	}, m.spinner.Tick)
}

func (m *Model) prepareItemCmd(name string, index int) tea.Cmd {
	if m.browser == nil || m.store == nil || m.navigating {
		return nil
	}
	clone, st := m.browser.Clone(), m.store
	token := m.queryID
	m.navID++
	navID := m.navID
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(m.prefs.QueryTimeout())*time.Second)
	m.cancelNav()
	m.navCancel, m.navigating, m.browsePreparing = cancel, true, true
	return tea.Batch(func() tea.Msg {
		item, err := clone.PrepareItem(ctx, name, st)
		return preparedDoneMsg{token: token, navID: navID, err: err, b: clone, item: item, index: index}
	}, m.spinner.Tick)
}

func (m *Model) reloadActiveCmd() tea.Cmd {
	if m.browser == nil || m.store == nil || m.browser.ActiveTable == "" || m.navigating {
		return nil
	}
	clone, st, name := m.browser.Clone(), m.store, m.browser.ActiveTable
	m.browseTargetIndex = 0
	for i, candidate := range clone.Tables {
		if candidate == name {
			m.browseTargetIndex = i
			break
		}
	}
	m.browsePreviousIndex = m.browseTargetIndex
	m.browseRefreshCurrent = true
	token := m.queryID
	m.navID++
	navID := m.navID
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(m.prefs.QueryTimeout())*time.Second)
	m.cancelNav()
	m.navCancel, m.navigating, m.browsePreparing = cancel, true, true
	return tea.Batch(func() tea.Msg {
		if err := clone.Load(ctx, st); err != nil {
			return preparedDoneMsg{token: token, navID: navID, err: err}
		}
		item, err := clone.PrepareItem(ctx, name, st)
		index := 0
		for i, candidate := range clone.Tables {
			if candidate == name {
				index = i
				break
			}
		}
		return preparedDoneMsg{token: token, navID: navID, err: err, b: clone, item: item, index: index}
	}, m.spinner.Tick)
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
