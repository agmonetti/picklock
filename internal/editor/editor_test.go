package editor

import (
	"context"
	"strings"
	"testing"

	"github.com/agmonetti/picklock/internal/conn"
	"github.com/agmonetti/picklock/internal/store"
	_ "github.com/agmonetti/picklock/internal/store/sqlite"
)

func newTestStore(t *testing.T) store.DataSource {
	t.Helper()
	cfg := conn.New(conn.DriverSQLite)
	cfg.Path = ":memory:"
	st, err := store.New(cfg)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := st.Query().Execute(context.Background(), `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`, 0, 100); err != nil {
		t.Fatalf("create: %v", err)
	}
	return st
}

func TestEditor_ExecuteSelect(t *testing.T) {
	st := newTestStore(t)
	e := New()
	e.Buffer = "SELECT * FROM users"
	if err := e.Execute(st); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if e.Error != "" {
		t.Errorf("Error = %q, want empty", e.Error)
	}
	tab, ok := e.Data.(*store.TabularData)
	if !ok || len(tab.Columns) != 2 {
		t.Errorf("Data = %+v, want 2 columns", e.Data)
	}
	if tab.Affected != -1 {
		t.Errorf("Affected = %d, want -1 (read)", tab.Affected)
	}
}

func TestEditor_ExecuteInsert(t *testing.T) {
	st := newTestStore(t)
	e := New()
	e.Buffer = "INSERT INTO users (name) VALUES ('X')"
	if err := e.Execute(st); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	tab, ok := e.Data.(*store.TabularData)
	if !ok || tab.Affected != 1 {
		t.Errorf("Data = %+v, want Affected=1", e.Data)
	}
}

func TestEditor_ExecuteInvalidSQL(t *testing.T) {
	st := newTestStore(t)
	e := New()
	e.Buffer = "SELLECT * FROM users"
	err := e.Execute(st)
	if err == nil {
		t.Fatal("expected an SQL error")
	}
	if e.Error == "" || e.Data != nil {
		t.Errorf("Error=%q Data=%+v, want error and nil data", e.Error, e.Data)
	}
}

func TestEditor_ExecuteEmptyBuffer(t *testing.T) {
	st := newTestStore(t)
	e := New()
	e.Buffer = "   "
	if err := e.Execute(st); err != nil {
		t.Fatalf("Execute empty: %v", err)
	}
	if e.Data != nil {
		t.Errorf("Data = %+v, want nil", e.Data)
	}
}

func TestEditor_HistoryPushedOnExecute(t *testing.T) {
	st := newTestStore(t)
	e := New()
	e.Buffer = "SELECT 1"
	_ = e.Execute(st)
	if e.History.Len() != 1 {
		t.Errorf("History.Len = %d, want 1", e.History.Len())
	}
	// an invalid query is not added to the history
	e.Buffer = "BROKEN SQL"
	_ = e.Execute(st)
	if e.History.Len() != 1 {
		t.Errorf("History.Len = %d, want 1 (only successful queries)", e.History.Len())
	}
}

func TestEditor_Clear(t *testing.T) {
	st := newTestStore(t)
	e := New()
	e.Buffer = "SELECT 1"
	_ = e.Execute(st)
	e.Clear()
	if e.Buffer != "" || e.Data != nil || e.Error != "" {
		t.Errorf("Clear did not reset: %+v", e)
	}
}

func TestEditor_ExecuteOnlyFirstStatement(t *testing.T) {
	st := newTestStore(t)
	e := New()
	e.Buffer = "CREATE TABLE t (x INT); INSERT INTO t VALUES (1)"
	if err := e.Execute(st); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// the second statement must not have run
	res, err := st.Query().Execute(context.Background(), "SELECT COUNT(*) FROM t", 0, 100)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	tab := res.(*store.TabularData)
	if tab.Rows[0][0] != "0" {
		t.Errorf("Count = %s, want 0 (INSERT must not run)", tab.Rows[0][0])
	}
}

func TestEditor_ExecuteAtSelectsStatementAtCursor(t *testing.T) {
	st := newTestStore(t)
	e := New()
	e.Buffer = "SELECT 1;\nSELECT 2;\nSELECT * FROM users"

	// cursor on line 2 (0-based): the third statement
	if err := e.ExecuteAt(context.Background(), st, 2); err != nil {
		t.Fatalf("ExecuteAt: %v", err)
	}
	tab, ok := e.Data.(*store.TabularData)
	if !ok || len(tab.Columns) != 2 {
		t.Errorf("Data = %+v, want 2 columns from users", e.Data)
	}
}

func TestEditor_ExecuteAtSingleStatementIgnoresLine(t *testing.T) {
	st := newTestStore(t)
	e := New()
	e.Buffer = "INSERT INTO users (name) VALUES ('X')"
	if err := e.ExecuteAt(context.Background(), st, 5); err != nil {
		t.Fatalf("ExecuteAt: %v", err)
	}
	tab, ok := e.Data.(*store.TabularData)
	if !ok || tab.Affected != 1 {
		t.Errorf("Data = %+v, want Affected=1", e.Data)
	}
}

func TestEditor_FirstStatementRespectsStrings(t *testing.T) {
	// ";" inside a string literal does not split the statement
	stmts := store.SplitStatements("INSERT INTO users (name) VALUES ('a;b')")
	if len(stmts) != 1 {
		t.Errorf("len(stmts) = %d, want 1", len(stmts))
	}
	if stmts[0].Text != "INSERT INTO users (name) VALUES ('a;b')" {
		t.Errorf("stmt = %q", stmts[0].Text)
	}

	// doubled quotes '' inside the string
	stmts = store.SplitStatements("INSERT INTO t VALUES ('it''s; ok'); DROP TABLE x")
	if len(stmts) != 2 {
		t.Errorf("len(stmts) = %d, want 2", len(stmts))
	}
	if stmts[0].Text != "INSERT INTO t VALUES ('it''s; ok')" {
		t.Errorf("stmt = %q", stmts[0].Text)
	}

	// escaped backslash inside the string
	stmts = store.SplitStatements(`INSERT INTO t VALUES ('a\;b')`)
	if len(stmts) != 1 {
		t.Errorf("len(stmts) = %d, want 1", len(stmts))
	}
}

func TestEditor_FirstStatementIgnoresSemicolonsInComments(t *testing.T) {
	cases := []struct{ sql, want string }{
		{"SELECT 1; -- note with ; inside\nSELECT 2", "SELECT 1"},
		{"SELECT 1 /* block ; with ; */; SELECT 2", "SELECT 1"},
		{"-- c ; d\nSELECT 1; SELECT 2", "SELECT 1"},
		{"/* c ; d */\nSELECT 1; SELECT 2", "SELECT 1"},
		{"SELECT * FROM t -- ; done\n", "SELECT * FROM t"},
		{"SELECT 1 # mysql ; comment\n; SELECT 2", "SELECT 1"},
		// SQL Server temp tables: #temp is not a comment
		{"CREATE TABLE #temp (x INT); SELECT * FROM #temp", "CREATE TABLE #temp (x INT)"},
	}
	for _, tc := range cases {
		stmts := store.SplitStatements(tc.sql)
		if len(stmts) == 0 || stmts[0].Text != tc.want {
			t.Errorf("SplitStatements(%q)[0] = %q, want %q", tc.sql, stmts[0].Text, tc.want)
		}
	}
}

func TestEditor_CommentsDoNotGlueTokens(t *testing.T) {
	stmts := store.SplitStatements("SELECT a/*x*/b")
	if len(stmts) != 1 || stmts[0].Text != "SELECT a b" {
		t.Errorf("splitStatements = %+v, want one statement 'SELECT a b'", stmts)
	}
}

func TestEditor_WroteFlag(t *testing.T) {
	st := newTestStore(t)
	cases := []struct {
		sql  string
		want bool
	}{
		{"SELECT * FROM users", false},
		{"WITH x AS (SELECT 1) SELECT * FROM x", false},
		{"INSERT INTO users (name) VALUES ('x')", true},
		{"INSERT INTO users (name) VALUES ('x') RETURNING id", true},
		{"UPDATE users SET name = 'x'", true},
		{"CREATE TABLE t (x INT)", true},
		{"ALTER TABLE users ADD COLUMN y INT", true},
		{"DELETE FROM users", true},
		{"DROP TABLE users", true},
	}
	for _, tc := range cases {
		e := New()
		e.Buffer = tc.sql
		if err := e.Execute(st); err != nil {
			t.Fatalf("Execute(%q): %v", tc.sql, err)
		}
		if got := e.Wrote; got != tc.want {
			t.Errorf("Wrote for %q = %v, want %v", tc.sql, got, tc.want)
		}
	}
}

func TestEditor_ResultTruncatedOverLimit(t *testing.T) {
	st := newTestStore(t)
	for i := 0; i < MaxResultRows+5; i++ {
		if _, err := st.Query().Execute(context.Background(), "INSERT INTO users (name) VALUES ('u')", 0, 100); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	e := New()
	e.Buffer = "SELECT * FROM users"
	if err := e.Execute(st); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	tab, ok := e.Data.(*store.TabularData)
	if !ok || !tab.Truncated {
		t.Errorf("tab.Truncated = %v, want true", tab)
	}
	if len(tab.Rows) != MaxResultRows {
		t.Errorf("Rows = %d, want %d", len(tab.Rows), MaxResultRows)
	}
}

func TestEditor_SplitStatementsSpecialQuotes(t *testing.T) {
	stmts := store.SplitStatements(`SELECT * FROM "users;backup"; SELECT 2`)
	if len(stmts) != 2 || stmts[0].Text != `SELECT * FROM "users;backup"` {
		t.Errorf("stmts = %+v", stmts)
	}

	stmts = store.SplitStatements("SELECT * FROM `users;backup`; SELECT 2")
	if len(stmts) != 2 || stmts[0].Text != "SELECT * FROM `users;backup`" {
		t.Errorf("stmts = %+v", stmts)
	}

	stmts = store.SplitStatements("SELECT * FROM [users;backup]; SELECT 2")
	if len(stmts) != 2 || stmts[0].Text != "SELECT * FROM [users;backup]" {
		t.Errorf("stmts = %+v", stmts)
	}

	sql := "CREATE FUNCTION foo() RETURNS void AS $$ BEGIN SELECT 1; END; $$ LANGUAGE plpgsql; SELECT 2"
	stmts = store.SplitStatements(sql)
	if len(stmts) != 2 || stmts[0].Text != "CREATE FUNCTION foo() RETURNS void AS $$ BEGIN SELECT 1; END; $$ LANGUAGE plpgsql" {
		t.Errorf("stmts = %+v", stmts)
	}
}

// recordingStore is a fake DataSource whose QueryExecutor records the exact
// buffer/line it receives. It emulates the non-relational engines (Cassandra,
// Neo4j, ...) that treat their input as a single statement and fail on
// multi-statement buffers.
type recordingStore struct {
	buffer string
	line   int
	max    int
}

func (r *recordingStore) Driver() conn.Driver { return conn.DriverCassandra }
func (r *recordingStore) Version(context.Context) (string, error) {
	return "fake", nil
}
func (r *recordingStore) Close() error   { return nil }
func (r *recordingStore) ReadOnly() bool { return true }
func (r *recordingStore) Catalog() store.CatalogDescriptor {
	return store.CatalogDescriptor{Title: "KEYS", ItemNoun: "key"}
}
func (r *recordingStore) Browse(context.Context, store.BrowseRequest) (store.BrowseResponse, error) {
	return store.BrowseResponse{}, nil
}
func (r *recordingStore) Inspect(context.Context, string) (store.InspectionView, error) {
	return &store.KeyValueStructure{Key: "k"}, nil
}
func (r *recordingStore) Query() store.QueryExecutor { return &recordingExec{r: r} }

// recordingExec implements store.QueryExecutor and captures the incoming call.
type recordingExec struct {
	r *recordingStore
}

func (e *recordingExec) Language() string    { return "CQL" }
func (e *recordingExec) PromptTitle() string { return "CQL QUERY" }
func (e *recordingExec) Placeholder() string { return "SELECT ...;" }
func (e *recordingExec) IsMutation(string) bool {
	return false
}
func (e *recordingExec) Execute(_ context.Context, buffer string, line, maxRows int) (store.DataView, error) {
	e.r.buffer = buffer
	e.r.line = line
	e.r.max = maxRows
	return &store.TabularData{Columns: []string{"c"}, Rows: [][]string{{"ok"}}}, nil
}

func TestEditor_ExecuteSendsOnlyStatementAtCursor(t *testing.T) {
	cases := []struct {
		line int
		want string
	}{
		{0, "SELECT 1"},
		{1, "SELECT 2"},
		{2, "SELECT * FROM users"},
		{10, "SELECT * FROM users"}, // past the end: last statement
	}
	for _, tc := range cases {
		ds := &recordingStore{}
		e := New()
		e.Buffer = "SELECT 1;\nSELECT 2;\nSELECT * FROM users"

		if err := e.ExecuteAt(context.Background(), ds, tc.line); err != nil {
			t.Fatalf("ExecuteAt(line %d): %v", tc.line, err)
		}
		if ds.buffer != tc.want {
			t.Errorf("line %d: executor received %q, want %q", tc.line, ds.buffer, tc.want)
		}
		if e.LastQuery != tc.want {
			t.Errorf("line %d: LastQuery = %q, want %q", tc.line, e.LastQuery, tc.want)
		}
		if tab, ok := e.Data.(*store.TabularData); !ok || len(tab.Rows) != 1 {
			t.Errorf("line %d: Data = %+v, want the recorded result", tc.line, e.Data)
		}
	}
}

func TestEditor_ExecuteSendsMultilineStatementWhole(t *testing.T) {
	ds := &recordingStore{}
	e := New()
	stmt := "SELECT *\nFROM users\nWHERE id = 1"
	e.Buffer = stmt

	if err := e.ExecuteAt(context.Background(), ds, 1); err != nil {
		t.Fatalf("ExecuteAt: %v", err)
	}
	if ds.buffer != stmt {
		t.Errorf("executor received %q, want the whole multi-line statement", ds.buffer)
	}
}

func TestEditor_CommentOnlyBufferDoesNotExecute(t *testing.T) {
	ds := &recordingStore{}
	e := New()
	e.Buffer = "-- just a comment\n/* and another */"

	if err := e.ExecuteAt(context.Background(), ds, 0); err != nil {
		t.Fatalf("ExecuteAt: %v", err)
	}
	if ds.buffer != "" {
		t.Errorf("executor received %q, want no call for a comment-only buffer", ds.buffer)
	}
	if e.Data != nil {
		t.Errorf("Data = %+v, want nil", e.Data)
	}
}

type customSplitterStore struct {
	recordingStore
}

func (c *customSplitterStore) Query() store.QueryExecutor {
	return &customSplitterExec{recordingExec: recordingExec{r: &c.recordingStore}}
}

type customSplitterExec struct {
	recordingExec
}

func (c *customSplitterExec) SplitStatements(buffer string) []store.Statement {
	// Simple line-based splitter for testing
	var stmts []store.Statement
	for i, line := range strings.Split(buffer, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			stmts = append(stmts, store.Statement{Text: t, Line: i})
		}
	}
	return stmts
}

func TestEditor_ExecuteUsesStatementSplitterWhenAvailable(t *testing.T) {
	ds := &customSplitterStore{}
	e := New()
	e.Buffer = "HGETALL instancia:101\nSMEMBERS tareas_pendientes:rol:soporte"

	// Cursor on line 0 -> executes HGETALL
	if err := e.ExecuteAt(context.Background(), ds, 0); err != nil {
		t.Fatalf("ExecuteAt(0): %v", err)
	}
	if ds.buffer != "HGETALL instancia:101" {
		t.Errorf("got ds.buffer = %q, want 'HGETALL instancia:101'", ds.buffer)
	}
	if e.LastQuery != "HGETALL instancia:101" {
		t.Errorf("got e.LastQuery = %q, want 'HGETALL instancia:101'", e.LastQuery)
	}

	// Cursor on line 1 -> executes SMEMBERS
	if err := e.ExecuteAt(context.Background(), ds, 1); err != nil {
		t.Fatalf("ExecuteAt(1): %v", err)
	}
	if ds.buffer != "SMEMBERS tareas_pendientes:rol:soporte" {
		t.Errorf("got ds.buffer = %q, want 'SMEMBERS tareas_pendientes:rol:soporte'", ds.buffer)
	}
	if e.LastQuery != "SMEMBERS tareas_pendientes:rol:soporte" {
		t.Errorf("got e.LastQuery = %q, want 'SMEMBERS tareas_pendientes:rol:soporte'", e.LastQuery)
	}
}
