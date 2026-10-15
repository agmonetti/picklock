package store

import (
	"context"
	"encoding/hex"
	"fmt"
	"github.com/agmonetti/picklock/internal/conn"
	"sort"
	"strings"
	"time"
)

// RelationalAdapter adapts an internal relational Store to the universal DataSource interface.
type RelationalAdapter struct {
	store Store
	cfg   conn.ConnectionConfig
}

type projectedBrowsePageStore interface {
	SelectTableBrowsePageContext(
		ctx context.Context,
		req BrowseRequest,
		columns []Column,
		orderBy, identityColumns []string,
		keysetColumn string,
	) (*Result, error)
}

func trimBrowseLookahead(res *Result, pageSize int) ([][]string, [][]bool, bool) {
	rows := res.Rows
	nulls := res.Nulls
	hasNext := len(rows) > pageSize
	if hasNext {
		for i := pageSize; i < len(rows); i++ {
			rows[i] = nil
		}
		rows = rows[:pageSize]
		for i := pageSize; i < len(nulls); i++ {
			nulls[i] = nil
		}
		nulls = nulls[:min(pageSize, len(nulls))]
		for position := range res.Incomplete {
			if position.Row >= pageSize {
				delete(res.Incomplete, position)
			}
		}
		for i := pageSize; i < len(res.RowRefs); i++ {
			res.RowRefs[i] = nil
		}
		res.RowRefs = res.RowRefs[:min(pageSize, len(res.RowRefs))]
	}
	return rows, nulls, hasNext
}

func primaryKeyColumns(columns []Column) []string {
	keys := make([]string, 0, len(columns))
	for _, column := range columns {
		if column.PK {
			keys = append(keys, column.Name)
		}
	}
	return keys
}

func uniqueNotNullIndex(columns []Column, indexes []Index) []string {
	notNull := make(map[string]bool, len(columns))
	for _, column := range columns {
		notNull[column.Name] = column.NotNull || column.PK
	}
	for _, index := range indexes {
		if !index.Unique || index.Partial || len(index.Columns) == 0 {
			continue
		}
		reliable := true
		for _, name := range index.Columns {
			if !notNull[name] {
				reliable = false
				break
			}
		}
		if reliable {
			return append([]string(nil), index.Columns...)
		}
	}
	return nil
}

func (a *RelationalAdapter) stableBrowseOrder(table string, columns []Column) []string {
	orderBy := primaryKeyColumns(columns)
	if len(orderBy) > 0 {
		return orderBy
	}
	indexes, err := a.store.Indexes(table)
	if err != nil {
		return nil
	}
	return uniqueNotNullIndex(columns, indexes)
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

// ReadCell delegates complete-value reads only to relational stores that can
// provide a consistent server-side reader.
func (a *RelationalAdapter) ReadCell(ctx context.Context, req CellReadRequest) (*CellRead, error) {
	reader, ok := a.store.(CellReader)
	if !ok {
		return nil, fmt.Errorf("this relational driver does not support bounded cell reads")
	}
	return reader.ReadCell(ctx, req)
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
	if err := req.Validate(); err != nil {
		return BrowseResponse{}, err
	}
	columns, err := a.store.Columns(req.ObjectName)
	if err != nil {
		return BrowseResponse{}, err
	}
	if err := req.Options.ValidateColumns(columns); err != nil {
		return BrowseResponse{}, err
	}
	browseStore, ok := a.store.(projectedBrowsePageStore)
	if !ok {
		return BrowseResponse{}, fmt.Errorf("safe projected browsing is not supported by this SQL source")
	}
	orderBy := a.stableBrowseOrder(req.ObjectName, columns)
	identity := append([]string(nil), orderBy...)
	keysetColumn, _ := singlePK(columns)
	res, err := browseStore.SelectTableBrowsePageContext(ctx, req, columns, orderBy, identity, keysetColumn)
	if err != nil {
		return BrowseResponse{}, err
	}
	rows, nulls, hasNext := trimBrowseLookahead(res, req.PageSize)
	nextCursor := ""
	if keysetColumn != "" && len(res.RowRefs) > 0 {
		rowRef := res.RowRefs[len(rows)-1]
		if rowRef != nil {
			for _, part := range rowRef.KeyParts() {
				if part.Column == keysetColumn {
					nextCursor = browseCursorString(part.Value.Value)
					break
				}
			}
		}
	}
	return BrowseResponse{
		Data: &TabularData{
			Columns: res.Columns, Rows: rows, Nulls: nulls, Incomplete: res.Incomplete,
			RowRefs: res.RowRefs, Affected: -1, Truncated: res.Truncated, TotalRows: -1,
		},
		HasNext: hasNext, NextCursor: nextCursor, TotalCount: -1,
	}, nil
}
func browseCursorString(value any) string {
	switch value := value.(type) {
	case []byte:
		return "0x" + hex.EncodeToString(value)
	case time.Time:
		return "time:" + value.Format(time.RFC3339Nano)
	default:
		return Stringify(value)
	}
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

func (e *RelationalQueryExecutor) Execute(ctx context.Context, buffer string, line int, limits QueryLimits) (DataView, error) {
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	stmts := SplitStatements(buffer)
	if len(stmts) == 0 {
		return nil, nil
	}
	q := stmts[0].Text
	if len(stmts) > 1 {
		q = stmts[StatementAt(stmts, line)].Text
	}

	if ReturnsSQLRows(q) {
		res, err := e.store.QueryContextMax(ctx, q, limits.MaxRows)
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
