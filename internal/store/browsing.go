package store

import (
	"context"
	"fmt"
	"io"
	"math"
	"time"
)

// QueryLimits bounds application-retained result content, not driver
// allocations, network transfer, or process RSS.
type QueryLimits struct {
	MaxRows        int
	MaxCellBytes   int
	MaxResultBytes int
}

func DefaultQueryLimits() QueryLimits {
	return QueryLimits{MaxRows: 10000, MaxCellBytes: 64 << 10, MaxResultBytes: 16 << 20}
}

func (l QueryLimits) Validate() error {
	if l.MaxRows <= 0 || l.MaxRows == math.MaxInt || l.MaxCellBytes <= 0 || l.MaxResultBytes <= 0 {
		return fmt.Errorf("query limits must be positive and leave room for row lookahead")
	}
	if l.MaxCellBytes > l.MaxResultBytes {
		return fmt.Errorf("cell byte limit exceeds result byte limit")
	}
	return nil
}

// TypedValue retains a native driver value, never a rendered/truncated string.
// Supported values are string, []byte, bool, int64, uint64, float64 and time.Time.
// A nil value is invalid: filters express NULL with an explicit operator, and
// reliable row identities never contain NULL. Byte slices transfer ownership.
type TypedValue struct{ Value any }

func (v TypedValue) Validate() error {
	switch n := v.Value.(type) {
	case []byte:
		if n != nil {
			return nil
		}
	case string, bool, int64, uint64, time.Time:
		return nil
	case float64:
		if !math.IsNaN(n) && !math.IsInf(n, 0) {
			return nil
		}
	}
	return fmt.Errorf("unsupported or null typed value %T", v.Value)
}

type FilterOperator string

const (
	FilterEqual        FilterOperator = "="
	FilterNotEqual     FilterOperator = "!="
	FilterLess         FilterOperator = "<"
	FilterLessEqual    FilterOperator = "<="
	FilterGreater      FilterOperator = ">"
	FilterGreaterEqual FilterOperator = ">="
	FilterIsNull       FilterOperator = "IS NULL"
	FilterIsNotNull    FilterOperator = "IS NOT NULL"
)

// BrowseFilter is AND-combined with the other filters. Column must match metadata.
type BrowseFilter struct {
	Column   string
	Operator FilterOperator
	Value    TypedValue
}

// BrowseOptions are session-only. Empty columns means all columns; PreviewBytes
// zero means no preview option requested, not an unlimited preview.
type BrowseOptions struct {
	Columns      []string
	Filters      []BrowseFilter
	PreviewBytes int
}

func (o BrowseOptions) Validate() error {
	if o.PreviewBytes < 0 {
		return fmt.Errorf("preview bytes must be positive when specified")
	}
	seen := make(map[string]bool, len(o.Columns))
	for _, name := range o.Columns {
		if name == "" || seen[name] {
			return fmt.Errorf("empty or duplicate selected column %q", name)
		}
		seen[name] = true
	}
	for _, f := range o.Filters {
		if f.Column == "" {
			return fmt.Errorf("filter column is required")
		}
		switch f.Operator {
		case FilterIsNull, FilterIsNotNull:
			if f.Value.Value != nil {
				return fmt.Errorf("NULL operator does not accept a value")
			}
		case FilterEqual, FilterNotEqual, FilterLess, FilterLessEqual, FilterGreater, FilterGreaterEqual:
			if err := f.Value.Validate(); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported filter operator %q", f.Operator)
		}
	}
	return nil
}

// ValidateColumns must precede dialect quoting/parameterization in a driver.
func (o BrowseOptions) ValidateColumns(columns []Column) error {
	if err := o.Validate(); err != nil {
		return err
	}
	known := make(map[string]bool, len(columns))
	for _, c := range columns {
		known[c.Name] = true
	}
	for _, c := range o.Columns {
		if !known[c] {
			return fmt.Errorf("unknown column %q", c)
		}
	}
	for _, f := range o.Filters {
		if !known[f.Column] {
			return fmt.Errorf("unknown filter column %q", f.Column)
		}
	}
	return nil
}

// ValidateUnsupported rejects options when a source has no server-side
// implementation. It prevents silent client-side filtering or false promises.
func (o BrowseOptions) ValidateUnsupported() error {
	if err := o.Validate(); err != nil {
		return err
	}
	if len(o.Columns) != 0 || len(o.Filters) != 0 || o.PreviewBytes != 0 {
		return fmt.Errorf("this source does not support column selection, structured filters or bounded previews")
	}
	return nil
}

func (r BrowseRequest) Validate() error {
	if r.ObjectName == "" || r.PageSize <= 0 || r.PageSize == math.MaxInt || r.Page < 0 || r.Page > (math.MaxInt-r.PageSize)/r.PageSize {
		return fmt.Errorf("invalid browse object, page size or page offset")
	}
	return r.Options.Validate()
}

// CellPosition indexes the retained Rows. Absence in Incomplete means complete;
// Nulls remains the only source of truth for NULL, including deferred cells.
type CellPosition struct{ Row, Column int }
type CellState string

const (
	CellPreview  CellState = "preview"
	CellDeferred CellState = "deferred"
)

type IncompleteCell struct {
	State     CellState
	Type      string
	SizeBytes *int64 // nil means unknown, unlike a known zero-length value
}

type LimitReason string

const (
	LimitRows        LimitReason = "rows"
	LimitCellBytes   LimitReason = "cell_bytes"
	LimitResultBytes LimitReason = "result_bytes"
)

// ResultStatus applies equally to tabular and non-tabular editor outcomes.
// An empty Reason means no result cutoff. Cell metadata describes partial items.
type ResultStatus struct {
	Reason        LimitReason
	RetainedItems int
	RetainedBytes int64
}

type KeyPart struct {
	Column string
	Value  TypedValue
}

// RowReference is opaque to presentation code. Only drivers create references
// from complete, native PK/unique-not-null values, independent of visible cells.
// Native key bytes are owned by the reference, not reused scan buffers.
type RowReference struct {
	object string
	parts  []KeyPart
}

func NewRowReference(object string, parts []KeyPart) (*RowReference, error) {
	if object == "" || len(parts) == 0 {
		return nil, fmt.Errorf("row identity requires an object and non-null key")
	}
	seen := make(map[string]bool, len(parts))
	for _, p := range parts {
		if p.Column == "" || seen[p.Column] {
			return nil, fmt.Errorf("invalid identity column %q", p.Column)
		}
		seen[p.Column] = true
		if err := p.Value.Validate(); err != nil {
			return nil, err
		}
	}
	return &RowReference{object: object, parts: cloneKeyParts(parts)}, nil
}
func (r *RowReference) ObjectName() string  { return r.object }
func (r *RowReference) KeyParts() []KeyPart { return cloneKeyParts(r.parts) }

func cloneKeyParts(parts []KeyPart) []KeyPart {
	out := append([]KeyPart(nil), parts...)
	for i := range out {
		if value, ok := out[i].Value.Value.([]byte); ok {
			copyValue := make([]byte, len(value))
			copy(copyValue, value)
			out[i].Value.Value = copyValue
		}
	}
	return out
}

// RowRef returns nil for rows without reliable native identity.
func (t *TabularData) RowRef(row int) *RowReference {
	if row < 0 || row >= len(t.Rows) || row >= len(t.RowRefs) {
		return nil
	}
	return t.RowRefs[row]
}

// CellReader is OPTIONAL: sources without real consistent, bounded server reads
// do not implement it. The reader owns a snapshot until EOF/Close and must
// release resources on cancellation. No driver implements this until E5.
type CellReader interface {
	ReadCell(context.Context, CellReadRequest) (*CellRead, error)
}
type CellReadRequest struct {
	Row        *RowReference
	Column     string
	BlockBytes int
}

func (r CellReadRequest) Validate() error {
	if r.Row == nil || r.Row.object == "" || len(r.Row.parts) == 0 {
		return fmt.Errorf("cell read requires a reliable row reference")
	}
	if r.Column == "" || r.BlockBytes <= 0 {
		return fmt.Errorf("cell read requires a column and positive block bytes")
	}
	return nil
}

type CellRead struct {
	Reader    io.ReadCloser
	SizeBytes *int64
	IsNull    bool // when true Reader is nil; no payload or second NULL metadata
	Binary    bool // reader yields original binary bytes, otherwise UTF-8 text
}
