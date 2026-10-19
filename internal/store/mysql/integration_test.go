package mysql

import (
	"context"
	"io"
	"os"
	"strconv"
	"testing"

	"github.com/agmonetti/picklock/internal/conn"
	"github.com/agmonetti/picklock/internal/store"
)

func envCfg(t *testing.T, prefix string) conn.ConnectionConfig {
	t.Helper()
	host := os.Getenv(prefix + "_HOST")
	if host == "" {
		t.Skipf("env %s_HOST not set; skipping integration test", prefix)
	}
	port := 3306
	if raw := os.Getenv(prefix + "_PORT"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 65535 {
			t.Fatalf("invalid %s_PORT %q", prefix, raw)
		}
		port = parsed
	}
	return conn.ConnectionConfig{
		Driver:   conn.DriverMySQL,
		Host:     host,
		Port:     port,
		User:     os.Getenv(prefix + "_USER"),
		Password: os.Getenv(prefix + "_PASSWORD"),
		Database: os.Getenv(prefix + "_DATABASE"),
	}
}

// TestIntegrationProductImages exercises the reported large-text column without
// modifying the fixture database.
func TestIntegrationProductImages(t *testing.T) {
	cfg := envCfg(t, "PICKLOCK_TEST_PRODUCT_IMAGES")
	cfg.Driver = conn.DriverMySQL
	s, err := NewMySQL(cfg)
	if err != nil {
		t.Fatalf("NewMySQL: %v", err)
	}
	defer s.Close()

	browser := store.NewRelationalAdapter(s, cfg)
	projected, err := browser.Browse(context.Background(), store.BrowseRequest{
		ObjectName: "product_images",
		PageSize:   10,
		Options:    store.BrowseOptions{Columns: []string{"id", "mime_type"}},
	})
	if err != nil {
		t.Fatalf("Browse projected product_images: %v", err)
	}
	projectedData, ok := projected.Data.(*store.TabularData)
	if !ok || len(projectedData.Columns) != 2 || len(projectedData.Rows) == 0 {
		t.Fatalf("projected Browse = %#v, want rows with id and mime_type", projected.Data)
	}
	t.Logf("projection: %d rows, columns %v; image_data omitted", len(projectedData.Rows), projectedData.Columns)

	var largestID, largestSize int64
	if err := s.db.QueryRow("SELECT id, OCTET_LENGTH(image_data) FROM product_images ORDER BY OCTET_LENGTH(image_data) DESC, id LIMIT 1").Scan(&largestID, &largestSize); err != nil {
		t.Fatalf("query largest image_data metadata: %v", err)
	}
	if largestSize <= 64 {
		t.Fatalf("largest image_data is %d bytes, want more than preview budget", largestSize)
	}

	imageColumn := -1
	cursor, pages, rows := "", 0, 0
	for {
		page, err := browser.Browse(context.Background(), store.BrowseRequest{
			ObjectName: "product_images",
			PageSize:   50,
			Cursor:     cursor,
			Options:    store.BrowseOptions{PreviewBytes: 64},
		})
		if err != nil {
			t.Fatalf("Browse product_images page %d: %v", pages, err)
		}
		data, ok := page.Data.(*store.TabularData)
		if !ok || len(data.Rows) == 0 {
			t.Fatalf("Browse product_images page %d = %#v, want rows", pages, page.Data)
		}
		if imageColumn < 0 {
			for i, column := range data.Columns {
				if column == "image_data" {
					imageColumn = i
					break
				}
			}
			if imageColumn < 0 {
				t.Fatalf("Browse columns %v do not include image_data", data.Columns)
			}
		}
		for rowIndex, row := range data.Rows {
			position := store.CellPosition{Row: rowIndex, Column: imageColumn}
			state, incomplete := data.Incomplete[position]
			if !incomplete || state.State != store.CellPreview || len(row[imageColumn]) > 64 {
				t.Fatalf("image_data preview page=%d row=%d value_bytes=%d metadata=%+v present=%v", pages, rowIndex, len(row[imageColumn]), state, incomplete)
			}
		}
		rows += len(data.Rows)
		pages++
		if !page.HasNext {
			break
		}
		cursor = page.NextCursor
		if cursor == "" {
			t.Fatalf("Browse page %d reports HasNext without a cursor", pages)
		}
	}
	if rows == 0 {
		t.Fatal("product_images Browse returned no rows")
	}

	largestPage, err := browser.Browse(context.Background(), store.BrowseRequest{
		ObjectName: "product_images",
		PageSize:   1,
		Options: store.BrowseOptions{
			Columns: []string{"id"},
			Filters: []store.BrowseFilter{{Column: "id", Operator: store.FilterEqual, Value: store.TypedValue{Value: largestID}}},
		},
	})
	if err != nil {
		t.Fatalf("Browse largest product_images row: %v", err)
	}
	largestData, ok := largestPage.Data.(*store.TabularData)
	if !ok || len(largestData.Rows) != 1 || largestData.RowRef(0) == nil {
		t.Fatalf("Browse largest row = %#v, want one row with stable identity", largestPage.Data)
	}
	if largestData.Rows[0][0] != strconv.FormatInt(largestID, 10) {
		t.Fatalf("largest row id = %q, want %d", largestData.Rows[0][0], largestID)
	}

	cell, err := browser.ReadCell(context.Background(), store.CellReadRequest{
		Row:        largestData.RowRef(0),
		Column:     "image_data",
		BlockBytes: 64 << 10,
	})
	if err != nil {
		t.Fatalf("ReadCell largest image_data: %v", err)
	}
	if cell.Reader == nil || cell.SizeBytes == nil || *cell.SizeBytes != largestSize {
		t.Fatalf("ReadCell metadata = %+v, scanned maximum size=%d", cell, largestSize)
	}
	defer cell.Reader.Close()
	readBytes, err := io.Copy(io.Discard, cell.Reader)
	if err != nil || readBytes != largestSize {
		t.Fatalf("ReadCell image_data streamed %d bytes, want %d, err=%v", readBytes, largestSize, err)
	}
	t.Logf("paged through %d rows in %d pages with <=64-byte image previews; streamed largest image_data in bounded blocks: %d bytes", rows, pages, readBytes)
}

// TestIntegrationMySQL exercises the engine against a real MySQL.
func TestIntegrationMySQL(t *testing.T) {
	cfg := envCfg(t, "PICKLOCK_TEST_MYSQL")
	cfg.Driver = conn.DriverMySQL
	testStore(t, cfg)
}

// TestIntegrationMariaDB exercises the engine against a real MariaDB.
func TestIntegrationMariaDB(t *testing.T) {
	cfg := envCfg(t, "PICKLOCK_TEST_MARIADB")
	cfg.Driver = conn.DriverMariaDB
	testStore(t, cfg)
}

func testStore(t *testing.T, cfg conn.ConnectionConfig) {
	var (
		s   *Store
		err error
	)
	if cfg.Driver == conn.DriverMariaDB {
		s, err = NewMariaDB(cfg)
	} else {
		s, err = NewMySQL(cfg)
	}
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	if _, err := s.Exec("DROP TABLE IF EXISTS picklock_test"); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := s.Exec("CREATE TABLE picklock_test (id INT AUTO_INCREMENT PRIMARY KEY, name VARCHAR(255) NOT NULL, email VARCHAR(255), payload LONGTEXT, document JSON, binary_payload LONGBLOB)"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.Exec("INSERT INTO picklock_test (name, email, payload, document, binary_payload) VALUES ('Alice','a@t.com',CONCAT('界',REPEAT('x',1048576)),JSON_OBJECT('v',REPEAT('x',1048576)),REPEAT(CAST(0xFF AS BINARY),1048576)), ('Bob','b@t.com',CONCAT('界',REPEAT('x',1048576)),JSON_OBJECT('v',REPEAT('x',1048576)),REPEAT(CAST(0xFF AS BINARY),1048576))"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	t.Cleanup(func() {
		s.Exec("DROP TABLE IF EXISTS picklock_test")
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
	if len(cols) != 6 {
		t.Fatalf("len(Columns) = %d, want 6", len(cols))
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
	t.Logf("projection returned %d selected bytes; omitted text/JSON/blob columns from both rows", retainedBytes)
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
	binaryPosition := store.CellPosition{Row: 0, Column: 5}
	if !ok || len(defaultData.Columns) != 6 || len(defaultData.Rows) != 2 || defaultPage.HasNext ||
		defaultData.Rows[0][3] != "界xxxxx" || defaultData.Incomplete[payloadPosition].State != store.CellPreview ||
		len(defaultData.Rows[0][4]) != 8 || defaultData.Incomplete[documentPosition].State != store.CellPreview ||
		len(defaultData.Rows[0][5]) != 18 || defaultData.Incomplete[binaryPosition].State != store.CellPreview ||
		defaultData.RowRef(0) == nil || defaultData.RowRef(0).KeyParts()[0].Value.Value != int64(1) {
		t.Fatalf("default Browse projection = %+v hasNext=%v, want bounded text/JSON/blob previews and hidden identity", defaultPage.Data, defaultPage.HasNext)
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
	if err != nil || !binaryCell.Binary || len(binaryValue) != 1<<20 || binaryValue[0] != 0xff {
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
// connection against the compose containers (prefer and disable; "require"
// needs a server with a trusted certificate, out of scope here).
func TestIntegrationTLSOptions(t *testing.T) {
	drivers := []struct {
		driver conn.Driver
		prefix string
	}{
		{conn.DriverMySQL, "PICKLOCK_TEST_MYSQL"},
		{conn.DriverMariaDB, "PICKLOCK_TEST_MARIADB"},
	}
	for _, d := range drivers {
		t.Run(string(d.driver), func(t *testing.T) {
			for _, ssl := range []string{"prefer", "disable"} {
				cfg := envCfg(t, d.prefix)
				cfg.Driver = d.driver
				cfg.SSLMode = ssl
				var (
					s   *Store
					err error
				)
				if d.driver == conn.DriverMariaDB {
					s, err = NewMariaDB(cfg)
				} else {
					s, err = NewMySQL(cfg)
				}
				if err != nil {
					t.Fatalf("New(tls=%s): %v", ssl, err)
				}
				if _, err := s.Query("SELECT 1"); err != nil {
					t.Errorf("query tls=%s: %v", ssl, err)
				}
				s.Close()
			}
		})
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

// TestIntegrationReadOnly verifies that a read-only connection rejects writes
// (via an pinned connection with SET SESSION TRANSACTION READ ONLY) while reads
// keep working. A writable connection prepares the table first.
func TestIntegrationReadOnly(t *testing.T) {
	for _, driver := range []conn.Driver{conn.DriverMySQL, conn.DriverMariaDB} {
		t.Run(string(driver), func(t *testing.T) {
			prefix := "PICKLOCK_TEST_MYSQL"
			if driver == conn.DriverMariaDB {
				prefix = "PICKLOCK_TEST_MARIADB"
			}
			cfg := envCfg(t, prefix)
			cfg.Driver = driver

			var open = func(c conn.ConnectionConfig) (*Store, error) {
				if c.Driver == conn.DriverMariaDB {
					return NewMariaDB(c)
				}
				return NewMySQL(c)
			}
			w, err := open(cfg)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if _, err := w.Exec("DROP TABLE IF EXISTS picklock_ro_test"); err != nil {
				t.Fatalf("drop: %v", err)
			}
			if _, err := w.Exec("CREATE TABLE picklock_ro_test (id INT)"); err != nil {
				t.Fatalf("create: %v", err)
			}
			if n, err := w.Exec("INSERT INTO picklock_ro_test VALUES (1)"); err != nil || n != 1 {
				t.Fatalf("seed insert: n=%d err=%v", n, err)
			}
			w.Close()

			rc := cfg
			rc.ReadOnly = true
			ro, err := open(rc)
			if err != nil {
				t.Fatalf("New read-only: %v", err)
			}
			defer ro.Close()

			if _, err := ro.Exec("INSERT INTO picklock_ro_test VALUES (2)"); err == nil {
				t.Error("write must fail in read-only mode")
			}
			if n, err := ro.CountTable("picklock_ro_test"); err != nil || n != 1 {
				t.Errorf("read in read-only mode: n=%d err=%v, want 1", n, err)
			}
		})
	}
}
