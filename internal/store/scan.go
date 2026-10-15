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

// ScanResultLimits bounds retained rows and rendered cell content. Driver-owned
// values still exist until Scan returns; conversion and application retention
// are capped before formatting/hex encoding.
func ScanResultLimits(rows *sql.Rows, limits QueryLimits) (*Result, error) {
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	colTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}
	columns := make([]string, len(colTypes))
	for i, ct := range colTypes {
		columns[i] = ct.Name()
	}

	res := &Result{Affected: -1, Columns: columns}
	var retainedBytes int64
	for rows.Next() {
		if len(res.Rows) >= limits.MaxRows {
			res.Truncated = true
			res.Status.Reason = LimitRows
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
		for i, value := range vals {
			nulls[i] = value == nil
		}
		rowBytes := int64(0)
		rowReason := LimitReason("")
		for i, value := range vals {
			if value == nil {
				continue
			}
			remaining := int64(limits.MaxResultBytes) - retainedBytes - rowBytes
			cellLimit := limits.MaxCellBytes
			if remaining < int64(cellLimit) {
				cellLimit = int(remaining)
			}
			text, fullBytes, capped := stringifyBounded(value, cellLimit)
			row[i] = text
			rowBytes += int64(len(text))
			if capped {
				reason := LimitCellBytes
				if remaining < int64(limits.MaxCellBytes) {
					reason = LimitResultBytes
				}
				if rowReason == "" || reason == LimitResultBytes {
					rowReason = reason
				}
				if res.Incomplete == nil {
					res.Incomplete = make(map[CellPosition]IncompleteCell)
				}
				size := int64(fullBytes)
				res.Incomplete[CellPosition{Row: len(res.Rows), Column: i}] = IncompleteCell{
					State: CellPreview, Type: fmt.Sprintf("%T", value), SizeBytes: &size,
				}
			}
			if rowReason != "" {
				for skipped := i + 1; skipped < len(vals); skipped++ {
					if vals[skipped] != nil {
						res.Incomplete[CellPosition{Row: len(res.Rows), Column: skipped}] = IncompleteCell{
							State: CellDeferred, Type: fmt.Sprintf("%T", vals[skipped]),
						}
					}
				}
				break
			}
		}
		res.Rows = append(res.Rows, row)
		res.Nulls = append(res.Nulls, nulls)
		retainedBytes += rowBytes
		if rowReason != "" {
			res.Truncated = true
			res.Status.Reason = rowReason
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	res.Status.RetainedItems = len(res.Rows)
	res.Status.RetainedBytes = retainedBytes
	return res, nil
}

// stringifyBounded applies the display-byte budget without first formatting an
// oversized string or allocating a full hex representation of binary data.
func stringifyBounded(value any, maxBytes int) (text string, fullBytes int, truncated bool) {
	switch v := value.(type) {
	case string:
		fullBytes = len(v)
		if fullBytes <= maxBytes {
			return v, fullBytes, false
		}
		end := maxBytes
		for end > 0 && !utf8.RuneStart(v[end]) {
			end--
		}
		return v[:end], fullBytes, true
	case []byte:
		if len(v) == 1 && (v[0] == 0 || v[0] == 1) {
			s := strconv.Itoa(int(v[0]))
			return s, len(s), len(s) > maxBytes
		}
		if isPrintableUTF8(v) {
			fullBytes = len(v)
			if fullBytes <= maxBytes {
				return string(v), fullBytes, false
			}
			end := maxBytes
			for end > 0 && !utf8.RuneStart(v[end]) {
				end--
			}
			return string(v[:end]), fullBytes, true
		}
		fullBytes = 2 + len(v)*2
		if fullBytes <= maxBytes {
			return "0x" + hex.EncodeToString(v), fullBytes, false
		}
		if maxBytes < 2 {
			return "", fullBytes, true
		}
		n := (maxBytes - 2) / 2
		return "0x" + hex.EncodeToString(v[:min(n, len(v))]), fullBytes, true
	default:
		text = Stringify(value)
		fullBytes = len(text)
		if fullBytes <= maxBytes {
			return text, fullBytes, false
		}
		if end := maxBytes; end > 0 {
			for end > 0 && !utf8.RuneStart(text[end]) {
				end--
			}
			return text[:end], fullBytes, true
		}
		return "", fullBytes, true
	}
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
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		if !unicode.IsPrint(r) && r != '\t' && r != '\n' && r != '\r' {
			return false
		}
		b = b[size:]
	}
	return true
}
