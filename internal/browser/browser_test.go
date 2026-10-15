package browser

import (
	"context"
	"fmt"
	"github.com/agmonetti/picklock/internal/conn"
	"github.com/agmonetti/picklock/internal/store"
	_ "github.com/agmonetti/picklock/internal/store/sqlite"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
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
	if _, err := ds.Query().Execute(context.Background(), q, 0, store.DefaultQueryLimits()); err != nil {
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

func TestNewLoadsCatalogWithoutFetching(t *testing.T) {
	st := newTestStore(t)
	b, err := New(context.Background(), st)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if len(b.Tables) != 2 {
		t.Fatalf("Tables = %v, want 2", b.Tables)
	}
	if b.ActiveTable != "" || b.Data != nil || b.Structure != nil {
		t.Fatalf("catalog load fetched or selected data: %+v", b)
	}
}

func TestBrowser_SelectItem_LoadsColumns(t *testing.T) {
	st := newTestStore(t)
	b := &Browser{PageSize: 50}
	if err := b.Load(context.Background(), st); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := b.SelectItem(context.Background(), "users", st); err != nil {
		t.Fatalf("SelectItem: %v", err)
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
	if err := b.SelectItem(context.Background(), "users", st); err != nil {
		t.Fatalf("SelectItem: %v", err)
	}

	if b.TotalRows != -1 {
		t.Errorf("TotalRows = %d, want unknown (-1)", b.TotalRows)
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
	if err := b.SelectItem(context.Background(), "users", st); err != nil {
		t.Fatalf("SelectItem: %v", err)
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
	if err := b.SelectItem(context.Background(), "users", st); err != nil {
		t.Fatalf("SelectItem: %v", err)
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
	if err := b.SelectItem(context.Background(), "nopk", st); err != nil {
		t.Fatalf("SelectItem: %v", err)
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
	if err := b.SelectItem(context.Background(), "users", st); err != nil {
		t.Fatalf("SelectItem: %v", err)
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
	if err := b.SelectItem(context.Background(), "orders", st); err != nil {
		t.Fatalf("SelectItem: %v", err)
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
	if b.TotalRows != -1 {
		t.Errorf("TotalRows = %d, want unknown (-1)", b.TotalRows)
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

func TestBrowser_CloneNavigationDoesNotMovePublishedPage(t *testing.T) {
	ds := newCursorSource(130)
	b := &Browser{PageSize: 50}
	if err := b.SelectItem(context.Background(), "cur", ds); err != nil {
		t.Fatal(err)
	}
	c := b.Clone()
	if c.Data != b.Data {
		t.Fatal("clone duplicated the immutable published page")
	}
	published := b.Data
	if err := c.NextPage(context.Background(), ds); err != nil {
		t.Fatal(err)
	}
	if b.Data != published || c.Data == published {
		t.Fatal("background page replacement mutated or retained the published page")
	}
	if b.Page != 0 || b.Rows[0][0] != "k0" || c.Page != 1 || c.Rows[0][0] != "k50" {
		t.Fatalf("background navigation changed published page: original=%d/%v clone=%d/%v", b.Page, b.Rows[0], c.Page, c.Rows[0])
	}
}

func TestBrowser_PrepareDoesNotReplaceActivePage(t *testing.T) {
	ds := newTestStore(t)
	seedRows(t, ds, 3)
	b := &Browser{PageSize: 2}
	ctx := context.Background()
	if err := b.SelectItem(ctx, "users", ds); err != nil {
		t.Fatal(err)
	}
	oldData := b.Data
	prepared, err := b.PrepareItem(ctx, "orders", ds)
	if err != nil {
		t.Fatal(err)
	}
	if b.Data != oldData || b.ActiveTable != "users" || b.Rows[0][0] != "1" {
		t.Fatal("inspection changed the visible page before explicit loading")
	}
	if err := b.OpenPrepared(ctx, prepared, store.BrowseOptions{Columns: []string{"missing"}}, 1, ds); err == nil {
		t.Fatal("unknown first-page column was accepted")
	}
	if b.Data != oldData || b.ActiveTable != "users" {
		t.Fatal("rejected first-page selection replaced active data")
	}
	if err := b.OpenPrepared(ctx, prepared, store.BrowseOptions{}, b.PageSize, ds); err != nil {
		t.Fatal(err)
	}
	if b.ActiveTable != "orders" || len(b.Rows) != 0 || len(b.Columns) != 1 {
		t.Fatalf("explicit first load did not select the inspected table: %+v", b)
	}
}

func TestBrowser_ChangedPageSizeDiscardsOldCursorRange(t *testing.T) {
	ctx := context.Background()
	ds := newTestStore(t)
	seedRows(t, ds, 8)
	b := &Browser{PageSize: 3}
	if err := b.SelectItem(ctx, "users", ds); err != nil {
		t.Fatal(err)
	}
	if err := b.NextPage(ctx, ds); err != nil {
		t.Fatal(err)
	}
	if b.Rows[0][0] != "4" {
		t.Fatalf("setup page: %v", b.Rows)
	}
	if err := b.SetOptions(ctx, store.BrowseOptions{}, 2, ds); err != nil {
		t.Fatal(err)
	}
	if b.Page != 0 || b.Cursor != 0 || b.HasPrevPage() || len(b.Rows) != 2 || b.Rows[0][0] != "1" {
		t.Fatalf("selection reused old page: page=%d rows=%v", b.Page, b.Rows)
	}
	if err := b.NextPage(ctx, ds); err != nil {
		t.Fatal(err)
	}
	if b.Rows[0][0] != "3" {
		t.Fatalf("old keyset boundary reused: %v", b.Rows)
	}
	if err := b.PrevPage(ctx, ds); err != nil {
		t.Fatal(err)
	}
	if b.Rows[0][0] != "1" || len(b.Rows) != 2 {
		t.Fatalf("back navigation reused old selection: %v", b.Rows)
	}
	if err := b.SetOptions(ctx, store.BrowseOptions{Columns: []string{"name"}}, 4, ds); err != nil {
		t.Fatalf("select projected column: %v", err)
	}
	projected, ok := b.Data.(*store.TabularData)
	if !ok || len(projected.Columns) != 1 || projected.Columns[0] != "name" || len(projected.Rows) != 4 || projected.Rows[0][0] != "u0" || !b.HasNextPage() {
		t.Fatalf("projected result = %#v, want four projected rows with a next page", b.Data)
	}
	filter := store.BrowseOptions{Filters: []store.BrowseFilter{{Column: "id", Operator: store.FilterEqual, Value: store.TypedValue{Value: int64(1)}}}}
	if err := b.SetOptions(ctx, filter, 4, ds); err != nil {
		t.Fatalf("apply structured filter: %v", err)
	}
	filtered, ok := b.Data.(*store.TabularData)
	if !ok || len(filtered.Rows) != 1 || filtered.Rows[0][0] != "1" {
		t.Fatalf("filtered result = %#v, want only id 1", b.Data)
	}
	injection := store.BrowseOptions{Filters: []store.BrowseFilter{{Column: "name", Operator: store.FilterEqual, Value: store.TypedValue{Value: "u0' OR 1=1 --"}}}}
	if err := b.SetOptions(ctx, injection, 4, ds); err != nil {
		t.Fatalf("apply parameterized string filter: %v", err)
	}
	injected, ok := b.Data.(*store.TabularData)
	if !ok || len(injected.Rows) != 0 {
		t.Fatalf("parameterized filter returned rows: %#v", b.Data)
	}
	if err := b.SetOptions(ctx, store.BrowseOptions{PreviewBytes: 256}, 4, ds); err != nil {
		t.Fatalf("apply bounded preview: %v", err)
	}
	preview, ok := b.Data.(*store.TabularData)
	if !ok || len(preview.Rows) != 4 || b.Options.PreviewBytes != 256 || b.PageSize != 4 || b.Page != 0 {
		t.Fatalf("bounded preview options were not applied: data=%#v options=%+v pageSize=%d page=%d", b.Data, b.Options, b.PageSize, b.Page)
	}
}

type failingInspection struct{ store.DataSource }

func (f failingInspection) Inspect(context.Context, string) (store.InspectionView, error) {
	return nil, fmt.Errorf("metadata permission denied")
}

func TestBrowser_MetadataFailurePreservesSelection(t *testing.T) {
	ctx := context.Background()
	ds := newTestStore(t)
	seedRows(t, ds, 2)
	b := &Browser{PageSize: 1}
	if err := b.SelectItem(ctx, "users", ds); err != nil {
		t.Fatal(err)
	}
	if err := b.NextPage(ctx, ds); err != nil {
		t.Fatal(err)
	}
	oldData, oldStructure := b.Data, b.Structure
	if err := b.SelectItem(ctx, "orders", failingInspection{ds}); err == nil {
		t.Fatal("metadata failure was ignored")
	}
	if b.ActiveTable != "users" || b.Page != 1 || b.Data != oldData || b.Structure != oldStructure || b.Rows[0][0] != "2" {
		t.Fatal("metadata failure discarded the previous selection")
	}
}

func TestSQLiteBrowseProjectionOmitsLargePayload(t *testing.T) {
	ds := newTestStore(t)
	execQuery(t, ds, "CREATE TABLE heavy (id INTEGER PRIMARY KEY, name TEXT NOT NULL, payload BLOB)")
	execQuery(t, ds, "INSERT INTO heavy (name, payload) VALUES ('Alice', zeroblob(1048576)), ('Bob', zeroblob(1048576))")

	response, err := ds.Browse(context.Background(), store.BrowseRequest{
		ObjectName: "heavy",
		PageSize:   10,
		Options: store.BrowseOptions{
			Columns: []string{"name"},
			Filters: []store.BrowseFilter{{Column: "id", Operator: store.FilterGreater, Value: store.TypedValue{Value: int64(1)}}},
		},
	})
	if err != nil {
		t.Fatalf("Browse projection/filter: %v", err)
	}
	data, ok := response.Data.(*store.TabularData)
	if !ok || len(data.Columns) != 1 || data.Columns[0] != "name" || len(data.Rows) != 1 || data.Rows[0][0] != "Bob" {
		t.Fatalf("Browse projection/filter = %#v, want Bob only", response.Data)
	}
	retainedBytes := len(data.Rows[0][0])
	if retainedBytes != len("Bob") {
		t.Fatalf("projected result retained %d bytes, want 3", retainedBytes)
	}
	ref := data.RowRef(0)
	if ref == nil || ref.ObjectName() != "heavy" || len(ref.KeyParts()) != 1 || ref.KeyParts()[0].Column != "id" || ref.KeyParts()[0].Value.Value != int64(2) {
		t.Fatalf("hidden identity reference = %#v, want native id 2", ref)
	}
	t.Logf("projection returned %d payload bytes; omitted 2 MiB BLOB columns", retainedBytes)
}

func TestSQLiteBrowsePreservesPlaceholderLikeUserValue(t *testing.T) {
	ds := newTestStore(t)
	execQuery(t, ds, "CREATE TABLE marker (id INTEGER PRIMARY KEY, value TEXT)")
	execQuery(t, ds, "INSERT INTO marker (value) VALUES ('<deferred>')")

	response, err := ds.Browse(context.Background(), store.BrowseRequest{
		ObjectName: "marker",
		PageSize:   1,
	})
	if err != nil {
		t.Fatalf("Browse marker: %v", err)
	}
	data, ok := response.Data.(*store.TabularData)
	if !ok || len(data.Rows) != 1 || data.Rows[0][1] != "<deferred>" || len(data.Incomplete) != 0 {
		t.Fatalf("Browse marker = %#v, want complete literal value", response.Data)
	}
}

func TestSQLiteDefaultBrowseBoundsDynamicTextAndBinaryValues(t *testing.T) {
	ds := newTestStore(t)
	execQuery(t, ds, "CREATE TABLE mixed (id INTEGER PRIMARY KEY, payload)")
	execQuery(t, ds, "INSERT INTO mixed VALUES (1, 'é界x'), (2, X'00FF010203'), (3, NULL), (4, ''), (5, 42)")

	response, err := ds.Browse(context.Background(), store.BrowseRequest{
		ObjectName: "mixed",
		PageSize:   10,
		Options:    store.BrowseOptions{PreviewBytes: 4},
	})
	if err != nil {
		t.Fatalf("Browse dynamic values: %v", err)
	}
	data, ok := response.Data.(*store.TabularData)
	if !ok || len(data.Rows) != 5 || response.HasNext {
		t.Fatalf("Browse dynamic values = %#v hasNext=%v", response.Data, response.HasNext)
	}
	if data.Rows[0][1] != "é" || !utf8.ValidString(data.Rows[0][1]) {
		t.Fatalf("UTF-8 preview = %q, want a valid prefix within 4 bytes", data.Rows[0][1])
	}
	if data.Rows[1][1] != "0x00ff0102" {
		t.Fatalf("binary preview = %q, want first 4 bytes as hex", data.Rows[1][1])
	}
	if !data.Nulls[2][1] || data.Nulls[3][1] || data.Rows[3][1] != "" || data.Rows[4][1] != "42" {
		t.Fatalf("dynamic NULL/empty/number values lost: rows=%v nulls=%v", data.Rows, data.Nulls)
	}
	for _, position := range []store.CellPosition{{Row: 0, Column: 1}, {Row: 1, Column: 1}} {
		if _, ok := data.Incomplete[position]; !ok {
			t.Errorf("preview metadata missing at %+v", position)
		}
	}
	if got := data.Incomplete[store.CellPosition{Row: 0, Column: 1}].Type; got != "TEXT" {
		t.Errorf("text preview type = %q, want TEXT", got)
	}
	if got := data.Incomplete[store.CellPosition{Row: 1, Column: 1}].Type; got != "BLOB" {
		t.Errorf("binary preview type = %q, want BLOB", got)
	}
}

func TestSQLiteDefaultBrowsePreviewsLargeBlob(t *testing.T) {
	ds := newTestStore(t)
	execQuery(t, ds, "CREATE TABLE heavy_default (id INTEGER PRIMARY KEY, payload BLOB)")
	execQuery(t, ds, "INSERT INTO heavy_default VALUES (1, zeroblob(1048576)), (2, zeroblob(1048576))")
	response, err := ds.Browse(context.Background(), store.BrowseRequest{ObjectName: "heavy_default", PageSize: 10})
	if err != nil {
		t.Fatalf("Browse large blob: %v", err)
	}
	data := response.Data.(*store.TabularData)
	if len(data.Rows) != 2 || len(data.Rows[0][1]) != 2+2*store.DefaultBrowsePreviewBytes ||
		data.Incomplete[store.CellPosition{Row: 0, Column: 1}].State != store.CellPreview ||
		data.Incomplete[store.CellPosition{Row: 1, Column: 1}].State != store.CellPreview {
		t.Fatalf("large BLOB browse retained unbounded values: rows=%v incomplete=%v", data.Rows, data.Incomplete)
	}
}
func TestSQLiteDefaultBrowsePreviewsLargeJSONText(t *testing.T) {
	ds := newTestStore(t)
	execQuery(t, ds, "CREATE TABLE heavy_json (id INTEGER PRIMARY KEY, document JSON)")
	execQuery(t, ds, "INSERT INTO heavy_json VALUES (1, '{\"v\":\"' || replace(hex(zeroblob(524288)), '00', 'xx') || '\"}')")

	response, err := ds.Browse(context.Background(), store.BrowseRequest{ObjectName: "heavy_json", PageSize: 10})
	if err != nil {
		t.Fatalf("Browse large JSON text: %v", err)
	}
	data := response.Data.(*store.TabularData)
	position := store.CellPosition{Row: 0, Column: 1}
	if len(data.Rows) != 1 || len(data.Rows[0][1]) != store.DefaultBrowsePreviewBytes ||
		!strings.HasPrefix(data.Rows[0][1], "{\"v\":\"") ||
		data.Incomplete[position].State != store.CellPreview || data.Incomplete[position].Type != "TEXT" {
		t.Fatalf("large JSON Browse retained unbounded/invalid preview: rows=%v incomplete=%v", data.Rows, data.Incomplete)
	}
}

func TestSQLiteBrowseKeysetUsesBinaryIdentityBytes(t *testing.T) {
	ds := newTestStore(t)
	execQuery(t, ds, "CREATE TABLE binary_keys (id BLOB NOT NULL PRIMARY KEY, payload TEXT)")
	execQuery(t, ds, "INSERT INTO binary_keys VALUES (X'6161', 'first'), (X'6262', 'second')")

	b := &Browser{PageSize: 1}
	if err := b.SelectItem(context.Background(), "binary_keys", ds); err != nil {
		t.Fatalf("SelectItem: %v", err)
	}
	if err := b.NextPage(context.Background(), ds); err != nil {
		t.Fatalf("NextPage: %v", err)
	}
	if b.Page != 1 || len(b.Rows) != 1 || b.Rows[0][1] != "second" {
		t.Fatalf("binary-key second page = page %d rows %v, want second row", b.Page, b.Rows)
	}
}
