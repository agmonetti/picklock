package store

import (
	"bytes"
	"math"
	"testing"
)

func TestQueryLimitsRejectUnsafeBudgets(t *testing.T) {
	for _, limits := range []QueryLimits{
		{}, {MaxRows: -1, MaxCellBytes: 1, MaxResultBytes: 1},
		{MaxRows: math.MaxInt, MaxCellBytes: 1, MaxResultBytes: 1},
		{MaxRows: 1, MaxCellBytes: 0, MaxResultBytes: 1},
		{MaxRows: 1, MaxCellBytes: 2, MaxResultBytes: 1},
	} {
		if err := limits.Validate(); err == nil {
			t.Errorf("accepted unsafe limits %+v", limits)
		}
	}
	if err := (QueryLimits{MaxRows: math.MaxInt - 1, MaxCellBytes: 1, MaxResultBytes: math.MaxInt}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestBrowseRequestRejectsOverflowAndInvalidFilters(t *testing.T) {
	for _, req := range []BrowseRequest{
		{ObjectName: "t", PageSize: math.MaxInt},
		{ObjectName: "t", PageSize: 2, Page: math.MaxInt / 2},
		{ObjectName: "t", PageSize: 1, Page: -1},
		{ObjectName: "t", PageSize: 0},
	} {
		if err := req.Validate(); err == nil {
			t.Errorf("accepted unsafe page %+v", req)
		}
	}
	for _, f := range []BrowseFilter{
		{Column: "id", Operator: "= 1 OR 1=1"},
		{Column: "id", Operator: FilterEqual},
		{Column: "id", Operator: FilterEqual, Value: TypedValue{math.Inf(1)}},
		{Column: "id", Operator: FilterEqual, Value: TypedValue{[]byte(nil)}},
		{Column: "id", Operator: FilterIsNull, Value: TypedValue{"NULL"}},
	} {
		if err := (BrowseOptions{Filters: []BrowseFilter{f}}).Validate(); err == nil {
			t.Errorf("accepted invalid filter %+v", f)
		}
	}
	columns := []Column{{Name: "id"}, {Name: "payload"}}
	options := BrowseOptions{Columns: []string{"payload"}, Filters: []BrowseFilter{{Column: "id", Operator: FilterEqual, Value: TypedValue{int64(9007199254740993)}}, {Column: "payload", Operator: FilterIsNotNull}}}
	if err := options.ValidateColumns(columns); err != nil {
		t.Fatal(err)
	}
	options.Filters[0].Column = "id; DROP TABLE t"
	if err := options.ValidateColumns(columns); err == nil {
		t.Fatal("accepted a column absent from metadata")
	}
	for _, selection := range [][]string{{"payload", "payload"}, {""}, {"missing"}} {
		if err := (BrowseOptions{Columns: selection}).ValidateColumns(columns); err == nil {
			t.Errorf("accepted columns %v", selection)
		}
	}
}

func TestNativeIdentitySurvivesPreviewAndRejectsNullKey(t *testing.T) {
	original := []byte{0, 255, 1, 2, 3}
	ref, err := NewRowReference("objects", []KeyPart{{Column: "tenant", Value: TypedValue{int64(9007199254740993)}}, {Column: "key", Value: TypedValue{original}}})
	if err != nil {
		t.Fatal(err)
	}
	data := &TabularData{Columns: []string{"key"}, Rows: [][]string{{"0x00"}}, Incomplete: map[CellPosition]IncompleteCell{{0, 0}: {State: CellPreview, Type: "BLOB"}}, RowRefs: []*RowReference{ref}}
	data.Rows[0][0] = "shorter visible prefix"
	parts := data.RowRef(0).KeyParts()
	if ref.ObjectName() != "objects" || parts[0].Value.Value != int64(9007199254740993) || !bytes.Equal(parts[1].Value.Value.([]byte), original) {
		t.Fatalf("presentation corrupted native identity: %+v", parts)
	}
	original[0] = 42                      // reused scan buffer must not retarget the row
	parts[1].Value.Value.([]byte)[1] = 42 // callers cannot mutate owned keys
	parts[0].Column = "other"
	retained := ref.KeyParts()
	if retained[0].Column != "tenant" || !bytes.Equal(retained[1].Value.Value.([]byte), []byte{0, 255, 1, 2, 3}) {
		t.Fatal("mutable caller/scan storage changed reliable identity")
	}
	if data.RowRef(-1) != nil || data.RowRef(1) != nil || (&TabularData{Rows: [][]string{{"preview"}}}).RowRef(0) != nil {
		t.Fatal("a row without reliable identity exposed a reference")
	}
	for _, parts := range [][]KeyPart{
		nil, {{Column: "id", Value: TypedValue{nil}}},
		{{Column: "id", Value: TypedValue{[]byte(nil)}}},
		{{Column: "id", Value: TypedValue{int64(1)}}, {Column: "id", Value: TypedValue{int64(2)}}},
	} {
		if _, err := NewRowReference("objects", parts); err == nil {
			t.Errorf("accepted ambiguous/null identity %+v", parts)
		}
	}
}

func TestCellReadRejectsAmbiguousOrUnboundedRequest(t *testing.T) {
	ref, err := NewRowReference("t", []KeyPart{{Column: "id", Value: TypedValue{int64(1)}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, req := range []CellReadRequest{
		{Column: "payload", BlockBytes: 1024},
		{Row: &RowReference{}, Column: "payload", BlockBytes: 1024},
		{Row: ref, BlockBytes: 1024},
		{Row: ref, Column: "payload", BlockBytes: 0},
		{Row: ref, Column: "payload", BlockBytes: -1},
	} {
		if err := req.Validate(); err == nil {
			t.Errorf("accepted ambiguous/unbounded read %+v", req)
		}
	}
	if err := (CellReadRequest{Row: ref, Column: "payload", BlockBytes: 1024}).Validate(); err != nil {
		t.Fatal(err)
	}
}
