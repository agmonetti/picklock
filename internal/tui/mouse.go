package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/agmonetti/picklock/internal/browser"
	"github.com/agmonetti/picklock/internal/store"
	"github.com/agmonetti/picklock/internal/tui/screens"
)

// handleMouse handles mouse events. On the connection screen a left click
// focuses the clicked field / engine, toggles a checkbox, connects or opens a
// saved connection. In the workspace a left click focuses and selects the
// clicked row, a right-click drag resizes the nearest pane divider and the
// wheel scrolls the pane under the pointer. Page changes from the wheel run in
// the background; the returned cmd carries the navigation.
func (m *Model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	if m.showHelp {
		return nil
	}
	switch m.screen {
	case ScreenConnect:
		return m.handleConnectMouse(msg)
	case ScreenSettings:
		return nil
	case ScreenWorkspace:
		return m.handleWorkspaceMouse(msg)
	}
	return nil
}

// handleConnectMouse handles left clicks on the connection screen. The screen
// content starts at terminal cell (1,2): the frame adds a blank line, the
// header and a one-column left margin.
func (m *Model) handleConnectMouse(msg tea.MouseMsg) tea.Cmd {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return nil
	}
	return m.connect.Activate(m.connect.HitTest(msg.X-1, msg.Y-2))
}

// handleWorkspaceMouse handles mouse events inside the workspace.
func (m *Model) handleWorkspaceMouse(msg tea.MouseMsg) tea.Cmd {
	if m.showDetail {
		return m.handleDetailMouse(msg)
	}
	if m.navigating {
		// a navigation is in flight: ignore wheel/click row ops until it lands
		return nil
	}
	innerW := m.width - 2
	if innerW < 1 {
		innerW = 1
	}
	contentHeight := m.height - 4
	if contentHeight < 1 {
		contentHeight = 1
	}
	layout := screens.ComputeLayout(innerW, contentHeight, m.showSidebar, m.showMain, m.showEditor, m.sidebarW, m.editorH)

	// the workspace content starts at terminal cell (1, 2): the frame adds a
	// blank line, the header and a one-column left margin
	wx := msg.X - 1
	wy := msg.Y - 2

	if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown ||
		msg.Button == tea.MouseButtonWheelLeft || msg.Button == tea.MouseButtonWheelRight {
		return m.scrollAt(wx, wy, layout, msg.Button)
	}

	switch msg.Action {
	case tea.MouseActionPress:
		switch msg.Button {
		case tea.MouseButtonRight:
			if div := m.pickResizeDivider(wx, wy, layout); div != resizeNone {
				m.resizing = true
				m.resizeDiv = div
			}
		case tea.MouseButtonLeft:
			m.focusPaneAt(wx, wy, layout)
			m.selectAt(wx, wy, layout)
			line, col, inInput := m.resolveEditorPos(wx, wy, layout)
			if inInput && strings.TrimSpace(m.editorScreen.Value()) != "" {
				m.dragSelecting = true
				m.dragStartLine = line
				m.dragStartCol = col
				m.dragEndLine = line
				m.dragEndCol = col
			}
		}
	case tea.MouseActionMotion:
		if m.resizing {
			m.applyResize(wx, wy, layout)
		}
		if m.dragSelecting {
			line, col, _ := m.resolveEditorPos(wx, wy, layout)
			m.dragEndLine = line
			m.dragEndCol = col
		}
	case tea.MouseActionRelease:
		if m.resizing {
			m.resizing = false
			m.resizeDiv = resizeNone
			m.persistLayout()
		}
		if m.dragSelecting {
			m.dragSelecting = false
			line, col, _ := m.resolveEditorPos(wx, wy, layout)
			m.dragEndLine = line
			m.dragEndCol = col
			if m.dragStartLine != m.dragEndLine || m.dragStartCol != m.dragEndCol {
				selected := screens.ExtractTextRange(m.editorScreen.Value(),
					m.dragStartLine, m.dragStartCol, m.dragEndLine, m.dragEndCol)
				if strings.TrimSpace(selected) != "" {
					if err := clipboardWriteAll(selected); err == nil {
						return m.setSuccess("selection copied to clipboard")
					}
				}
			}
		}
	}
	return nil
}

// resolveEditorPos maps workspace cell coordinates to (line, col) inside the query editor textarea.
func (m *Model) resolveEditorPos(wx, wy int, layout screens.WorkspaceLayout) (line, col int, inInput bool) {
	if !layout.ShowEditor {
		return 0, 0, false
	}
	editorTop := 0
	if layout.ShowMain {
		editorTop = layout.MainH + 1
	}
	editorContentTop := editorTop + 1
	inputHeight := screens.EditorInputHeight(layout.EditorH - 2)
	editorLeft := 0
	if layout.ShowSidebar {
		editorLeft = layout.SidebarW + 1
	}
	editorContentLeft := editorLeft + 2 // border (1) + padding (1)

	lineCount := m.editorScreen.LineCount()
	if lineCount < 1 {
		lineCount = 1
	}
	gutterW := screens.EditorGutterWidth(lineCount)
	textLeft := editorContentLeft + gutterW

	relY := wy - editorContentTop
	relX := wx - textLeft
	if relX < 0 {
		relX = 0
	}
	inInput = wy >= editorContentTop && wy < editorContentTop+inputHeight && wx >= editorLeft && wx <= editorLeft+layout.RightW

	lines := strings.Split(m.editorScreen.Value(), "\n")
	if len(lines) == 0 {
		return 0, 0, inInput
	}
	if relY < 0 {
		relY = 0
	}
	if relY >= len(lines) {
		relY = len(lines) - 1
	}
	return relY, relX, inInput
}

// scrollAt scrolls the pane under the pointer with the mouse wheel.
func (m *Model) scrollAt(wx, wy int, layout screens.WorkspaceLayout, btn tea.MouseButton) tea.Cmd {
	delta := 0
	switch btn {
	case tea.MouseButtonWheelUp:
		delta = -1
	case tea.MouseButtonWheelDown:
		delta = 1
	case tea.MouseButtonWheelLeft:
		if layout.ShowMain && (!layout.ShowSidebar || wx >= layout.SidebarW) && wy <= layout.MainH {
			m.scrollColLeft()
		} else if layout.ShowEditor && (!layout.ShowSidebar || wx >= layout.SidebarW) && (!layout.ShowMain || wy > layout.MainH) {
			m.scrollEditorColLeft()
		}
		return nil
	case tea.MouseButtonWheelRight:
		if layout.ShowMain && (!layout.ShowSidebar || wx >= layout.SidebarW) && wy <= layout.MainH {
			m.scrollColRight()
		} else if layout.ShowEditor && (!layout.ShowSidebar || wx >= layout.SidebarW) && (!layout.ShowMain || wy > layout.MainH) {
			m.scrollEditorColRight()
		}
		return nil
	default:
		return nil
	}
	const wheelStep = 3

	switch {
	case layout.ShowSidebar && wx < layout.SidebarW:
		m.scrollSidebar(delta * wheelStep)
	case layout.ShowEditor && (!layout.ShowMain || wy > layout.MainH):
		m.scrollResults(delta*wheelStep, layout)
	case layout.ShowMain && m.structure:
		m.scrollStructure(delta * wheelStep)
		m.clampStructureScroll(layout.MainH - 2)
	case layout.ShowMain:
		return m.scrollMain(delta * wheelStep)
	}
	return nil
}

func (m *Model) scrollSidebar(delta int) {
	if m.browser == nil {
		return
	}
	m.sidebarCursor += delta
	if m.sidebarCursor < 0 {
		m.sidebarCursor = 0
	}
	if n := len(m.browser.Tables); m.sidebarCursor >= n {
		m.sidebarCursor = n - 1
	}
}

func (m *Model) scrollMain(delta int) tea.Cmd {
	b := m.browser
	if b == nil || len(b.Rows) == 0 {
		return nil
	}
	if delta < 0 && b.Cursor == 0 && b.HasPrevPage() {
		// land on the last row of the previous page after the async swap
		return m.runBrowserOp(func(nb *browser.Browser, st store.DataSource, ctx context.Context) error {
			if err := nb.PrevPage(ctx, st); err != nil {
				return err
			}
			nb.MoveCursor(len(nb.Rows))
			return nil
		})
	}
	if delta > 0 && b.Cursor >= len(b.Rows)-1 && b.HasNextPage() {
		return m.runBrowserOp(func(nb *browser.Browser, st store.DataSource, ctx context.Context) error {
			return nb.NextPage(ctx, st)
		})
	}
	b.MoveCursor(delta)
	return nil
}

func (m *Model) scrollResults(delta int, layout screens.WorkspaceLayout) {
	tab, ok := m.editor.Data.(*store.TabularData)
	if !ok || len(tab.Rows) == 0 {
		return
	}
	_, dataRows := screens.EditorResultsLayout(layout.EditorH - 2)
	maxScroll := len(tab.Rows) - dataRows
	if maxScroll < 0 {
		maxScroll = 0
	}
	m.editorScreen.SetResultScroll(m.editorScreen.ResultScroll() + delta)
	if s := m.editorScreen.ResultScroll(); s < 0 {
		m.editorScreen.SetResultScroll(0)
	} else if s > maxScroll {
		m.editorScreen.SetResultScroll(maxScroll)
	}
}

// selectAt selects the row or table under the pointer on a left click.
func (m *Model) selectAt(wx, wy int, layout screens.WorkspaceLayout) {
	switch {
	case layout.ShowSidebar && wx < layout.SidebarW:
		if m.browser == nil {
			return
		}
		line := wy - 1
		if idx := screens.SidebarItemAtLine(m.browser, m.sidebarCursor, layout.MainH+layout.EditorH-1, line); idx >= 0 {
			m.sidebarCursor = idx
		}
	case layout.ShowEditor && (!layout.ShowMain || wy > layout.MainH):
		m.selectResultRow(wy, layout)
	case layout.ShowMain && m.structure:
		// The structure inspector is not a row-selectable data table.
		return
	case layout.ShowMain:
		if m.browser == nil {
			return
		}
		start, visible := screens.TableWindow(len(m.browser.Rows), m.browser.Cursor, layout.MainH-2)
		rel := wy - 3 // top border (1) + header line (1) + separator line (1)
		if rel >= 0 && rel < visible {
			if row := start + rel; row < len(m.browser.Rows) {
				m.browser.Cursor = row
			}
		}
	}
}

// selectResultRow selects the query result row under the pointer.
func (m *Model) selectResultRow(wy int, layout screens.WorkspaceLayout) {
	tab, ok := m.editor.Data.(*store.TabularData)
	if !ok || len(tab.Rows) == 0 {
		return
	}
	editorTop := 0
	if layout.ShowMain {
		editorTop = layout.MainH + 1
	}
	startLine, _ := screens.EditorResultsLayout(layout.EditorH - 2)
	rel := (wy - editorTop - 1) - startLine - 2 // header + sep line above data rows
	if rel < 0 {
		return
	}
	row := m.editorScreen.ResultScroll() + rel
	if row >= 0 && row < len(tab.Rows) {
		m.editorScreen.SetResultCursor(row)
	}
}

// pickResizeDivider returns the pane divider closest to the given workspace
// coordinates, or resizeNone when there is none to resize. A click must land
// within 2 cells of a divider to avoid grabbing the wrong pane when clicking
// far from any divider.
func (m *Model) pickResizeDivider(wx, wy int, layout screens.WorkspaceLayout) int {
	best := resizeNone
	bestDist := 1 << 30
	if layout.ShowSidebar && (layout.ShowMain || layout.ShowEditor) {
		if d := absInt(wx - layout.SidebarW); d < bestDist {
			bestDist = d
			best = resizeSidebar
		}
	}
	if layout.ShowMain && layout.ShowEditor {
		if d := absInt(wy - layout.MainH); d < bestDist {
			bestDist = d
			best = resizeEditor
		}
	}
	if bestDist > 2 {
		return resizeNone
	}
	return best
}

// applyResize moves the divider being dragged to the pointer position. The
// stored value is clamped again when the next layout is computed.
func (m *Model) applyResize(wx, wy int, layout screens.WorkspaceLayout) {
	switch m.resizeDiv {
	case resizeSidebar:
		m.sidebarW = wx
	case resizeEditor:
		m.editorH = layout.MainH + layout.EditorH - wy
	}
}

// focusPaneAt focuses the pane under the pointer.
func (m *Model) focusPaneAt(wx, wy int, layout screens.WorkspaceLayout) {
	switch {
	case layout.ShowSidebar && wx < layout.SidebarW:
		m.setFocus(screens.FocusSidebar)
	case layout.ShowEditor && (!layout.ShowMain || wy > layout.MainH):
		m.setFocus(screens.FocusEditor)
	case layout.ShowMain:
		m.setFocus(screens.FocusMain)
	}
}

// persistLayout stores the current pane sizes in the preferences.
func (m *Model) persistLayout() {
	m.prefs.SidebarWidth = m.sidebarW
	m.prefs.EditorHeight = m.editorH
	_ = m.prefs.Save() // best effort: layout is not critical
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func (m *Model) handleDetailMouse(msg tea.MouseMsg) tea.Cmd {
	innerW := m.width - 2
	if innerW < 1 {
		innerW = 1
	}

	wx := msg.X - 1
	wy := msg.Y - 2

	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.detailScroll--
		if m.detailScroll < 0 {
			m.detailScroll = 0
		}
		return nil
	case tea.MouseButtonWheelDown:
		m.detailScroll++
		return nil
	}

	switch msg.Action {
	case tea.MouseActionPress:
		if msg.Button == tea.MouseButtonLeft {
			m.selectDetailFieldAt(wy)
			m.dragSelecting = true
			m.dragStartLine = wy + m.detailScroll
			m.dragStartCol = wx
			m.dragEndLine = m.dragStartLine
			m.dragEndCol = m.dragStartCol
		}
	case tea.MouseActionMotion:
		if m.dragSelecting {
			m.dragEndLine = wy + m.detailScroll
			m.dragEndCol = wx
		}
	case tea.MouseActionRelease:
		if m.dragSelecting {
			m.dragSelecting = false
			m.dragEndLine = wy + m.detailScroll
			m.dragEndCol = wx
			if m.dragStartLine != m.dragEndLine || m.dragStartCol != m.dragEndCol {
				text := m.detailPlainText(innerW)
				if text != "" {
					selected := screens.ExtractTextRange(text,
						m.dragStartLine, m.dragStartCol, m.dragEndLine, m.dragEndCol)
					if strings.TrimSpace(selected) != "" {
						if err := clipboardWriteAll(selected); err == nil {
							return m.setSuccess("selection copied to clipboard")
						}
					}
				}
			}
		}
	}
	return nil
}

func (m *Model) selectDetailFieldAt(wy int) {
	if len(m.detailCols) == 0 {
		return
	}
	contentW := m.width - 6
	if contentW < 4 {
		contentW = 4
	}

	targetLine := wy + m.detailScroll - 2 // 2 lines: title + blank line
	if targetLine < 0 {
		return
	}

	currentLine := 0
	for i := 0; i < len(m.detailCols); i++ {
		val := ""
		if i < len(m.detailVals) {
			val = m.detailVals[i]
		}
		wrappedCount := 1
		if val != "" {
			wrappedCount = len(screens.WrapCells(val, contentW-4))
		}
		fieldHeight := 1 + wrappedCount + 1
		if targetLine >= currentLine && targetLine < currentLine+fieldHeight {
			m.detailCursor = i
			break
		}
		currentLine += fieldHeight
	}
}

func (m *Model) detailPlainText(innerW int) string {
	if m.detailDoc != "" {
		return m.detailDoc
	}
	rendered := m.renderActiveDetail(innerW, 10000)
	return ansi.Strip(rendered)
}
