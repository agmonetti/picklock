package screens

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/agmonetti/picklock/internal/editor"
	"github.com/agmonetti/picklock/internal/store"
	"github.com/agmonetti/picklock/internal/tui/styles"
)

// EditorScreen keeps the multiline query editor input and its rendering.
type EditorScreen struct {
	ta    textarea.Model
	title string

	resultScroll int
	resultCursor int
	colScroll    int
}

// NewEditorScreen creates the editor textarea.
func NewEditorScreen() *EditorScreen {
	ta := textarea.New()
	ta.Cursor.BlinkSpeed = CursorBlink
	ta.Placeholder = "SELECT * FROM table LIMIT 10"
	ta.ShowLineNumbers = true
	ta.Prompt = ""
	ta.CharLimit = 0
	ta.FocusedStyle.LineNumber = styles.StyleEditorLineNo
	ta.FocusedStyle.CursorLineNumber = styles.StyleEditorLineNoCursor
	ta.BlurredStyle.LineNumber = styles.StyleEditorLineNo
	ta.BlurredStyle.CursorLineNumber = styles.StyleEditorLineNo
	return &EditorScreen{ta: ta, title: "SQL EDITOR"}
}

// SetPlaceholder sets the textarea placeholder.
func (s *EditorScreen) SetPlaceholder(p string) {
	if p != "" {
		s.ta.Placeholder = p
	}
}

// SetTitle sets the editor pane title.
func (s *EditorScreen) SetTitle(t string) { s.title = t }

// Title returns the editor pane title.
func (s *EditorScreen) Title() string { return s.title }

// formatDuration renders a query duration compactly (e.g. 42µs, 1.2ms, 3s).
func formatDuration(d time.Duration) string {
	switch {
	case d <= 0:
		return "0s"
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1000)
	}
	return d.Round(time.Millisecond).String()
}

// SetResultScroll sets the scroll offset of the query results table.
func (s *EditorScreen) SetResultScroll(n int) { s.resultScroll = n }

// ResultScroll returns the scroll offset of the query results table.
func (s *EditorScreen) ResultScroll() int { return s.resultScroll }

// SetResultCursor selects a row of the query results (-1 = none).
func (s *EditorScreen) SetResultCursor(n int) { s.resultCursor = n }

// ResultCursor returns the selected row of the query results.
func (s *EditorScreen) ResultCursor() int { return s.resultCursor }

// SetColScroll sets the column horizontal scroll offset of the query results table.
func (s *EditorScreen) SetColScroll(n int) { s.colScroll = n }

// ColScroll returns the column horizontal scroll offset of the query results table.
func (s *EditorScreen) ColScroll() int { return s.colScroll }

// ResetResult resets the results viewport after a new query.
func (s *EditorScreen) ResetResult() {
	s.resultScroll = 0
	s.resultCursor = -1
	s.colScroll = 0
}

// EditorInputHeight returns the textarea height for an editor pane content height.
func EditorInputHeight(contentH int) int {
	h := contentH / 2
	if h < 3 {
		h = 3
	}
	return h
}

// EditorResultsLayout returns the content line where the query results table starts.
func EditorResultsLayout(contentH int) (startLine, dataRows int) {
	ih := EditorInputHeight(contentH)
	regionH := contentH - ih
	if regionH < 2 {
		regionH = 2
	}
	dataRows = regionH - 2
	if dataRows < 0 {
		dataRows = 0
	}
	return ih, dataRows
}

// EditorGutterWidth calculates the line number gutter width for the given line count.
func EditorGutterWidth(lineCount int) int {
	digits := len(strconv.Itoa(lineCount))
	if digits < 1 {
		digits = 1
	}
	// StyleEditorLineNo has Padding(0, 1), so 1 leading space + digits + 1 trailing space
	return digits + 2
}

// ExtractTextRange extracts a substring from a multiline string between
// (startLine, startCol) and (endLine, endCol) 0-indexed coordinates.
func ExtractTextRange(text string, startLine, startCol, endLine, endCol int) string {
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return ""
	}

	// Normalize order so (startLine, startCol) is before (endLine, endCol)
	if startLine > endLine || (startLine == endLine && startCol > endCol) {
		startLine, endLine = endLine, startLine
		startCol, endCol = endCol, startCol
	}

	if startLine < 0 {
		startLine = 0
		startCol = 0
	}
	if startLine >= len(lines) {
		return ""
	}
	if endLine < 0 {
		return ""
	}
	if endLine >= len(lines) {
		endLine = len(lines) - 1
		endCol = len(lines[endLine])
	}

	if startCol < 0 {
		startCol = 0
	}
	if startCol > len(lines[startLine]) {
		startCol = len(lines[startLine])
	}
	if endCol < 0 {
		endCol = 0
	}
	if endCol > len(lines[endLine]) {
		endCol = len(lines[endLine])
	}

	if startLine == endLine {
		if startCol >= endCol {
			return ""
		}
		return lines[startLine][startCol:endCol]
	}

	var result []string
	result = append(result, lines[startLine][startCol:])
	for i := startLine + 1; i < endLine; i++ {
		result = append(result, lines[i])
	}
	result = append(result, lines[endLine][:endCol])
	return strings.Join(result, "\n")
}

// SetValue replaces the textarea content.
func (s *EditorScreen) SetValue(v string) { s.ta.SetValue(v) }

// Value returns the current textarea content.
func (s *EditorScreen) Value() string { return s.ta.Value() }

// Focus focuses the textarea and moves the cursor to the end.
func (s *EditorScreen) Focus() tea.Cmd {
	cmd := s.ta.Focus()
	s.ta.CursorEnd()
	return cmd
}

// FocusStart focuses the textarea and moves the cursor to the start.
func (s *EditorScreen) FocusStart() tea.Cmd {
	cmd := s.ta.Focus()
	s.ta.CursorStart()
	return cmd
}

// Blur blurs the textarea.
func (s *EditorScreen) Blur() { s.ta.Blur() }

// Update forwards the message to the textarea.
func (s *EditorScreen) Update(msg tea.Msg) (*EditorScreen, tea.Cmd) {
	ta, cmd := s.ta.Update(msg)
	s.ta = ta
	return s, cmd
}

// AtBoundary reports whether the cursor is at the top or bottom edge.
func (s *EditorScreen) AtBoundary(up bool) bool {
	if up {
		return s.ta.Line() == 0
	}
	return s.ta.Line() >= s.ta.LineCount()-1
}

// Line returns the current cursor line.
func (s *EditorScreen) Line() int { return s.ta.Line() }

// LineCount returns the number of lines in the input.
func (s *EditorScreen) LineCount() int { return s.ta.LineCount() }

// View renders the editor input + results.
func (s *EditorScreen) View(e *editor.Editor, width, height int) string {
	if width < 1 {
		width = 80
	}
	s.ta.SetWidth(width - 2)
	inputHeight := height / 2
	if inputHeight < 3 {
		inputHeight = 3
	}
	s.ta.SetHeight(inputHeight - 1)

	var b strings.Builder
	b.WriteString(s.ta.View())

	// separator
	b.WriteString("\n" + styles.StyleBorder.Render(strings.Repeat("─", width-2)) + "\n")

	if e.Error != "" {
		b.WriteString(styles.StyleError.Render(e.Error))
		return b.String()
	}

	if e.Data == nil {
		b.WriteString(styles.StyleHeaderDim.Render("  ctrl+r to run"))
		return b.String()
	}

	resH := height - inputHeight
	if resH < 2 {
		resH = 2
	}

	switch v := e.Data.(type) {
	case *store.TabularData:
		if len(v.Columns) > 0 {
			rows := v.Rows
			dataRows := resH - 2
			if dataRows < 0 {
				dataRows = 0
			}
			maxScroll := len(rows) - dataRows
			if maxScroll < 0 {
				maxScroll = 0
			}
			scroll := s.resultScroll
			if scroll < 0 {
				scroll = 0
			}
			if scroll > maxScroll {
				scroll = maxScroll
			}
			view := rows[scroll:]
			var viewNulls [][]bool
			if v.Nulls != nil && scroll < len(v.Nulls) {
				viewNulls = v.Nulls[scroll:]
			}
			cursor := -1
			if s.resultCursor >= scroll {
				cursor = s.resultCursor - scroll
				if cursor >= len(view) {
					cursor = len(view) - 1
				}
			}
			b.WriteString(RenderDataTableWithNulls(v.Columns, view, viewNulls, cursor, s.colScroll, width-2, resH))
			if v.Truncated {
				b.WriteString(styles.StyleHeaderDim.Render(
					fmt.Sprintf("  showing first %d rows (%s)", editor.MaxResultRows, formatDuration(e.Duration))) + "\n")
			}
		} else if v.Affected >= 0 {
			noun := "rows"
			if v.Affected == 1 {
				noun = "row"
			}
			b.WriteString(styles.StyleHeader.Render("STATUS: OK") +
				styles.StyleHeaderDim.Render(fmt.Sprintf(" (%d %s affected, %s)",
					v.Affected, noun, formatDuration(e.Duration))))
		}

	case *store.DocumentData:
		b.WriteString(RenderDocumentList(v, s.resultCursor, width-2, resH))
		b.WriteString("\n" + styles.StyleHeaderDim.Render(fmt.Sprintf("  %s (%s)", v.Summary(), formatDuration(e.Duration))))

	case *store.KeyValueData:
		b.WriteString(RenderKeyValue(v, s.resultCursor, width-2, resH))

	case *store.GraphData:
		b.WriteString(RenderGraph(v, s.resultCursor, width-2, resH))
		b.WriteString("\n" + styles.StyleHeaderDim.Render(fmt.Sprintf("  %s (%s)", v.Summary(), formatDuration(e.Duration))))

	case *store.RawTextData:
		b.WriteString(RenderRawText(v, width-2, resH))
	}

	return b.String()
}
