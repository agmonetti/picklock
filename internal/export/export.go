// Package export serializes store.DataView into CSV or JSON files. It is a thin
// layer over in-memory results: data is already stringified, so exporting never
// touches the database or a driver.
package export

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/agmonetti/picklock/internal/store"
)

// WriteCSV writes the data view as RFC 4180 CSV.
func WriteCSV(data store.DataView, w io.Writer) error {
	if data == nil || data.IsEmpty() {
		return fmt.Errorf("nothing to export")
	}

	switch v := data.(type) {
	case *store.TabularData:
		return writeTabularCSV(v, w)
	case *store.DocumentData:
		return writeDocumentCSV(v, w)
	case *store.KeyValueData:
		return writeKeyValueCSV(v, w)
	case *store.GraphData:
		return writeGraphCSV(v, w)
	case *store.RawTextData:
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"TEXT"})
		_ = cw.Write([]string{v.Text})
		cw.Flush()
		return cw.Error()
	default:
		return fmt.Errorf("unsupported data format for CSV export")
	}
}

func writeTabularCSV(res *store.TabularData, w io.Writer) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(res.Columns); err != nil {
		return err
	}
	for _, row := range res.Rows {
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func writeDocumentCSV(docs *store.DocumentData, w io.Writer) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"_id", "summary", "json"}); err != nil {
		return err
	}
	for _, doc := range docs.Documents {
		if err := cw.Write([]string{doc.ID, doc.Summary, doc.RawJSON}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func writeKeyValueCSV(kv *store.KeyValueData, w io.Writer) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"INDEX", "VALUE", "EXTRA"}); err != nil {
		return err
	}
	for _, entry := range kv.Entries {
		if err := cw.Write([]string{entry.Index, entry.Value, entry.Extra}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func writeGraphCSV(g *store.GraphData, w io.Writer) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"ID", "LABELS", "PROPERTIES"}); err != nil {
		return err
	}
	for _, node := range g.Nodes {
		labels := strings.Join(node.Labels, ":")
		props, _ := json.Marshal(node.Properties)
		if err := cw.Write([]string{node.ID, labels, string(props)}); err != nil {
			return err
		}
	}
	if len(g.Edges) > 0 {
		if err := cw.Write([]string{"", "", ""}); err != nil {
			return err
		}
		if err := cw.Write([]string{"EDGE_ID", "TYPE", "START", "END", "PROPERTIES"}); err != nil {
			return err
		}
		for _, e := range g.Edges {
			props, _ := json.Marshal(e.Properties)
			if err := cw.Write([]string{e.ID, e.Type, e.StartNode, e.EndNode, string(props)}); err != nil {
				return err
			}
		}
	}
	cw.Flush()
	return cw.Error()
}

// WriteJSON writes the data view as JSON.
func WriteJSON(data store.DataView, w io.Writer) error {
	if data == nil || data.IsEmpty() {
		return fmt.Errorf("nothing to export")
	}

	switch v := data.(type) {
	case *store.TabularData:
		return writeTabularJSON(v, w)
	case *store.DocumentData:
		return writeDocumentJSON(v, w)
	case *store.KeyValueData:
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	case *store.GraphData:
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	case *store.RawTextData:
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	default:
		return fmt.Errorf("unsupported data format for JSON export")
	}
}

func writeTabularJSON(res *store.TabularData, w io.Writer) error {
	rows := make([]orderedRow, len(res.Rows))
	for i, row := range res.Rows {
		vals := make([]any, len(row))
		for j := range row {
			if isNull(res.Nulls, i, j) {
				vals[j] = nil
			} else {
				vals[j] = row[j]
			}
		}
		rows[i] = orderedRow{keys: res.Columns, values: vals}
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(rows)
}

func writeDocumentJSON(docs *store.DocumentData, w io.Writer) error {
	rawItems := make([]json.RawMessage, 0)
	for _, d := range docs.Documents {
		if strings.TrimSpace(d.RawJSON) != "" {
			rawItems = append(rawItems, json.RawMessage(d.RawJSON))
		}
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(rawItems)
}

func isNull(nulls [][]bool, row, col int) bool {
	return nulls != nil && row < len(nulls) && col < len(nulls[row]) && nulls[row][col]
}

type orderedRow struct {
	keys   []string
	values []any
}

func (r orderedRow) MarshalJSON() ([]byte, error) {
	var b []byte
	b = append(b, '{')
	for i, k := range r.keys {
		if i > 0 {
			b = append(b, ',')
		}
		kb, err := jsonNoEscape(k)
		if err != nil {
			return nil, err
		}
		var val any
		if i < len(r.values) {
			val = r.values[i]
		}
		vb, err := jsonNoEscape(val)
		if err != nil {
			return nil, err
		}
		b = append(b, kb...)
		b = append(b, ':')
		b = append(b, vb...)
	}
	b = append(b, '}')
	return b, nil
}

func jsonNoEscape(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}
