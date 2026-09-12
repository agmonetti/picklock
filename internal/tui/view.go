package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/agmonetti/picklock/internal/store"
	"github.com/agmonetti/picklock/internal/tui/screens"
	"github.com/agmonetti/picklock/internal/tui/styles"
)

// View implements tea.Model.
func (m *Model) View() string {
	if m.showHelp {
		return m.renderHelp()
	}
	return m.render()
}

func (m *Model) render() string {
	innerW := m.width - 2 // 1 char outer margin on each side
	if innerW < 1 {
		innerW = 1
	}

	header := m.renderHeader()
	footer := m.renderFooter()
	contentHeight := m.height - 4
	if contentHeight < 1 {
		contentHeight = 1
	}

	var content string
	switch m.screen {
	case ScreenConnect:
		content = m.connect.View(innerW, contentHeight)
	case ScreenSettings:
		content = m.settings.View(innerW, contentHeight)
	case ScreenWorkspace:
		if m.showDetail {
			content = m.renderActiveDetail(innerW, contentHeight)
		} else if m.exporting {
			content = m.renderExportPrompt(innerW, contentHeight)
		} else {
			layout := screens.ComputeLayout(innerW, contentHeight, m.showSidebar, m.showMain, m.showEditor, m.sidebarW, m.editorH)
			content = screens.RenderWorkspace(m.browser, m.editorScreen, m.editor,
				m.focus, m.structure, layout, m.sidebarCursor, m.colScroll, innerW, contentHeight)
		}
	}

	body := content
	if m.err != "" {
		body = content + "\n" + styles.StyleError.Render(m.err)
	}
	if m.warn != "" {
		body = body + "\n" + styles.StyleWarn.Render(m.warn)
	}
	if m.exported != "" {
		body = body + "\n" + styles.StyleSuccess.Render(m.exported)
	}
	body = styles.StyleOuterMargin.Render(body)

	return lipgloss.JoinVertical(lipgloss.Left,
		" ",
		" "+header+" ",
		body,
		" "+footer+" ",
		" ")
}

func (m *Model) renderHeader() string {
	label := "no connection"
	if m.store != nil {
		label = m.cfgLabel
	}
	labelStyle := styles.StylePillDefault
	if m.store != nil {
		labelStyle = styles.StylePillConn
	}

	table := ""
	if m.browser != nil && m.browser.ActiveTable != "" {
		table = m.browser.ActiveTable
	}
	tableStyle := styles.StylePillDefault
	if table != "" {
		tableStyle = styles.StylePillTable
	}

	parts := []string{
		styles.StyleHeader.Render("picklock"),
		" " + styles.StylePillDefault.Render("[") + labelStyle.Render(label) + styles.StylePillDefault.Render("]"),
	}
	if table != "" {
		parts = append(parts, " "+styles.StylePillDefault.Render("[")+tableStyle.Render(table)+styles.StylePillDefault.Render("]"))
	}

	return strings.Join(parts, " ")
}

// binding is a footer shortcut: a pressing key and the action it triggers.
type binding struct {
	key    string
	action string
}

// footerBindings renders [key] action pairs separated by a middot.
func footerBindings(pairs []binding) string {
	var b strings.Builder
	for i, p := range pairs {
		if i > 0 {
			b.WriteString(styles.StyleFooter.Render(" · "))
		}
		b.WriteString(styles.StyleFooterKey.Render("[" + p.key + "]"))
		b.WriteString(" " + p.action)
	}
	return b.String()
}

func (m *Model) renderFooter() string {
	sidebarNoun := "tables"
	if m.browser != nil && m.browser.ItemNoun != "" {
		sidebarNoun = m.browser.ItemNoun + "s"
	}

	left := ""
	switch m.screen {
	case ScreenConnect:
		left = footerBindings([]binding{
			{"↑↓", "saved"},
			{"tab", "engine/fields"},
			{"←→", "engine"},
			{"space", "SQL/NoSQL"},
			{"enter", "connect"},
			{"ctrl+s", "save"},
			{"d", "delete"},
			{"ctrl+p", "settings"},
		})
	case ScreenSettings:
		left = footerBindings([]binding{
			{"enter", "save"},
			{"esc", "back"},
		})
	case ScreenWorkspace:
		if m.showDetail {
			left = footerBindings([]binding{
				{"↑↓", "fields"},
				{"c", "copy val"},
				{"C", "copy field"},
				{"a", "copy all"},
				{"esc", "back"},
			})
		} else {
			switch m.focus {
			case screens.FocusSidebar:
				left = footerBindings([]binding{
					{"↑↓", sidebarNoun},
					{"enter", "open"},
					{"tab", "next"},
					{"?", "help"},
					{"ctrl+p", "settings"},
				})
			case screens.FocusMain:
				if m.structure {
					left = footerBindings([]binding{
						{"esc", "back"},
						{"tab", "next"},
					})
				} else {
					left = footerBindings([]binding{
						{"↑↓", "navigate"},
						{"←→", "columns"},
						{"i", "structure"},
						{"v", "detail"},
						{"r", "refresh"},
						{"pgup/pgdn", "page"},
						{"tab", "next"},
						{"right-click", "resize"},
					})
				}
			case screens.FocusEditor:
				bindings := []binding{
					{"ctrl+r", "run"},
					{"alt+c", "copy"},
					{"ctrl+l", "clear"},
					{"esc", "back"},
				}
				if m.editor != nil && m.editor.Data != nil {
					if tab, ok := m.editor.Data.(*store.TabularData); ok && len(tab.Columns) > 1 {
						bindings = []binding{
							{"ctrl+r", "run"},
							{"alt+←/→", "cols"},
							{"alt+c", "copy"},
							{"ctrl+l", "clear"},
							{"esc", "back"},
						}
					}
				}
				left = footerBindings(bindings)
			}
		}
	}

	right := ""
	switch {
	case m.loading:
		right = m.spinner.View() + " running query…"
	case m.connecting:
		right = m.spinner.View() + " connecting…"
	case m.navigating:
		right = m.spinner.View() + " loading…"
	}
	if right == "" && m.screen == ScreenWorkspace && m.focus != screens.FocusEditor &&
		m.browser != nil && m.browser.ActiveTable != "" && m.browser.TotalRows > 0 {
		first := m.browser.Page*m.browser.PageSize + 1
		last := (m.browser.Page + 1) * m.browser.PageSize
		if last > m.browser.TotalRows {
			last = m.browser.TotalRows
		}
		arrow := ""
		if m.browser.HasNextPage() {
			arrow = " ▼"
		}
		right = fmt.Sprintf("%d-%d/%d%s", first, last, m.browser.TotalRows, arrow)
	}

	pad := (m.width - 2) - lipgloss.Width(left) - lipgloss.Width(right)
	if pad < 1 {
		pad = 1
	}
	return styles.StyleFooter.Render(left + padSpaces(pad) + right)
}

func (m *Model) renderHelp() string {
	var out string
	for _, group := range m.keys.FullHelp() {
		for _, b := range group {
			key := fmt.Sprintf("[%s]", b.Help().Key)
			out += fmt.Sprintf("  %-20s %s\n", styles.StyleFooterKey.Render(key), styles.StyleHeaderDim.Render(b.Help().Desc))
		}
		out += "\n"
	}
	out += fmt.Sprintf("  %-20s %s\n", styles.StyleFooterKey.Render("[right-click drag]"), styles.StyleHeaderDim.Render("resize panes"))
	out += fmt.Sprintf("  %-20s %s\n", styles.StyleFooterKey.Render("[drag click]"), styles.StyleHeaderDim.Render("select & copy text"))
	out += fmt.Sprintf("  %-20s %s\n", styles.StyleFooterKey.Render("[click]"), styles.StyleHeaderDim.Render("focus / select item"))
	out += fmt.Sprintf("  %-20s %s\n", styles.StyleFooterKey.Render("[wheel]"), styles.StyleHeaderDim.Render("scroll pane"))

	return lipgloss.JoinVertical(lipgloss.Left,
		styles.StyleHeader.Render("Help"),
		"",
		out,
		styles.StyleHeaderDim.Render("? to close"),
	)
}

func padSpaces(n int) string {
	if n < 0 {
		n = 0
	}
	return fmt.Sprintf("%*s", n, "")
}
