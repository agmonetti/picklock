package browser

import (
	"context"
	"fmt"
	"strconv"
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

	execQuery(t, st, `CREATE TABLE users (
		id INTEGER PRIMARY KEY,
		name TEXT,
		email TEXT
	)`)
	execQuery(t, st, "CREATE TABLE orders (id INTEGER PRIMARY KEY)")
	return st
}

func execQuery(t *testing.T, ds store.DataSource, q string) {
	t.Helper()
	if _, err := ds.Query().Execute(context.Background(), q, 0, 100); err != nil {
		t.Fatalf("exec: %v", err)
	}
}

func seedRows(t *testing.T, st store.DataSource, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		sql := fmt.Sprintf(`INSERT INTO users (name, email) VALUES ('u%d', 'u%d@t.com')`, i, i)
		execQuery(t, st, sql)
	}
}

func TestNewLoadsTablesAndSelectsFirst(t *testing.T) {
	st := newTestStore(t)
	b, err := New(context.Background(), st)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if len(b.Tables) != 2 {
		t.Fatalf("Tables = %v, want 2", b.Tables)
	}
	// Alphabetical order: orders < users
	if b.ActiveTable != "orders" {
		t.Errorf("ActiveTable = %q, want orders", b.ActiveTable)
	}
	if len(b.Columns) != 1 {
		t.Errorf("len(Columns) = %d, want 1", len(b.Columns))
	}
}

func TestBrowser_SelectTable_LoadsColumns(t *testing.T) {
	st := newTestStore(t)
	b := &Browser{PageSize: 50}
	if err := b.Load(context.Background(), st); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := b.SelectTable(context.Background(), "users", st); err != nil {
		t.Fatalf("SelectTable: %v", err)
	}
	if len(b.Columns) != 3 {
		t.Errorf("len(Columns) = %d, want 3", len(b.Columns))
	}
	if b.Columns[0].PK != true {
		t.Errorf("col 0 should be PK: %+v", b.Columns[0])
	}
}

func TestBrowser_Pagination(t *testing.T) {
	st := newTestStore(t)
	seedRows(t, st, 120)

	b := &Browser{PageSize: 50}
	if err := b.SelectTable(context.Background(), "users", st); err != nil {
		t.Fatalf("SelectTable: %v", err)
	}

	if b.TotalRows != 120 {
		t.Errorf("TotalRows = %d, want 120", b.TotalRows)
	}
	if len(b.Rows) != 50 {
		t.Errorf("len(Rows) page 0 = %d, want 50", len(b.Rows))
	}
	if !b.HasNextPage() || b.HasPrevPage() {
		t.Error("page 0: HasNextPage=true and HasPrevPage=false expected")
	}

	if err := b.NextPage(context.Background(), st); err != nil {
		t.Fatalf("NextPage: %v", err)
	}
	if len(b.Rows) != 50 || b.Page != 1 {
		t.Errorf("page 1: rows=%d page=%d", len(b.Rows), b.Page)
	}

	if err := b.NextPage(context.Background(), st); err != nil {
		t.Fatalf("NextPage: %v", err)
	}
	if len(b.Rows) != 20 {
		t.Errorf("page 2: rows=%d, want 20", len(b.Rows))
	}
	if b.HasNextPage() {
		t.Error("page 2 (last) should not have a next page")
	}

	if err := b.NextPage(context.Background(), st); err != nil {
		t.Fatalf("NextPage out of range: %v", err)
	}
	if b.Page != 2 {
		t.Errorf("Page = %d, should not advance beyond 2", b.Page)
	}

	if err := b.PrevPage(context.Background(), st); err != nil {
		t.Fatalf("PrevPage: %v", err)
	}
	if b.Page != 1 {
		t.Errorf("Page = %d, want 1", b.Page)
	}
}

func TestBrowser_MoveCursor_Clamps(t *testing.T) {
	st := newTestStore(t)
	seedRows(t, st, 5)

	b := &Browser{PageSize: 50}
	if err := b.SelectTable(context.Background(), "users", st); err != nil {
		t.Fatalf("SelectTable: %v", err)
	}
	b.MoveCursor(10)
	if b.Cursor != 4 {
		t.Errorf("Cursor = %d, want 4 (clamp to max)", b.Cursor)
	}
	b.MoveCursor(-100)
	if b.Cursor != 0 {
		t.Errorf("Cursor = %d, want 0 (clamp to min)", b.Cursor)
	}
}

func TestBrowser_KeysetRefreshIsStable(t *testing.T) {
	st := newTestStore(t)
	seedRows(t, st, 60) // users id 1..60

	b := &Browser{PageSize: 50}
	if err := b.SelectTable(context.Background(), "users", st); err != nil {
		t.Fatalf("SelectTable: %v", err)
	}
	if len(b.Rows) != 50 || b.Rows[0][1] != "u0" {
		t.Fatalf("page 0 = %d rows, first %v", len(b.Rows), b.Rows[0])
	}

	// a row inserted before the last row of the page must not shift the page
	execQuery(t, st, "INSERT INTO users (name, email) VALUES ('u0b','u0b@t.com')")
	if err := b.Refresh(context.Background(), st); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if b.Rows[0][1] != "u0" {
		t.Errorf("first row after refresh = %v, want u0 (stable)", b.Rows[0])
	}

	// next page and back
	if !b.HasNextPage() {
		t.Fatal("page 0 should have a next page")
	}
	page0Last := b.Rows[len(b.Rows)-1][0]
	if err := b.NextPage(context.Background(), st); err != nil {
		t.Fatalf("NextPage: %v", err)
	}
	if b.Page != 1 || len(b.Rows) == 0 {
		t.Fatalf("page 1 = %d rows, want > 0", len(b.Rows))
	}
	if err := b.PrevPage(context.Background(), st); err != nil {
		t.Fatalf("PrevPage: %v", err)
	}
	if b.Page != 0 || len(b.Rows) != 50 {
		t.Errorf("back to page 0 = %d rows, want 50", len(b.Rows))
	}
	if b.Rows[len(b.Rows)-1][0] != page0Last {
		t.Errorf("last row of page 0 = %s, want %s (same page back)", b.Rows[len(b.Rows)-1][0], page0Last)
	}
}

func TestBrowser_KeysetFallbackWithoutPK(t *testing.T) {
	st := newTestStore(t)
	execQuery(t, st, "CREATE TABLE nopk (name TEXT, email TEXT)")
	execQuery(t, st, "INSERT INTO nopk (name, email) VALUES ('a','a@t.com'), ('b','b@t.com')")

	b := &Browser{PageSize: 50}
	if err := b.SelectTable(context.Background(), "nopk", st); err != nil {
		t.Fatalf("SelectTable: %v", err)
	}
	if len(b.Rows) != 2 {
		t.Errorf("Rows = %d, want 2", len(b.Rows))
	}
}

func TestBrowser_EmptyDatabase(t *testing.T) {
	cfg := conn.New(conn.DriverSQLite)
	cfg.Path = ":memory:"
	st, err := store.New(cfg)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer st.Close()

	b, err := New(context.Background(), st)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if len(b.Tables) != 0 {
		t.Errorf("Tables = %v, want 0", b.Tables)
	}
	if b.ActiveTable != "" {
		t.Errorf("ActiveTable = %q, want empty", b.ActiveTable)
	}
}

func TestBrowser_NullsFollowRowsAndClone(t *testing.T) {
	st := newTestStore(t)
	execQuery(t, st, "INSERT INTO users (name, email) VALUES (NULL, 'a@t.com'), ('x', 'b@t.com')")

	b := &Browser{PageSize: 50}
	if err := b.SelectTable(context.Background(), "users", st); err != nil {
		t.Fatalf("SelectTable: %v", err)
	}
	if len(b.Rows) != 2 || len(b.Nulls) != 2 {
		t.Fatalf("rows/nulls = %d/%d, want 2/2", len(b.Rows), len(b.Nulls))
	}
	if got := b.Nulls[0][1]; !got {
		t.Errorf("row 0 (NULL name) marked %v, want true", got)
	}
	if got := b.Nulls[1][1]; got {
		t.Errorf("row 1 ('x') marked %v, want false", got)
	}

	c := b.Clone()
	if len(c.Nulls) != len(b.Nulls) {
		t.Fatalf("clone nulls = %d, want %d", len(c.Nulls), len(b.Nulls))
	}
	if c.Nulls[0][1] != true || c.Nulls[1][1] != false {
		t.Errorf("clone nulls = %v, want [{f t f} {f f f}]", c.Nulls)
	}
}

func TestBrowser_ReloadPicksUpNewTableAndRows(t *testing.T) {
	st := newTestStore(t)
	b, err := New(context.Background(), st)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if b.ActiveTable != "orders" {
		t.Fatalf("setup: ActiveTable = %q", b.ActiveTable)
	}

	execQuery(t, st, "CREATE TABLE products (id INTEGER PRIMARY KEY)")
	execQuery(t, st, "INSERT INTO orders (id) VALUES (1)")

	if err := b.Reload(context.Background(), st); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if len(b.Tables) != 3 {
		t.Errorf("Tables = %v, want 3 (orders, products, users)", b.Tables)
	}
	if b.ActiveTable != "orders" {
		t.Errorf("ActiveTable = %q, want orders", b.ActiveTable)
	}
	if b.TotalRows != 1 {
		t.Errorf("TotalRows = %d, want 1", b.TotalRows)
	}
}

// cursorSource emulates a cursor-paged engine (Redis hash/set/zset SCAN
// cursors, Cassandra page states) where the engine controls the next-cursor
// and offset-based paging would repeat data. It exposes 130 items that only
// advance when the previous response's cursor is sent back.
type cursorSource struct {
	items []store.CatalogItem
	name  string
}

func newCursorSource(n int) *cursorSource {
	items := make([]store.CatalogItem, n)
	for i := range items {
		items[i] = store.CatalogItem{Name: fmt.Sprintf("k%d", i)}
	}
	return &cursorSource{items: items, name: "cur"}
}

func (f *cursorSource) Driver() conn.Driver { return conn.DriverRedis }
func (f *cursorSource) Version(context.Context) (string, error) {
	return "fake", nil
}
func (f *cursorSource) Close() error   { return nil }
func (f *cursorSource) ReadOnly() bool { return true }
func (f *cursorSource) Catalog() store.CatalogDescriptor {
	return store.CatalogDescriptor{
		Title:    "KEYS",
		ItemNoun: "key",
		ListObjects: func(ctx context.Context) ([]store.CatalogItem, error) {
			return f.items, nil
		},
	}
}
func (f *cursorSource) Inspect(context.Context, string) (store.InspectionView, error) {
	return &store.KeyValueStructure{Key: f.name}, nil
}
func (f *cursorSource) Query() store.QueryExecutor { return nil }

func (f *cursorSource) Browse(_ context.Context, req store.BrowseRequest) (store.BrowseResponse, error) {
	start := 0
	if req.Cursor != "" {
		var err error
		start, err = strconv.Atoi(req.Cursor)
		if err != nil {
			start = 0 // a non-integer cursor must NOT silently replay page 0
		}
	}
	total := len(f.items)
	end := start + req.PageSize
	if end > total {
		end = total
	}
	rows := make([][]string, 0, end-start)
	for i := start; i < end; i++ {
		rows = append(rows, []string{f.items[i].Name})
	}
	next := ""
	if end < total {
		next = strconv.Itoa(end)
	}
	return store.BrowseResponse{
		Data: &store.TabularData{
			Columns:   []string{"key"},
			Rows:      rows,
			Affected:  -1,
			TotalRows: int64(total),
		},
		HasNext:    end < total,
		NextCursor: next,
		TotalCount: int64(total),
	}, nil
}

func TestBrowser_CursorPagination(t *testing.T) {
	ds := newCursorSource(130)
	b := &Browser{PageSize: 50, cur: []string{""}, next: []string{""}}
	if err := b.SelectItem(context.Background(), "cur", ds); err != nil {
		t.Fatalf("SelectItem: %v", err)
	}
	firstOf := func() string { return b.Rows[0][0] }
	lastOf := func() string { return b.Rows[len(b.Rows)-1][0] }

	if got, want := len(b.Rows), 50; got != want {
		t.Fatalf("page 0 rows = %d, want %d", got, want)
	}
	if firstOf() != "k0" || lastOf() != "k49" {
		t.Fatalf("page 0 range = %s..%s, want k0..k49", firstOf(), lastOf())
	}

	if err := b.NextPage(context.Background(), ds); err != nil {
		t.Fatalf("NextPage: %v", err)
	}
	if b.Page != 1 || firstOf() != "k50" || lastOf() != "k99" {
		t.Fatalf("page 1 = %d rows %s..%s, want k50..k99", len(b.Rows), firstOf(), lastOf())
	}

	// Refresh must stay on page 1 (the recorded cursor, not a recomputed PK).
	if err := b.Refresh(context.Background(), ds); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if firstOf() != "k50" {
		t.Errorf("after Refresh page 1 starts at %s, want k50 (stable)", firstOf())
	}

	if err := b.NextPage(context.Background(), ds); err != nil {
		t.Fatalf("NextPage: %v", err)
	}
	if b.Page != 2 || len(b.Rows) != 30 || firstOf() != "k100" {
		t.Fatalf("page 2 = %d rows starting at %s, want 30 from k100", len(b.Rows), firstOf())
	}
	if b.HasNextPage() {
		t.Error("page 2 (last) should not have a next page")
	}
	if err := b.NextPage(context.Background(), ds); err != nil {
		t.Fatalf("NextPage out of range: %v", err)
	}
	if b.Page != 2 {
		t.Errorf("Page = %d, should not advance beyond 2", b.Page)
	}

	if err := b.PrevPage(context.Background(), ds); err != nil {
		t.Fatalf("PrevPage: %v", err)
	}
	if b.Page != 1 || firstOf() != "k50" {
		t.Errorf("PrevPage -> page %d starting at %s, want 1/k50", b.Page, firstOf())
	}
	if err := b.PrevPage(context.Background(), ds); err != nil {
		t.Fatalf("PrevPage: %v", err)
	}
	if b.Page != 0 || firstOf() != "k0" {
		t.Errorf("PrevPage -> page %d starting at %s, want 0/k0", b.Page, firstOf())
	}
}

func TestBrowser_CloneCopiesCursorStacks(t *testing.T) {
	ds := newCursorSource(130)
	b := &Browser{PageSize: 50, cur: []string{""}, next: []string{""}}
	if err := b.SelectItem(context.Background(), "cur", ds); err != nil {
		t.Fatalf("SelectItem: %v", err)
	}
	// record the next-cursor of page 0 as SelectItem already does
	c := b.Clone()
	if len(c.cur) != len(b.cur) || len(c.next) != len(b.next) {
		t.Fatalf("clone cursor stacks (cur=%d next=%d) != source (cur=%d next=%d)",
			len(c.cur), len(c.next), len(b.cur), len(b.next))
	}
	for i := range b.next {
		if c.next[i] != b.next[i] {
			t.Errorf("clone next[%d] = %q, want %q", i, c.next[i], b.next[i])
		}
	}
}
