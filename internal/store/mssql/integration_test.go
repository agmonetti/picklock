package mssql

import (
	"context"
	"io"
	"os"
	"testing"

	"github.com/agmonetti/picklock/internal/conn"
	"github.com/agmonetti/picklock/internal/store"
)

func envCfg(t *testing.T) conn.ConnectionConfig {
	t.Helper()
	host := os.Getenv("PICKLOCK_TEST_MSSQL_HOST")
	if host == "" {
		t.Skip("env PICKLOCK_TEST_MSSQL_HOST not set; skipping integration test")
	}
	return conn.ConnectionConfig{
		Driver:   conn.DriverMSSQL,
		Host:     host,
		Port:     1433,
		User:     os.Getenv("PICKLOCK_TEST_MSSQL_USER"),
		Password: os.Getenv("PICKLOCK_TEST_MSSQL_PASSWORD"),
		Database: os.Getenv("PICKLOCK_TEST_MSSQL_DATABASE"),
	}
}

// TestIntegration exercises the engine against a real SQL Server.
func TestIntegration(t *testing.T) {
	cfg := envCfg(t)

	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	if _, err := s.Exec("IF OBJECT_ID('picklock_test') IS NOT NULL DROP TABLE picklock_test"); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := s.Exec("CREATE TABLE picklock_test (id INT IDENTITY PRIMARY KEY, name NVARCHAR(255) NOT NULL, email NVARCHAR(255), payload NVARCHAR(MAX), document XML, json_payload NVARCHAR(MAX), binary_payload VARBINARY(MAX))"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.Exec("INSERT INTO picklock_test (name, email, payload, document, json_payload, binary_payload) VALUES ('Alice','a@t.com',N'界' + REPLICATE(CAST(N'x' AS NVARCHAR(MAX)),1048576),CAST(N'<v>' + REPLICATE(CAST(N'x' AS NVARCHAR(MAX)),1048576) + N'</v>' AS XML),N'{\"v\":\"' + REPLICATE(CAST(N'x' AS NVARCHAR(MAX)),1048576) + N'\"}',CONVERT(VARBINARY(MAX),REPLICATE(CAST('x' AS VARCHAR(MAX)),1048576))), ('Bob','b@t.com',N'界' + REPLICATE(CAST(N'x' AS NVARCHAR(MAX)),1048576),CAST(N'<v>' + REPLICATE(CAST(N'x' AS NVARCHAR(MAX)),1048576) + N'</v>' AS XML),N'{\"v\":\"' + REPLICATE(CAST(N'x' AS NVARCHAR(MAX)),1048576) + N'\"}',CONVERT(VARBINARY(MAX),REPLICATE(CAST('x' AS VARCHAR(MAX)),1048576)))"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	t.Cleanup(func() {
		s.Exec("IF OBJECT_ID('picklock_test') IS NOT NULL DROP TABLE picklock_test")
	})

	tables, err := s.Tables()
	if err != nil {
		t.Fatalf("Tables: %v", err)
	}
	if !contains(tables, "picklock_test") {
		t.Errorf("Tables does not include picklock_test: %v", tables)
	}

	cols, err := s.Columns("picklock_test")
	if err != nil {
		t.Fatalf("Columns: %v", err)
	}
	if len(cols) != 7 {
		t.Fatalf("len(Columns) = %d, want 7", len(cols))
	}
	if !cols[0].PK || !cols[1].NotNull {
		t.Errorf("constraints: %+v", cols)
	}

	n, err := s.CountTable("picklock_test")
	if err != nil {
		t.Fatalf("CountTable: %v", err)
	}
	if n != 2 {
		t.Errorf("CountTable = %d, want 2", n)
	}

	page, err := s.SelectTablePage("picklock_test", 10, 0)
	if err != nil {
		t.Fatalf("SelectTablePage: %v", err)
	}
	if len(page.Rows) != 2 || page.Rows[0][1] != "Alice" {
		t.Errorf("page.Rows = %v", page.Rows)
	}

	browse := store.NewRelationalAdapter(s, cfg)
	response, err := browse.Browse(context.Background(), store.BrowseRequest{
		ObjectName: "picklock_test",
		PageSize:   10,
		Options: store.BrowseOptions{
			Columns: []string{"name"},
			Filters: []store.BrowseFilter{{Column: "id", Operator: store.FilterGreater, Value: store.TypedValue{Value: int64(1)}}},
		},
	})
	if err != nil {
		t.Fatalf("Browse projection/filter: %v", err)
	}
	data, ok := response.Data.(*store.TabularData)
	if !ok || len(data.Columns) != 1 || data.Columns[0] != "name" || len(data.Rows) != 1 ||
		data.Rows[0][0] != "Bob" || response.HasNext || response.TotalCount != -1 ||
		data.RowRef(0) == nil || data.RowRef(0).KeyParts()[0].Column != "id" ||
		data.RowRef(0).KeyParts()[0].Value.Value != int64(2) {
		t.Fatalf("Browse projection/filter = %+v hasNext=%v total=%d, want Bob only, hidden id=2, unknown total", response.Data, response.HasNext, response.TotalCount)
	}
	retainedBytes := 0
	for _, row := range data.Rows {
		for _, value := range row {
			retainedBytes += len(value)
		}
	}
	if retainedBytes != len("Bob") {
		t.Fatalf("projected result retained %d bytes, want 3", retainedBytes)
	}
	t.Logf("projection returned %d selected bytes; omitted text/JSON/XML/blob columns from both rows", retainedBytes)
	defaultPage, err := browse.Browse(context.Background(), store.BrowseRequest{
		ObjectName: "picklock_test",
		PageSize:   10,
		Options:    store.BrowseOptions{PreviewBytes: 8},
	})
	if err != nil {
		t.Fatalf("Browse default projection: %v", err)
	}
	defaultData, ok := defaultPage.Data.(*store.TabularData)
	payloadPosition := store.CellPosition{Row: 0, Column: 3}
	documentPosition := store.CellPosition{Row: 0, Column: 4}
	jsonPosition := store.CellPosition{Row: 0, Column: 5}
	binaryPosition := store.CellPosition{Row: 0, Column: 6}
	if !ok || len(defaultData.Columns) != 7 || len(defaultData.Rows) != 2 || defaultPage.HasNext ||
		defaultData.Rows[0][3] != "界xxxxx" || defaultData.Incomplete[payloadPosition].State != store.CellPreview ||
		len(defaultData.Rows[0][4]) != 8 || defaultData.Incomplete[documentPosition].State != store.CellPreview ||
		len(defaultData.Rows[0][5]) != 8 || defaultData.Incomplete[jsonPosition].State != store.CellPreview ||
		len(defaultData.Rows[0][6]) != 18 || defaultData.Incomplete[binaryPosition].State != store.CellPreview ||
		defaultData.RowRef(0) == nil || defaultData.RowRef(0).KeyParts()[0].Value.Value != int64(1) {
		t.Fatalf("default Browse projection = %+v hasNext=%v, want bounded text/JSON/XML/blob previews and hidden identity", defaultPage.Data, defaultPage.HasNext)
	}
	textCell, err := s.ReadCell(context.Background(), store.CellReadRequest{Row: defaultData.RowRef(0), Column: "payload", BlockBytes: 64 << 10})
	if err != nil {
		t.Fatalf("ReadCell text: %v", err)
	}
	text, err := io.ReadAll(textCell.Reader)
	if err != nil || len(text) != 1<<20+3 || string(text[:3]) != "界" {
		t.Fatalf("fragmented text read: bytes=%d prefix=%q err=%v", len(text), text[:min(len(text), 3)], err)
	}
	binaryCell, err := s.ReadCell(context.Background(), store.CellReadRequest{Row: defaultData.RowRef(0), Column: "binary_payload", BlockBytes: 64 << 10})
	if err != nil {
		t.Fatalf("ReadCell binary: %v", err)
	}
	binaryValue, err := io.ReadAll(binaryCell.Reader)
	if err != nil || !binaryCell.Binary || len(binaryValue) != 1<<20 || binaryValue[0] != 'x' {
		t.Fatalf("fragmented binary read: bytes=%d binary=%v err=%v", len(binaryValue), binaryCell.Binary, err)
	}

	// keyset pagination over the primary key
	first, err := s.SelectTableKeysetPage("picklock_test", "id", 10, "")
	if err != nil {
		t.Fatalf("SelectTableKeysetPage first: %v", err)
	}
	if len(first.Rows) != 2 || first.Rows[0][1] != "Alice" {
		t.Errorf("first keyset page = %v", first.Rows)
	}
	last := first.Rows[len(first.Rows)-1][0]
	second, err := s.SelectTableKeysetPage("picklock_test", "id", 10, last)
	if err != nil {
		t.Fatalf("SelectTableKeysetPage second: %v", err)
	}
	if len(second.Rows) != 0 {
		t.Errorf("second keyset page = %v, want empty", second.Rows)
	}

	if v, err := s.Version(); err != nil || v == "" {
		t.Errorf("Version = %q, err=%v", v, err)
	}

	fks, err := s.ForeignKeysContext(context.Background())
	if err != nil {
		t.Fatalf("ForeignKeysContext: %v", err)
	}
	for _, fk := range fks {
		if fk.Table == "" || fk.ReferencedTable == "" {
			t.Errorf("ForeignKeysContext returned incomplete key: %+v", fk)
		}
	}
}

// TestIntegrationTLSOptions verifies the TLS field values that open a
// connection against the compose container (prefer keeps the driver default,
// disable sends plaintext; "require" needs a trusted certificate).
func TestIntegrationTLSOptions(t *testing.T) {
	for _, ssl := range []string{"prefer", "disable"} {
		cfg := envCfg(t)
		cfg.SSLMode = ssl
		s, err := New(cfg)
		if err != nil {
			t.Fatalf("New(tls=%s): %v", ssl, err)
		}
		if _, err := s.Query("SELECT @@VERSION"); err != nil {
			t.Errorf("query tls=%s: %v", ssl, err)
		}
		s.Close()
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
