// Package editor manages the query/command editor state: buffer, history and results.
package editor

import (
	"context"
	"strings"
	"time"

	"github.com/agmonetti/picklock/internal/store"
)

// EditorMode is the operational state of the editor.
type EditorMode int

const (
	// EditorModeNormal: ready to edit or run.
	EditorModeNormal EditorMode = iota
	// EditorModeExecuting: a query is running.
	EditorModeExecuting
)

// MaxResultRows caps how many items an editor query loads into memory.
const MaxResultRows = 10000

// Editor keeps the state of the query/command editor.
type Editor struct {
	Buffer    string
	History   *History
	Data      store.DataView
	Error     string
	Mode      EditorMode
	LastQuery string
	Wrote     bool
	Duration  time.Duration
}

// New creates an editor with an empty history.
func New() *Editor {
	return &Editor{History: NewHistory(), Mode: EditorModeNormal}
}

// Execute runs the query against the data source.
func (e *Editor) Execute(ds store.DataSource) error {
	return e.ExecuteAt(context.Background(), ds, 0)
}

// ExecuteAt runs the statement that contains line (0-based) against the data source.
func (e *Editor) ExecuteAt(ctx context.Context, ds store.DataSource, line int) error {
	buf := strings.TrimSpace(e.Buffer)
	if buf == "" {
		return nil
	}

	start := time.Now()
	defer func() { e.Duration = time.Since(start) }()

	e.Mode = EditorModeExecuting
	defer func() { e.Mode = EditorModeNormal }()

	q := ds.Query()
	var stmts []store.Statement
	if splitter, ok := q.(store.StatementSplitter); ok {
		stmts = splitter.SplitStatements(e.Buffer)
	} else {
		stmts = store.SplitStatements(e.Buffer)
	}
	if len(stmts) == 0 {
		// Only comments/whitespace in the buffer: nothing to run. Mirrors the
		// relational executor, which treats an empty split the same way.
		return nil
	}
	stmtIdx := 0
	if len(stmts) > 1 {
		stmtIdx = store.StatementAt(stmts, line)
	}
	stmtText := stmts[stmtIdx].Text

	e.Wrote = q.IsMutation(stmtText)

	// Send the statement under the cursor, never the whole buffer: the
	// non-relational engines (Cassandra, Neo4j, Redis, Mongo) treat the input
	// as a single statement and would fail on a multi-statement buffer.
	data, err := q.Execute(ctx, stmtText, line, MaxResultRows)
	if err != nil {
		e.Data = nil
		e.Error = err.Error()
		return err
	}

	e.Data = data
	e.Error = ""
	e.History.Push(stmtText)
	e.LastQuery = stmtText
	return nil
}

// Clear resets the buffer and the result/error.
func (e *Editor) Clear() {
	e.Buffer = ""
	e.Data = nil
	e.Error = ""
	e.History.Reset()
}

// Statements splits buffer into statements for line detection.
func (e *Editor) Statements() []store.Statement {
	return store.SplitStatements(e.Buffer)
}
