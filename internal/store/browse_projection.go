package store

import (
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const DefaultBrowsePreviewBytes = 256

type SQLBrowseProjection struct {
	ValueExpr   string
	NullExpr    string
	PreviewExpr string
	TextExpr    string
	TypeExpr    string
	Type        string
	Text        bool
	Error       string
}

type BrowseProjectionColumn struct {
	Name         string
	Type         string
	ValueIndex   int
	NullIndex    int
	PreviewIndex int
	TextIndex    int
	TypeIndex    int
	Text         bool
}

type BrowseProjectionLayout struct {
	Columns       []BrowseProjectionColumn
	KeyColumns    []string
	KeyValueIndex []int
	PreviewBytes  int
}

// BuildRelationalBrowseProjectionQuery creates a parameterized read query and
// its scan layout. Project is called only with metadata-validated columns.
// Identity values reuse direct visible columns and are otherwise fetched
// separately, even when hidden from the user.
func BuildRelationalBrowseProjectionQuery(
	req BrowseRequest,
	columns []Column,
	orderBy, identityColumns []string,
	keysetColumn string,
	cursorValue any,
	dialect RelationalSQLDialect,
	previewBytes int,
	project func(Column, int) SQLBrowseProjection,
) (string, []any, BrowseProjectionLayout, error) {
	var layout BrowseProjectionLayout
	if err := req.Validate(); err != nil {
		return "", nil, layout, err
	}
	if err := req.Options.ValidateColumns(columns); err != nil {
		return "", nil, layout, err
	}
	if dialect.QuoteIdentifier == nil || dialect.BindVariable == nil || dialect.Pagination == nil || project == nil {
		return "", nil, layout, fmt.Errorf("incomplete SQL browse projection dialect")
	}
	if previewBytes <= 0 || previewBytes > math.MaxInt/2 {
		return "", nil, layout, fmt.Errorf("browse preview size must be positive and bounded for dialect expressions")
	}
	columnByName := make(map[string]Column, len(columns))
	for _, column := range columns {
		columnByName[column.Name] = column
	}
	selected := req.Options.Columns
	if len(selected) == 0 {
		selected = make([]string, len(columns))
		for i := range columns {
			selected[i] = columns[i].Name
		}
	}
	if len(selected) == 0 {
		return "", nil, layout, fmt.Errorf("table has no browseable columns")
	}
	if keysetColumn != "" {
		if _, ok := columnByName[keysetColumn]; !ok || len(identityColumns) != 1 || identityColumns[0] != keysetColumn {
			return "", nil, layout, fmt.Errorf("keyset column is not a reliable scalar identity")
		}
		if cursorValue != nil {
			if err := (TypedValue{Value: cursorValue}).Validate(); err != nil {
				return "", nil, layout, fmt.Errorf("invalid keyset cursor: %w", err)
			}
		}
	}
	for _, key := range identityColumns {
		if _, ok := columnByName[key]; !ok {
			return "", nil, layout, fmt.Errorf("unknown identity column %q", key)
		}
	}
	if len(orderBy) == 0 {
		orderBy = []string{selected[0]}
	}
	for _, name := range orderBy {
		if _, ok := columnByName[name]; !ok {
			return "", nil, layout, fmt.Errorf("unknown ordering column %q", name)
		}
	}

	selectExprs := make([]string, 0, len(selected)*5+len(identityColumns))
	layout.Columns = make([]BrowseProjectionColumn, 0, len(selected))
	directKeyIndices := make(map[string]int, len(identityColumns))
	outputIndex := 0
	for _, name := range selected {
		column := columnByName[name]
		projection := project(column, previewBytes)
		if projection.Error != "" {
			return "", nil, BrowseProjectionLayout{}, fmt.Errorf("cannot safely browse column %q (%s): %s", name, column.Type, projection.Error)
		}
		valueExpr := projection.ValueExpr
		if valueExpr == "" {
			valueExpr = dialect.QuoteIdentifier(name)
		}
		mapping := BrowseProjectionColumn{
			Name: name, Type: projection.Type, ValueIndex: outputIndex,
			NullIndex: -1, PreviewIndex: -1, TextIndex: -1, TypeIndex: -1,
			Text: projection.Text,
		}
		if projection.ValueExpr == "" && projection.NullExpr == "" && projection.PreviewExpr == "" &&
			projection.TextExpr == "" && projection.TypeExpr == "" {
			directKeyIndices[name] = mapping.ValueIndex
		}
		selectExprs = append(selectExprs, valueExpr)
		outputIndex++
		if projection.NullExpr != "" {
			mapping.NullIndex = outputIndex
			selectExprs = append(selectExprs, projection.NullExpr)
			outputIndex++
		}
		if projection.PreviewExpr != "" {
			mapping.PreviewIndex = outputIndex
			selectExprs = append(selectExprs, projection.PreviewExpr)
			outputIndex++
		}
		if projection.TextExpr != "" {
			mapping.TextIndex = outputIndex
			selectExprs = append(selectExprs, projection.TextExpr)
			outputIndex++
		}
		if projection.TypeExpr != "" {
			mapping.TypeIndex = outputIndex
			selectExprs = append(selectExprs, projection.TypeExpr)
			outputIndex++
		}
		layout.Columns = append(layout.Columns, mapping)
	}
	layout.KeyColumns = append([]string(nil), identityColumns...)
	layout.KeyValueIndex = make([]int, len(identityColumns))
	for i, name := range identityColumns {
		if valueIndex, ok := directKeyIndices[name]; ok {
			layout.KeyValueIndex[i] = valueIndex
			continue
		}
		layout.KeyValueIndex[i] = outputIndex
		selectExprs = append(selectExprs, dialect.QuoteIdentifier(name))
		outputIndex++
	}
	layout.PreviewBytes = previewBytes

	var query strings.Builder
	query.WriteString("SELECT ")
	query.WriteString(strings.Join(selectExprs, ", "))
	query.WriteString(" FROM ")
	query.WriteString(dialect.QuoteIdentifier(req.ObjectName))
	args := make([]any, 0, len(req.Options.Filters)+1)
	where := make([]string, 0, len(req.Options.Filters)+1)
	for _, filter := range req.Options.Filters {
		clause := dialect.QuoteIdentifier(filter.Column)
		switch filter.Operator {
		case FilterIsNull, FilterIsNotNull:
			clause += " " + string(filter.Operator)
		case FilterEqual, FilterNotEqual, FilterLess, FilterLessEqual, FilterGreater, FilterGreaterEqual:
			clause += " " + string(filter.Operator) + " " + dialect.BindVariable(len(args)+1)
			args = append(args, filter.Value.Value)
		default:
			return "", nil, BrowseProjectionLayout{}, fmt.Errorf("unsupported filter operator %q", filter.Operator)
		}
		where = append(where, clause)
	}
	if keysetColumn != "" && cursorValue != nil {
		bind := dialect.BindVariable(len(args) + 1)
		predicate := dialect.QuoteIdentifier(keysetColumn) + " > " + bind
		if dialect.KeysetPredicate != nil {
			predicate = dialect.KeysetPredicate(dialect.QuoteIdentifier(keysetColumn), bind)
		}
		where = append(where, predicate)
		args = append(args, cursorValue)
	}
	if len(where) > 0 {
		query.WriteString(" WHERE ")
		query.WriteString(strings.Join(where, " AND "))
	}
	quotedOrder := make([]string, len(orderBy))
	for i, name := range orderBy {
		quotedOrder[i] = dialect.QuoteIdentifier(name)
	}
	query.WriteString(" ORDER BY ")
	query.WriteString(strings.Join(quotedOrder, ", "))
	offset := req.Page * req.PageSize
	if keysetColumn != "" && cursorValue != nil {
		offset = 0
	}
	query.WriteByte(' ')
	query.WriteString(dialect.Pagination(req.PageSize+1, offset))
	return query.String(), args, layout, nil
}

// ParseBrowseCursor restores the native scalar value used by a keyset cursor.
// Text/date/custom key types remain strings and are cast by dialects that need it.
func ParseBrowseCursor(column Column, cursor string) (any, error) {
	typeName := strings.ToLower(strings.TrimSpace(column.Type))
	switch {
	case strings.Contains(typeName, "blob"), strings.Contains(typeName, "binary"), strings.Contains(typeName, "bytea"), strings.Contains(typeName, "image"), typeName == "rowversion", typeName == "timestamp" && strings.HasPrefix(cursor, "0x"):
		encoded := strings.TrimPrefix(cursor, "0x")
		value, err := hex.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("invalid binary browse cursor for %q: %w", column.Name, err)
		}
		return value, nil
	case strings.Contains(typeName, "unsigned"):
		value, err := strconv.ParseUint(cursor, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid unsigned browse cursor for %q: %w", column.Name, err)
		}
		return value, nil
	case integerBrowseType(typeName):
		value, err := strconv.ParseInt(cursor, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid integer browse cursor for %q: %w", column.Name, err)
		}
		return value, nil
	case strings.HasPrefix(cursor, "time:") &&
		(strings.Contains(typeName, "date") || strings.Contains(typeName, "time")):
		value, err := time.Parse(time.RFC3339Nano, strings.TrimPrefix(cursor, "time:"))
		if err != nil {
			return nil, fmt.Errorf("invalid temporal browse cursor for %q: %w", column.Name, err)
		}
		return value, nil
	case typeName == "bool", typeName == "boolean", typeName == "bit":
		value, err := strconv.ParseBool(cursor)
		if err != nil {
			return nil, fmt.Errorf("invalid boolean browse cursor for %q: %w", column.Name, err)
		}
		return value, nil
	default:
		return cursor, nil
	}
}

func integerBrowseType(typeName string) bool {
	switch typeName {
	case "tinyint", "smallint", "mediumint", "int", "integer", "bigint",
		"int2", "int4", "int8", "serial", "smallserial", "bigserial", "year":
		return true
	default:
		return false
	}
}
