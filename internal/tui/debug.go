package tui

import (
	"fmt"
	"os"

	term "github.com/charmbracelet/x/term"

	"github.com/agmonetti/picklock/internal/browser"
	"github.com/agmonetti/picklock/internal/editor"
	"github.com/agmonetti/picklock/internal/store"
	"github.com/agmonetti/picklock/internal/tui/screens"
)

// PrintLayout renders the connection screen and a sample workspace as plain
// text (no TUI) together with the terminal size the app would use. It is meant
// to diagnose layout problems on any terminal: `picklock --print-layout`. A zero
// width/height detects the real terminal (120x30 when not a TTY); a positive
// value forces the size, which makes layout bugs reproducible in CI.
func PrintLayout(width, height int) int {
	w, h := width, height
	if w == 0 || h == 0 {
		rw, rh, err := term.GetSize(os.Stdout.Fd())
		if err != nil {
			rw, rh = 120, 30
		}
		if w == 0 {
			w = rw
		}
		if h == 0 {
			h = rh
		}
	}
	w, h = correctSize(w, h)

	fmt.Printf("terminal size: %dx%d (after size guard)\n", w, h)

	fmt.Println("\n--- connect screen ---")
	fmt.Println(screens.NewConnScreen(nil).View(w, h))

	fmt.Println("\n--- workspace ---")
	layout := screens.ComputeLayout(w, h-2, true, true, true, 0, 0)
	fmt.Println(screens.RenderWorkspace(sampleBrowser(), screens.NewEditorScreen(),
		sampleEditor(), screens.FocusSidebar, false, layout, 0, 0, w, h-2))

	fmt.Println("\n--- workspace (no sidebar, small terminal) ---")
	layout = screens.ComputeLayout(50, h-2, true, true, true, 0, 0)
	fmt.Println(screens.RenderWorkspace(sampleBrowser(), screens.NewEditorScreen(),
		sampleEditor(), screens.FocusSidebar, false, layout, 0, 0, 50, h-2))

	return 0
}

// sampleBrowser builds a synthetic browser with a few tables and rows so the
// workspace can be rendered without a real database.
func sampleBrowser() *browser.Browser {
	return &browser.Browser{
		Tables:      []string{"orders", "sessions", "users"},
		ActiveTable: "users",
		Columns: []store.Column{
			{Name: "id", Type: "INTEGER", PK: true, NotNull: true},
			{Name: "name", Type: "TEXT", NotNull: true},
			{Name: "email", Type: "TEXT"},
		},
		Rows:      [][]string{{"1", "Alice", "alice@test.com"}, {"2", "Bob", "bob@test.com"}},
		Indexes:   []store.Index{{Name: "idx_email", Columns: []string{"email"}, Unique: true}},
		TotalRows: 2,
		PageSize:  browser.PageSizeDefault,
	}
}

func sampleEditor() *editor.Editor {
	return &editor.Editor{Buffer: "SELECT * FROM users LIMIT 10", History: editor.NewHistory()}
}
