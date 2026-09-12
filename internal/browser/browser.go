// Package browser manages the state for navigating catalog items and data views,
// independent of the TUI and the engine.
package browser

import (
	"context"

	"github.com/agmonetti/picklock/internal/store"
)

// PageSizeDefault is the number of items per page.
const PageSizeDefault = 50

// Browser keeps the navigation state of the active data source.
type Browser struct {
	// Catalog state
	CatalogTitle string
	ItemNoun     string
	Items        []store.CatalogItem
	Tables       []string // Names of catalog items (convenience & backwards compat)
	ActiveTable  string   // Name of selected item

	// Active data and inspection views
	Data      store.DataView
	Structure store.InspectionView

	// Relational / Tabular convenience fields
	Columns []store.Column
	Indexes []store.Index
	Rows    [][]string
	Nulls   [][]bool

	// Pagination & cursor state
	Page      int
	PageSize  int
	TotalRows int
	Cursor    int // Row/Item cursor within visible page

	// Cursor stack for backward pagination across keyset/cursor engines.
	// cur[p] is the cursor used to REQUEST page p (cur[0] = ""); next[p] is the
	// cursor returned by the engine after fetching page p, i.e. the cursor that
	// yields page p+1 ("" if unknown / engine ignores cursors).
	cur     []string
	next    []string
	hasNext bool
}

// New loads the catalog of the data source and selects the first item.
func New(ctx context.Context, ds store.DataSource) (*Browser, error) {
	b := &Browser{
		PageSize: PageSizeDefault,
		cur:      []string{""},
		next:     []string{""},
	}
	if err := b.Load(ctx, ds); err != nil {
		return nil, err
	}
	if len(b.Items) > 0 {
		if err := b.SelectItem(ctx, b.Items[0].Name, ds); err != nil {
			return nil, err
		}
	}
	return b, nil
}

// Load reloads the list of catalog items.
func (b *Browser) Load(ctx context.Context, ds store.DataSource) error {
	cat := ds.Catalog()
	b.CatalogTitle = cat.Title
	b.ItemNoun = cat.ItemNoun

	items, err := cat.ListObjects(ctx)
	if err != nil {
		return err
	}
	b.Items = items
	tables := make([]string, len(items))
	for i, it := range items {
		tables[i] = it.Name
	}
	b.Tables = tables
	return nil
}

// SelectTable switches the active item (alias for SelectItem).
func (b *Browser) SelectTable(ctx context.Context, name string, ds store.DataSource) error {
	return b.SelectItem(ctx, name, ds)
}

// SelectItem switches the active item, resets pagination and loads its data.
func (b *Browser) SelectItem(ctx context.Context, name string, ds store.DataSource) error {
	b.ActiveTable = name
	b.Page = 0
	b.Cursor = 0
	b.cur = []string{""}
	b.next = []string{""}
	b.hasNext = false

	// Inspect structure
	insp, err := ds.Inspect(ctx, name)
	if err == nil {
		b.Structure = insp
		if rel, ok := insp.(*store.RelationalStructure); ok {
			b.Columns = rel.Columns
			b.Indexes = rel.Indexes
		} else {
			b.Columns = nil
			b.Indexes = nil
		}
	}

	return b.fetchPage(ctx, ds)
}

// Refresh reloads the current page of the active item without re-listing the catalog.
func (b *Browser) Refresh(ctx context.Context, ds store.DataSource) error {
	if b.ActiveTable == "" {
		return nil
	}
	return b.fetchPage(ctx, ds)
}

func (b *Browser) fetchPage(ctx context.Context, ds store.DataSource) error {
	cursor := ""
	if b.Page < len(b.cur) {
		cursor = b.cur[b.Page]
	}

	resp, err := ds.Browse(ctx, store.BrowseRequest{
		ObjectName: b.ActiveTable,
		PageSize:   b.PageSize,
		Page:       b.Page,
		Cursor:     cursor,
	})
	if err != nil {
		return err
	}

	b.Data = resp.Data
	b.hasNext = resp.HasNext
	b.TotalRows = int(resp.TotalCount)

	// Record the cursor that yields the next page, if the engine provided one.
	for len(b.next) <= b.Page {
		b.next = append(b.next, "")
	}
	b.next[b.Page] = resp.NextCursor

	// Populate convenience tabular fields if data is TabularData
	if tab, ok := resp.Data.(*store.TabularData); ok {
		b.Rows = tab.Rows
		b.Nulls = tab.Nulls
		if b.TotalRows < 0 && tab.TotalRows >= 0 {
			b.TotalRows = int(tab.TotalRows)
		}
		if len(b.Columns) == 0 && len(tab.Columns) > 0 {
			cols := make([]store.Column, len(tab.Columns))
			for i, c := range tab.Columns {
				cols[i] = store.Column{Name: c}
			}
			b.Columns = cols
		}
	} else {
		b.Rows = nil
		b.Nulls = nil
	}

	b.clampCursor()
	return nil
}

// Reload reloads the catalog and the active item data.
func (b *Browser) Reload(ctx context.Context, ds store.DataSource) error {
	if err := b.Load(ctx, ds); err != nil {
		return err
	}
	if b.ActiveTable == "" || !hasString(b.Tables, b.ActiveTable) {
		if len(b.Tables) > 0 {
			return b.SelectItem(ctx, b.Tables[0], ds)
		}
		b.ActiveTable = ""
		b.Columns = nil
		b.Indexes = nil
		b.Rows = nil
		b.Nulls = nil
		b.Data = nil
		b.Structure = nil
		b.TotalRows = 0
		b.Page = 0
		b.Cursor = 0
		b.cur = []string{""}
		b.next = []string{""}
		b.hasNext = false
		return nil
	}

	// Re-inspect structure
	insp, err := ds.Inspect(ctx, b.ActiveTable)
	if err == nil {
		b.Structure = insp
		if rel, ok := insp.(*store.RelationalStructure); ok {
			b.Columns = rel.Columns
			b.Indexes = rel.Indexes
		}
	}
	// Reset pagination to avoid landing on an empty page after a mutation
	// (e.g. DELETE reduced total rows). Keep stack consistent for cursor
	// engines by clearing cur/next.
	b.Page = 0
	b.Cursor = 0
	b.cur = []string{""}
	b.next = []string{""}
	b.hasNext = false
	return b.fetchPage(ctx, ds)
}

func hasString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// NextPage advances to the next page if it exists.
func (b *Browser) NextPage(ctx context.Context, ds store.DataSource) error {
	if !b.HasNextPage() {
		return nil
	}
	// The cursor that yields page p+1 was recorded when page p was fetched.
	nextCursor := ""
	if b.Page < len(b.next) {
		nextCursor = b.next[b.Page]
	}

	for len(b.cur) <= b.Page+1 {
		b.cur = append(b.cur, "")
	}
	b.cur[b.Page+1] = nextCursor
	b.Page++
	b.Cursor = 0
	return b.fetchPage(ctx, ds)
}

// PrevPage goes back to the previous page if it exists.
func (b *Browser) PrevPage(ctx context.Context, ds store.DataSource) error {
	if !b.HasPrevPage() {
		return nil
	}
	b.Page--
	b.Cursor = 0
	return b.fetchPage(ctx, ds)
}

// MoveCursor moves the selected item within the visible page.
func (b *Browser) MoveCursor(delta int) {
	b.Cursor += delta
	b.clampCursor()
}

// HasNextPage reports whether there are more pages after the current one.
func (b *Browser) HasNextPage() bool {
	if b.hasNext {
		return true
	}
	if b.TotalRows > 0 {
		return (b.Page+1)*b.PageSize < b.TotalRows
	}
	return false
}

// HasPrevPage reports whether there are pages before the current one.
func (b *Browser) HasPrevPage() bool {
	return b.Page > 0
}

func (b *Browser) itemCount() int {
	if b.Data != nil {
		switch v := b.Data.(type) {
		case *store.TabularData:
			return len(v.Rows)
		case *store.DocumentData:
			return len(v.Documents)
		case *store.KeyValueData:
			return len(v.Entries)
		case *store.GraphData:
			return len(v.Nodes)
		}
	}
	return len(b.Rows)
}

func (b *Browser) clampCursor() {
	if b.Cursor < 0 {
		b.Cursor = 0
	}
	max := b.itemCount() - 1
	if max < 0 {
		max = 0
	}
	if b.Cursor > max {
		b.Cursor = max
	}
}

// Clone returns a deep copy of the browser.
func (b *Browser) Clone() *Browser {
	c := *b
	c.Items = append([]store.CatalogItem(nil), b.Items...)
	c.Tables = append([]string(nil), b.Tables...)
	c.Columns = append([]store.Column(nil), b.Columns...)
	c.Indexes = append([]store.Index(nil), b.Indexes...)
	c.Rows = make([][]string, len(b.Rows))
	for i, r := range b.Rows {
		c.Rows[i] = append([]string(nil), r...)
	}
	c.Nulls = make([][]bool, len(b.Nulls))
	for i, r := range b.Nulls {
		c.Nulls[i] = append([]bool(nil), r...)
	}
	c.cur = append([]string(nil), b.cur...)
	c.next = append([]string(nil), b.next...)
	return &c
}
