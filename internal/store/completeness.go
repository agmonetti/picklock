package store

import "fmt"

// IsNull consults the authoritative NULL mask, never the retained value.
func (t *TabularData) IsNull(row, column int) bool {
	return row >= 0 && column >= 0 && row < len(t.Nulls) && column < len(t.Nulls[row]) && t.Nulls[row][column]
}

// CellIncomplete gives NULL priority over incomplete-cell metadata.
func (t *TabularData) CellIncomplete(row, column int) (IncompleteCell, bool) {
	if t.IsNull(row, column) {
		return IncompleteCell{}, false
	}
	cell, ok := t.Incomplete[CellPosition{Row: row, Column: column}]
	return cell, ok
}

// Label describes presentation metadata without including or copying the value.
func (c IncompleteCell) Label() string {
	state := "incomplete"
	switch c.State {
	case CellPreview:
		state = "preview"
	case CellDeferred:
		state = "deferred"
	}
	if c.Type != "" {
		state += " · " + c.Type
	}
	if c.SizeBytes != nil {
		state += fmt.Sprintf(" · %d B", *c.SizeBytes)
	}
	return "<" + state + ">"
}

// ResultCutoffSummary reports an execution cutoff, not normal browse pagination.
func ResultCutoffSummary(data DataView) string {
	var status ResultStatus
	var truncated bool
	var retained int
	switch v := data.(type) {
	case *TabularData:
		status, truncated, retained = v.Status, v.Truncated, len(v.Rows)
	case *DocumentData:
		status, retained = v.Status, len(v.Documents)
	case *KeyValueData:
		status, retained = v.Status, len(v.Entries)
	case *GraphData:
		status, retained = v.Status, len(v.Nodes)+len(v.Edges)
	case *RawTextData:
		status = v.Status
		if v.Text != "" {
			retained = 1
		}
	default:
		return ""
	}
	if status.Reason == "" && !truncated {
		return ""
	}
	if status.Reason == "" {
		return fmt.Sprintf("filter/select less; cutoff (%s); %d items", LimitRows, retained)
	}
	return fmt.Sprintf("filter/select less; cutoff (%s); %d items; %d B", status.Reason, status.RetainedItems, status.RetainedBytes)
}

// IncompletenessSummary distinguishes incomplete content from a complete page.
// It scans metadata only, never the retained payloads.
func IncompletenessSummary(data DataView) string {
	if cutoff := ResultCutoffSummary(data); cutoff != "" {
		return cutoff
	}
	incomplete := false
	switch v := data.(type) {
	case *TabularData:
		for pos := range v.Incomplete {
			if !v.IsNull(pos.Row, pos.Column) {
				incomplete = true
				break
			}
		}
	case *DocumentData:
		for _, item := range v.Documents {
			if item.Incomplete != nil {
				incomplete = true
				break
			}
		}
	case *KeyValueData:
		for _, item := range v.Entries {
			if item.Incomplete != nil {
				incomplete = true
				break
			}
		}
	case *GraphData:
		for _, node := range v.Nodes {
			if len(node.Incomplete) > 0 {
				incomplete = true
				break
			}
		}
		if !incomplete {
			for _, edge := range v.Edges {
				if len(edge.Incomplete) > 0 {
					incomplete = true
					break
				}
			}
		}
	case *RawTextData:
		incomplete = v.Incomplete != nil
	}
	if incomplete {
		return "preview/deferred content; complete values are not loaded"
	}
	return ""
}

// EnsureComplete must run before writing any export bytes or copying a result.
// E2 rejects partial data; it never substitutes markers or silently omits values.
func EnsureComplete(data DataView) error {
	if summary := IncompletenessSummary(data); summary != "" {
		return fmt.Errorf("incomplete data: %s; copy/export requires complete data", summary)
	}
	return nil
}
