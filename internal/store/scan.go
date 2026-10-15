package store

import (
	"database/sql"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// ScanResult reads all rows from a Rows and converts them to a Result.
// NULL becomes ""; binary, time and numeric types are serialized to string.
func ScanResult(rows *sql.Rows) (*Result, error) {
	return ScanResultMax(rows, 0)
}

// ScanResultMax reads up to max rows from Rows (0 = unlimited) and converts
// them to a Result. When the result set is longer than max, the scan stops
// early and Result.Truncated is set, so the caller can avoid loading an
// unbounded number of rows into memory.
func ScanResultMax(rows *sql.Rows, max int) (*Result, error) {
	colTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}
	columns := make([]string, len(colTypes))
	for i, ct := range colTypes {
		columns[i] = ct.Name()
	}

	var res Result
	res.Affected = -1 // read query
	res.Columns = columns
	for rows.Next() {
		if max > 0 && len(res.Rows) >= max {
			res.Truncated = true
			break
		}
		vals := make([]any, len(columns))
		ptrs := make([]any, len(columns))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make([]string, len(columns))
		nulls := make([]bool, len(columns))
		for i, v := range vals {
			if v == nil {
				nulls[i] = true
			}
			row[i] = Stringify(v)
		}
		res.Rows = append(res.Rows, row)
		res.Nulls = append(res.Nulls, nulls)
	}
	return &res, rows.Err()
}

// ScanBrowseProjection scans projected visible values, hidden NULL/preview
// flags, and native identity values into the public browse result shape.
func ScanBrowseProjection(rows *sql.Rows, layout BrowseProjectionLayout, object string) (*Result, error) {
	outputColumns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	if object == "" {
		return nil, fmt.Errorf("browse projection requires an object name")
	}
	for _, column := range layout.Columns {
		for _, index := range []int{column.ValueIndex, column.NullIndex, column.PreviewIndex, column.TextIndex, column.TypeIndex} {
			if index >= len(outputColumns) {
				return nil, fmt.Errorf("browse projection column %q has an invalid scan index", column.Name)
			}
		}
		if column.ValueIndex < 0 || column.NullIndex < -1 || column.PreviewIndex < -1 || column.TextIndex < -1 || column.TypeIndex < -1 {
			return nil, fmt.Errorf("browse projection column %q has a negative scan index", column.Name)
		}
	}
	for _, index := range layout.KeyValueIndex {
		if index < 0 || index >= len(outputColumns) {
			return nil, fmt.Errorf("browse identity has an invalid scan index")
		}
	}
	if len(layout.KeyColumns) != len(layout.KeyValueIndex) {
		return nil, fmt.Errorf("browse identity layout has mismatched columns and indexes")
	}

	result := &Result{
		Columns:  make([]string, len(layout.Columns)),
		Affected: -1,
	}
	for i := range layout.Columns {
		result.Columns[i] = layout.Columns[i].Name
	}
	for rows.Next() {
		values := make([]any, len(outputColumns))
		pointers := make([]any, len(values))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, err
		}
		row := make([]string, len(layout.Columns))
		nulls := make([]bool, len(layout.Columns))
		for i, column := range layout.Columns {
			value := values[column.ValueIndex]
			isNull := value == nil
			if column.NullIndex >= 0 {
				isNull = browseFlag(values[column.NullIndex])
			}
			nulls[i] = isNull
			isText := column.Text
			if column.TextIndex >= 0 {
				isText = browseFlag(values[column.TextIndex])
			}
			isPreview := !isNull && column.PreviewIndex >= 0 && browseFlag(values[column.PreviewIndex])
			if !isNull && isText && browseTextValueBytes(value) > layout.PreviewBytes {
				isPreview = true
			}
			if isPreview {
				row[i] = stringifyBrowsePreview(value, isText, layout.PreviewBytes)
				if result.Incomplete == nil {
					result.Incomplete = make(map[CellPosition]IncompleteCell)
				}
				typeName := column.Type
				if column.TypeIndex >= 0 {
					typeName = Stringify(values[column.TypeIndex])
				}
				result.Incomplete[CellPosition{Row: len(result.Rows), Column: i}] = IncompleteCell{
					State: CellPreview, Type: typeName,
				}
			} else {
				row[i] = Stringify(value)
			}
		}
		result.Rows = append(result.Rows, row)
		result.Nulls = append(result.Nulls, nulls)
		if len(layout.KeyColumns) > 0 {
			parts := make([]KeyPart, len(layout.KeyColumns))
			valid := true
			for i, name := range layout.KeyColumns {
				value := values[layout.KeyValueIndex[i]]
				if value == nil {
					valid = false
					break
				}
				parts[i] = KeyPart{Column: name, Value: TypedValue{Value: value}}
			}
			if valid {
				if ref, err := NewRowReference(object, parts); err == nil {
					result.RowRefs = append(result.RowRefs, ref)
					continue
				}
			}
			result.RowRefs = append(result.RowRefs, nil)
		}
	}
	return result, rows.Err()
}

func browseFlag(value any) bool {
	switch flag := value.(type) {
	case bool:
		return flag
	case int64:
		return flag != 0
	case int32:
		return flag != 0
	case int:
		return flag != 0
	case []byte:
		return string(flag) == "1" || strings.EqualFold(string(flag), "true") || strings.EqualFold(string(flag), "t")
	case string:
		return flag == "1" || strings.EqualFold(flag, "true") || strings.EqualFold(flag, "t")
	default:
		return false
	}
}

func stringifyBrowsePreview(value any, text bool, maxBytes int) string {
	if !text {
		if bytes, ok := value.([]byte); ok {
			return "0x" + hex.EncodeToString(bytes)
		}
		return Stringify(value)
	}
	if maxBytes <= 0 {
		return Stringify(value)
	}
	var b []byte
	switch v := value.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return Stringify(value)
	}
	if len(b) > maxBytes {
		b = b[:maxBytes]
	}
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return string(b)
}
func browseTextValueBytes(value any) int {
	switch v := value.(type) {
	case []byte:
		return len(v)
	case string:
		return len(v)
	default:
		return 0
	}
}

// Stringify converts a value scanned from database/sql into a string.
func Stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []byte:
		if len(t) == 1 && (t[0] == 0 || t[0] == 1) {
			return strconv.Itoa(int(t[0]))
		}
		if isPrintableUTF8(t) {
			return string(t)
		}
		// binary value: show as 0x… instead of raw bytes
		return "0x" + hex.EncodeToString(t)
	case time.Time:
		return t.Format("2006-01-02 15:04:05")
	case bool:
		return strconv.FormatBool(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", t)
	}
}

func isPrintableUTF8(b []byte) bool {
	if !utf8.Valid(b) {
		return false
	}
	for _, r := range string(b) {
		if !unicode.IsPrint(r) && r != '\t' && r != '\n' && r != '\r' {
			return false
		}
	}
	return true
}
