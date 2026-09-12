package screens

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/agmonetti/picklock/internal/browser"
	"github.com/agmonetti/picklock/internal/editor"
	"github.com/agmonetti/picklock/internal/tui/styles"
)

// WorkspaceFocus identifies which pane of the workspace has the keyboard focus.
type WorkspaceFocus int

const (
	// FocusSidebar is the items list on the left.
	FocusSidebar WorkspaceFocus = iota
	// FocusMain is the central panel (browser or structure).
	FocusMain
	// FocusEditor is the bottom query editor.
	FocusEditor
)

// Pane sizing bounds.
const (
	SidebarMinW = 10
	EditorMinH  = 5
	RightMinW   = 20
	MainMinH    = 5
)

// WorkspaceLayout is the resolved geometry of the workspace panes.
type WorkspaceLayout struct {
	ShowSidebar bool
	ShowMain    bool
	ShowEditor  bool
	SidebarW    int
	EditorH     int
	MainH       int
	RightW      int
}

// ComputeLayout resolves the pane sizes for the given terminal size and panel visibility.
func ComputeLayout(width, height int, showSidebar, showMain, showEditor bool, sidebarW, editorH int) WorkspaceLayout {
	if !showSidebar && !showMain && !showEditor {
		showMain = true
	}
	if width < 60 {
		showSidebar = false
	}
	if !showSidebar && !showMain && !showEditor {
		showMain = true
	}

	l := WorkspaceLayout{
		ShowSidebar: showSidebar,
		ShowMain:    showMain,
		ShowEditor:  showEditor,
	}

	switch {
	case showMain && showEditor:
		maxEditorH := height - MainMinH - 1
		minEditorH := EditorMinH
		if maxEditorH < minEditorH {
			l.EditorH = height / 2
			if l.EditorH < 2 {
				l.EditorH = 2
			}
			l.MainH = height - l.EditorH - 1
			if l.MainH < 2 {
				l.MainH = 2
			}
		} else {
			if editorH > 0 {
				l.EditorH = clampInt(editorH, minEditorH, maxEditorH)
			} else {
				l.EditorH = height / 4
				if l.EditorH < 9 {
					l.EditorH = 9
				}
				l.EditorH = clampInt(l.EditorH, minEditorH, maxEditorH)
			}
			l.MainH = height - l.EditorH - 1
		}
	case showMain && !showEditor:
		l.MainH = height
		l.EditorH = 0
	case !showMain && showEditor:
		l.EditorH = height
		l.MainH = 0
	default:
		l.MainH = 0
		l.EditorH = 0
	}

	if showSidebar {
		if !showMain && !showEditor {
			l.SidebarW = width
			l.RightW = 0
		} else {
			maxSidebarW := width - RightMinW - 1
			minSidebarW := SidebarMinW
			if maxSidebarW < minSidebarW {
				l.SidebarW = width / 2
				if l.SidebarW < 2 {
					l.SidebarW = 2
				}
				l.RightW = width - l.SidebarW - 1
				if l.RightW < 2 {
					l.RightW = 2
				}
			} else {
				if sidebarW > 0 {
					l.SidebarW = clampInt(sidebarW, minSidebarW, maxSidebarW)
				} else {
					l.SidebarW = clampInt(width/5, 14, 20)
					l.SidebarW = clampInt(l.SidebarW, minSidebarW, maxSidebarW)
				}
				l.RightW = width - l.SidebarW - 1
			}
		}
	} else {
		l.SidebarW = 0
		l.RightW = width
	}

	return l
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// RenderWorkspace renders the single working screen.
func RenderWorkspace(b *browser.Browser, es *EditorScreen, e *editor.Editor,
	focus WorkspaceFocus, structure bool, layout WorkspaceLayout, sidebarCursor, colScroll int,
	width, height int) string {
	if width < 10 || height < 3 {
		return styles.StyleHeaderDim.Render("terminal too small")
	}

	sidebarW := layout.SidebarW
	editorH := layout.EditorH
	mainH := layout.MainH
	rightW := layout.RightW
	showSidebar := layout.ShowSidebar
	showMain := layout.ShowMain
	showEditor := layout.ShowEditor

	gap := " "
	contentW := func(paneW int) int {
		if w := paneW - 4; w < 1 {
			return 1
		}
		return paneW - 4
	}

	var right string
	if showMain && showEditor {
		mainTitle := ""
		if b != nil && b.ActiveTable != "" {
			if b.Data != nil {
				mainTitle = fmt.Sprintf("%s · %s", b.ActiveTable, b.Data.Summary())
			} else if b.TotalRows > 0 {
				noun := "rows"
				if b.TotalRows == 1 {
					noun = "row"
				}
				mainTitle = fmt.Sprintf("%s · %d %s", b.ActiveTable, b.TotalRows, noun)
			} else {
				mainTitle = fmt.Sprintf("%s · (empty)", b.ActiveTable)
			}
		}
		mainBodyH := mainH - 2
		var mainContent string
		if b == nil {
			mainContent = styles.StyleHeaderDim.Render("no connection")
		} else if structure {
			mainContent = RenderStructure(b, contentW(rightW), mainBodyH)
		} else {
			mainContent = RenderMainBrowser(b, colScroll, contentW(rightW), mainBodyH)
		}

		editorContent := es.View(e, contentW(rightW), editorH-2)
		editorTitle := "QUERY EDITOR"
		if es.Title() != "" {
			editorTitle = es.Title()
		}

		mainPane := boxed(mainContent, mainTitle, rightW, mainH, focus == FocusMain)
		editorPane := boxed(editorContent, editorTitle, rightW, editorH, focus == FocusEditor)
		right = lipgloss.JoinVertical(lipgloss.Top, mainPane, " ", editorPane)
	} else if showMain {
		mainTitle := ""
		if b != nil && b.ActiveTable != "" {
			if b.Data != nil {
				mainTitle = fmt.Sprintf("%s · %s", b.ActiveTable, b.Data.Summary())
			} else if b.TotalRows > 0 {
				noun := "rows"
				if b.TotalRows == 1 {
					noun = "row"
				}
				mainTitle = fmt.Sprintf("%s · %d %s", b.ActiveTable, b.TotalRows, noun)
			} else {
				mainTitle = fmt.Sprintf("%s · (empty)", b.ActiveTable)
			}
		}
		mainBodyH := mainH - 2
		var mainContent string
		if b == nil {
			mainContent = styles.StyleHeaderDim.Render("no connection")
		} else if structure {
			mainContent = RenderStructure(b, contentW(rightW), mainBodyH)
		} else {
			mainContent = RenderMainBrowser(b, colScroll, contentW(rightW), mainBodyH)
		}
		right = boxed(mainContent, mainTitle, rightW, mainH, focus == FocusMain)
	} else if showEditor {
		editorContent := es.View(e, contentW(rightW), editorH-2)
		editorTitle := "QUERY EDITOR"
		if es.Title() != "" {
			editorTitle = es.Title()
		}
		right = boxed(editorContent, editorTitle, rightW, editorH, focus == FocusEditor)
	}

	if !showSidebar {
		return right
	}

	sidebarTitle := "TABLES"
	if b != nil && b.CatalogTitle != "" {
		sidebarTitle = b.CatalogTitle
	}

	sidebarContent := RenderSidebar(b, sidebarCursor, contentW(sidebarW), height-2)
	sidebarPane := boxed(sidebarContent, sidebarTitle, sidebarW, height, focus == FocusSidebar)

	if right == "" {
		return sidebarPane
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, sidebarPane, gap, right)
}

func boxed(content, title string, width, height int, focused bool) string {
	if width < 2 || height < 2 {
		return ""
	}
	innerW := width - 2
	innerH := height - 2
	if innerW < 0 {
		innerW = 0
	}
	if innerH <= 0 {
		content = ""
	} else if lines := strings.Split(content, "\n"); len(lines) > innerH {
		content = strings.Join(lines[:innerH], "\n")
	}
	style := styles.StylePane
	if focused {
		style = styles.StylePaneFocus
	}
	box := style.Width(innerW).Height(innerH).Padding(0, 1).Render(content)
	lines := strings.Split(box, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	if title != "" && len(lines) > 0 {
		lines[0] = topBorder(title, width, focused)
	}
	return strings.Join(lines, "\n")
}

func topBorder(title string, width int, focused bool) string {
	border := styles.StyleBorderLine
	if focused {
		border = styles.StyleBorderLineFocus
	}
	left := "╭─ "
	maxTitle := width - 6
	if maxTitle < 0 {
		maxTitle = 0
	}
	title = truncate(title, maxTitle)
	fill := width - runewidth.StringWidth(left) - runewidth.StringWidth(title) - 2
	if fill < 1 {
		fill = 1
	}
	return border.Render(left) + styles.StylePaneTitle.Render(title) +
		border.Render(" "+strings.Repeat("─", fill)+"╮")
}
