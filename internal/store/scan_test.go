package store

import (
	"context"
	"database/sql"
	"strconv"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestStringify(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"nil", nil, ""},
		{"string", "abc", "abc"},
		{"text bytes", []byte("hello"), "hello"},
		{"mysql bit true", []byte{1}, "1"},
		{"mysql bit false", []byte{0}, "0"},
		{"control bytes", []byte{0x01, 0x02, 0x03}, "0x010203"},
		{"utf8 accented bytes", []byte("válido ñañdú"), "válido ñañdú"},
		{"binary bytes", []byte{0x00, 0xff, 0x10}, "0x00ff10"},
		{"time", time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC), "2024-01-02 03:04:05"},
		{"bool", true, "true"},
		{"int", int64(42), "42"},
		{"float", 3.5, "3.5"},
	}
	for _, tc := range cases {
		if got := Stringify(tc.in); got != tc.want {
			t.Errorf("Stringify(%v) [%s] = %q, want %q", tc.in, tc.name, got, tc.want)
		}
	}
}

func TestScanResultMax_TracksNulls(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("CREATE TABLE t (a TEXT, b TEXT)"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := db.Exec("INSERT INTO t VALUES ('x', NULL), (NULL, 'y'), (NULL, NULL)"); err != nil {
		t.Fatalf("insert: %v", err)
	}

	rows, err := db.QueryContext(context.Background(), "SELECT * FROM t")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	res, err := ScanResultMax(rows, 0)
	if err != nil {
		t.Fatalf("ScanResultMax: %v", err)
	}
	want := [][]bool{
		{false, true},
		{true, false},
		{true, true},
	}
	if len(res.Nulls) != len(want) {
		t.Fatalf("Nulls rows = %d, want %d", len(res.Nulls), len(want))
	}
	for i := range want {
		if res.Nulls[i][0] != want[i][0] || res.Nulls[i][1] != want[i][1] {
			t.Errorf("row %d Nulls = %v, want %v", i, res.Nulls[i], want[i])
		}
	}
	if res.Rows[0][1] != "" || res.Rows[1][0] != "" {
		t.Errorf("NULL cells should stringify to empty, got %v", res.Rows)
	}
}

func TestScanResultMaxCapsRows(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	// :memory: databases are per-connection; pin one so the table exists.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("CREATE TABLE t (x INT)"); err != nil {
		t.Fatalf("create: %v", err)
	}
	for i := 0; i < 5; i++ {
		if _, err := db.Exec("INSERT INTO t VALUES (?)", i); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	rows, err := db.QueryContext(context.Background(), "SELECT * FROM t")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	res, err := ScanResultMax(rows, 3)
	rows.Close() // the cursor is abandoned mid-stream; release it before the next query
	if err != nil {
		t.Fatalf("ScanResultMax: %v", err)
	}
	if len(res.Rows) != 3 {
		t.Errorf("Rows = %d, want 3", len(res.Rows))
	}
	if !res.Truncated {
		t.Error("Truncated = false, want true")
	}

	rows2, err := db.QueryContext(context.Background(), "SELECT * FROM t")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	res2, err := ScanResultMax(rows2, 0) // 0 = unlimited
	if err != nil {
		t.Fatalf("ScanResultMax: %v", err)
	}
	if len(res2.Rows) != 5 || res2.Truncated {
		t.Errorf("unlimited: Rows=%d Truncated=%v, want 5/false", len(res2.Rows), res2.Truncated)
	}

	rows3, err := db.QueryContext(context.Background(), "SELECT * FROM t")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	res3, err := ScanResultMax(rows3, 10) // more than available
	if err != nil {
		t.Fatalf("ScanResultMax: %v", err)
	}
	if len(res3.Rows) != 5 || res3.Truncated {
		t.Errorf("over-limit: Rows=%d Truncated=%v, want 5/false", len(res3.Rows), res3.Truncated)
	}
}

func TestScanResultLimitsRetention(t *testing.T) {
	tests := []struct {
		name           string
		query          string
		limits         QueryLimits
		wantReason     LimitReason
		wantRows       int
		wantBytes      int64
		wantFirst      string
		wantIncomplete bool
		incompleteCol  int
	}{
		{"large multibyte cell", `SELECT 'éé'`, QueryLimits{MaxRows: 4, MaxCellBytes: 3, MaxResultBytes: 8}, LimitCellBytes, 1, 2, "é", true, 0},
		{"result budget cuts first row", `SELECT 'ab', 'cd'`, QueryLimits{MaxRows: 4, MaxCellBytes: 3, MaxResultBytes: 3}, LimitResultBytes, 1, 3, "ab", true, 1},
		{"many small rows", `SELECT 'a' UNION ALL SELECT 'b' UNION ALL SELECT 'c'`, QueryLimits{MaxRows: 2, MaxCellBytes: 4, MaxResultBytes: 8}, LimitRows, 2, 2, "a", false, 0},
		{"exact budget", `SELECT 'abc'`, QueryLimits{MaxRows: 4, MaxCellBytes: 3, MaxResultBytes: 3}, "", 1, 3, "abc", false, 0},
		{"binary formatting bounded", `SELECT X'00FF10'`, QueryLimits{MaxRows: 4, MaxCellBytes: 6, MaxResultBytes: 8}, LimitCellBytes, 1, 6, "0x00ff", true, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db, err := sql.Open("sqlite", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			rows, err := db.QueryContext(context.Background(), tc.query)
			if err != nil {
				t.Fatal(err)
			}
			res, err := ScanResultLimits(rows, tc.limits)
			closeErr := rows.Close()
			if err != nil {
				t.Fatal(err)
			}
			if closeErr != nil {
				t.Fatal(closeErr)
			}
			if len(res.Rows) != tc.wantRows || res.Status.Reason != tc.wantReason || res.Status.RetainedBytes != tc.wantBytes {
				t.Fatalf("result rows=%d status=%+v, want rows=%d reason=%q bytes=%d", len(res.Rows), res.Status, tc.wantRows, tc.wantReason, tc.wantBytes)
			}
			if res.Rows[0][0] != tc.wantFirst {
				t.Errorf("first cell=%q, want %q", res.Rows[0][0], tc.wantFirst)
			}
			cell, incomplete := res.Incomplete[CellPosition{Row: 0, Column: tc.incompleteCol}]
			if incomplete != tc.wantIncomplete {
				t.Errorf("incomplete=%v, want %v", incomplete, tc.wantIncomplete)
			}
			if incomplete && (cell.SizeBytes == nil || *cell.SizeBytes == 0) {
				t.Errorf("incomplete metadata = %+v, want original size", cell)
			}
			if res.Truncated != (tc.wantReason != "") {
				t.Errorf("truncated=%v, want %v", res.Truncated, tc.wantReason != "")
			}
		})
	}
}

func TestScanResultLimitsMarksSkippedSuffixAndNulls(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.QueryContext(context.Background(), `SELECT 'heavy', 'tail', NULL`)
	if err != nil {
		t.Fatal(err)
	}
	res, scanErr := ScanResultLimits(rows, QueryLimits{MaxRows: 2, MaxCellBytes: 3, MaxResultBytes: 8})
	closeErr := rows.Close()
	if scanErr != nil {
		t.Fatal(scanErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	if res.Rows[0][0] != "hea" || !res.Nulls[0][2] {
		t.Fatalf("row/nulls = %q/%v, want bounded prefix and authoritative NULL", res.Rows[0], res.Nulls[0])
	}
	if cell := res.Incomplete[CellPosition{Row: 0, Column: 1}]; cell.State != CellDeferred {
		t.Fatalf("unretained suffix cell = %+v, want deferred", cell)
	}
}

// BenchmarkScanResultLimitsDriverSQLite measures allocations made while the
// SQLite driver fetches an oversized value and the executor closes the cursor.
func BenchmarkScanResultLimitsDriverSQLite(b *testing.B) {
	for _, size := range []int{128 << 10, 1 << 20, 8 << 20} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			db, err := sql.Open("sqlite", ":memory:")
			if err != nil {
				b.Fatal(err)
			}
			defer db.Close()
			db.SetMaxOpenConns(1)
			query := "SELECT zeroblob(" + strconv.Itoa(size) + ")"
			limits := QueryLimits{MaxRows: 1, MaxCellBytes: 64 << 10, MaxResultBytes: 64 << 10}
			var closeNanos int64

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rows, err := db.QueryContext(context.Background(), query)
				if err != nil {
					b.Fatal(err)
				}
				result, scanErr := ScanResultLimits(rows, limits)
				startClose := time.Now()
				closeErr := rows.Close()
				closeNanos += time.Since(startClose).Nanoseconds()
				if scanErr != nil {
					b.Fatal(scanErr)
				}
				if closeErr != nil {
					b.Fatal(closeErr)
				}
				if result.Status.Reason != LimitCellBytes || result.Status.RetainedBytes != 64<<10 {
					b.Fatalf("status = %+v, want cell cutoff at 64 KiB", result.Status)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(closeNanos)/float64(b.N), "close-ns/op")
		})
	}
}
