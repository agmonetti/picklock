package store

import (
	"context"
	"github.com/agmonetti/picklock/internal/conn"
	"sort"
	"strings"
)

// RelationalAdapter adapts an internal relational Store to the universal DataSource interface.
type RelationalAdapter struct {
	store Store
	cfg   conn.ConnectionConfig
}

// NewRelationalAdapter wraps a relational Store into a DataSource.
func NewRelationalAdapter(st Store, cfg conn.ConnectionConfig) *RelationalAdapter {
	return &RelationalAdapter{store: st, cfg: cfg}
}

func (a *RelationalAdapter) Driver() conn.Driver {
	return conn.Driver(a.store.Driver())
}

func (a *RelationalAdapter) Version(ctx context.Context) (string, error) {
	return a.store.Version()
}

func (a *RelationalAdapter) Close() error {
	return a.store.Close()
}

func (a *RelationalAdapter) ReadOnly() bool {
	return a.cfg.ReadOnly
}

func (a *RelationalAdapter) Catalog() CatalogDescriptor {
	return CatalogDescriptor{
		Title:    "TABLES",
		ItemNoun: "table",
		ListObjects: func(ctx context.Context) ([]CatalogItem, error) {
			return a.listCatalogItems(ctx)
		},
	}
}

func (a *RelationalAdapter) listCatalogItems(ctx context.Context) ([]CatalogItem, error) {
	tables, err := a.store.Tables()
	if err != nil {
		return nil, err
	}
	fks, err := a.store.ForeignKeysContext(ctx)
	if err != nil {
		// Foreign-key metadata is optional for browsing. A restricted metadata
		// view must not make otherwise valid tables disappear.
		fks = nil
	}
	relationTables, err := classifyRelationTables(ctx, a.store, tables, fks)
	if err != nil {
		return nil, err
	}

	tableItems := make([]CatalogItem, 0, len(tables))
	relationItems := make([]CatalogItem, 0)
	for _, table := range tables {
		if relationTables[table] {
			continue
		}
		tableItems = append(tableItems, CatalogItem{Name: table, Kind: CatalogItemTable, Group: "TABLES"})
	}
	for table := range relationTables {
		relationItems = append(relationItems, CatalogItem{
			Name: table, Kind: CatalogItemRelation, Group: "RELATIONS", Badge: "relation",
			Metadata: relationSummary(table, fks),
		})
	}
	sort.Slice(relationItems, func(i, j int) bool { return relationItems[i].Name < relationItems[j].Name })
	return append(tableItems, relationItems...), nil
}

func (a *RelationalAdapter) Browse(ctx context.Context, req BrowseRequest) (BrowseResponse, error) {
	cols, err := a.store.Columns(req.ObjectName)
	if err != nil {
		return BrowseResponse{}, err
	}

	pkCol, _ := singlePK(cols)
	total, err := a.store.CountTableContext(ctx, req.ObjectName)
	if err != nil {
		total = -1
	}

	if pkCol != "" {
		// Keyset pagination: request PageSize + 1 rows to determine HasNext
		res, err := a.store.SelectTableKeysetPageContext(ctx, req.ObjectName, pkCol, req.PageSize+1, req.Cursor)
		if err != nil {
			return BrowseResponse{}, err
		}
		hasNext := len(res.Rows) > req.PageSize
		rows := res.Rows
		var nulls [][]bool
		if res.Nulls != nil {
			nulls = res.Nulls
		}
		if len(rows) > req.PageSize {
			rows = rows[:req.PageSize]
			if nulls != nil && len(nulls) > req.PageSize {
				nulls = nulls[:req.PageSize]
			}
		}
		nextCursor := ""
		if len(rows) > 0 {
			pkResIdx := -1
			for i, c := range res.Columns {
				if c == pkCol {
					pkResIdx = i
					break
				}
			}
			if pkResIdx >= 0 && pkResIdx < len(rows[len(rows)-1]) {
				nextCursor = rows[len(rows)-1][pkResIdx]
			}
		}
		return BrowseResponse{
			Data: &TabularData{
				Columns:   res.Columns,
				Rows:      rows,
				Nulls:     nulls,
				Affected:  -1,
				Truncated: res.Truncated,
				TotalRows: int64(total),
			},
			HasNext:    hasNext,
			NextCursor: nextCursor,
			TotalCount: int64(total),
		}, nil
	}

	// Fallback to offset pagination
	res, err := a.store.SelectTablePageContext(ctx, req.ObjectName, req.PageSize, req.Page*req.PageSize)
	if err != nil {
		return BrowseResponse{}, err
	}
	hasNext := (req.Page+1)*req.PageSize < total
	return BrowseResponse{
		Data: &TabularData{
			Columns:   res.Columns,
			Rows:      res.Rows,
			Nulls:     res.Nulls,
			Affected:  -1,
			Truncated: res.Truncated,
			TotalRows: int64(total),
		},
		HasNext:    hasNext,
		TotalCount: int64(total),
	}, nil
}

func (a *RelationalAdapter) Inspect(ctx context.Context, objectName string) (InspectionView, error) {
	cols, err := a.store.Columns(objectName)
	if err != nil {
		return nil, err
	}
	indexes, err := a.store.Indexes(objectName)
	if err != nil {
		return nil, err
	}
	fks, err := a.store.ForeignKeysContext(ctx)
	if err != nil {
		fks = nil
	}
	var outgoing, incoming []ForeignKey
	for _, fk := range fks {
		switch {
		case fk.Table == objectName:
			outgoing = append(outgoing, fk)
		case fk.ReferencedTable == objectName:
			incoming = append(incoming, fk)
		}
	}
	relationTables, err := classifyRelationTables(ctx, a.store, []string{objectName}, fks)
	if err != nil {
		return nil, err
	}
	return &RelationalStructure{
		Columns: cols, Indexes: indexes, ForeignKeys: outgoing,
		ReferencedBy: incoming, RelationTable: relationTables[objectName],
	}, nil
}

func classifyRelationTables(ctx context.Context, st Store, tables []string, fks []ForeignKey) (map[string]bool, error) {
	byTable := make(map[string][]ForeignKey)
	for _, fk := range fks {
		byTable[fk.Table] = append(byTable[fk.Table], fk)
	}
	out := make(map[string]bool)
	for _, table := range tables {
		if len(byTable[table]) < 2 {
			continue
		}
		cols, err := st.Columns(table)
		if err != nil {
			return nil, err
		}
		if isPureRelationTable(cols, byTable[table]) {
			out[table] = true
		}
	}
	return out, nil
}

func isPureRelationTable(cols []Column, fks []ForeignKey) bool {
	if len(cols) == 0 || len(fks) < 2 {
		return false
	}
	fkColumns := make(map[string]bool, len(cols))
	for _, fk := range fks {
		for _, column := range fk.Columns {
			fkColumns[column] = true
		}
	}
	if len(fkColumns) != len(cols) {
		return false
	}
	for _, col := range cols {
		if !fkColumns[col.Name] {
			return false
		}
	}
	return true
}

func relationSummary(table string, fks []ForeignKey) string {
	var targets []string
	for _, fk := range fks {
		if fk.Table == table && fk.ReferencedTable != "" {
			targets = append(targets, fk.ReferencedTable)
		}
	}
	sort.Strings(targets)
	return strings.Join(uniqueStrings(targets), " ↔ ")
}

func uniqueStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	out := values[:1]
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}

func (a *RelationalAdapter) Query() QueryExecutor {
	return &RelationalQueryExecutor{store: a.store}
}

// RelationalQueryExecutor executes SQL queries against a relational store.
type RelationalQueryExecutor struct {
	store Store
}

func (e *RelationalQueryExecutor) Language() string {
	return "sql"
}

func (e *RelationalQueryExecutor) PromptTitle() string {
	return "SQL EDITOR"
}

func (e *RelationalQueryExecutor) Placeholder() string {
	return "SELECT * FROM table LIMIT 10"
}

func (e *RelationalQueryExecutor) IsMutation(statement string) bool {
	return IsSQLWrite(statement)
}

func (e *RelationalQueryExecutor) Execute(ctx context.Context, buffer string, line int, maxRows int) (DataView, error) {
	stmts := SplitStatements(buffer)
	if len(stmts) == 0 {
		return nil, nil
	}
	q := stmts[0].Text
	if len(stmts) > 1 {
		q = stmts[StatementAt(stmts, line)].Text
	}

	if ReturnsSQLRows(q) {
		res, err := e.store.QueryContextMax(ctx, q, maxRows)
		if err != nil {
			return nil, err
		}
		return &TabularData{
			Columns:   res.Columns,
			Rows:      res.Rows,
			Nulls:     res.Nulls,
			Affected:  -1,
			Truncated: res.Truncated,
			TotalRows: -1,
		}, nil
	}

	n, err := e.store.ExecContext(ctx, q)
	if err != nil {
		return nil, err
	}
	return &TabularData{
		Affected: n,
	}, nil
}

func singlePK(cols []Column) (string, int) {
	var pks []int
	for i, c := range cols {
		if c.PK {
			pks = append(pks, i)
		}
	}
	if len(pks) == 1 {
		return cols[pks[0]].Name, pks[0]
	}
	return "", -1
}
