package sqlite

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"

	"github.com/agmonetti/picklock/internal/conn"
	"github.com/agmonetti/picklock/internal/store"
)

func TestReadCellSpoolsTextAndBinaryByBlocks(t *testing.T) {
	cfg := conn.New(conn.DriverSQLite)
	cfg.Path = ":memory:"
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.db.Exec(`CREATE TABLE payloads (id INTEGER PRIMARY KEY, text_value TEXT, binary_value BLOB, null_value TEXT)`); err != nil {
		t.Fatal(err)
	}
	text := []byte("a界🙂z")
	binary := []byte{0, 1, 0xff, 0, 0x80, 9}
	if _, err := s.db.Exec(`INSERT INTO payloads VALUES (?, ?, ?, NULL)`, int64(7), string(text), binary); err != nil {
		t.Fatal(err)
	}
	ref, err := store.NewRowReference("payloads", []store.KeyPart{{Column: "id", Value: store.TypedValue{Value: int64(7)}}})
	if err != nil {
		t.Fatal(err)
	}
	nullCell, err := s.ReadCell(context.Background(), store.CellReadRequest{Row: ref, Column: "null_value", BlockBytes: 3})
	if err != nil || !nullCell.IsNull || nullCell.Reader != nil || nullCell.SizeBytes != nil {
		t.Fatalf("NULL cell = %+v, err=%v", nullCell, err)
	}
	for _, test := range []struct {
		column string
		want   []byte
		binary bool
	}{
		{column: "text_value", want: text},
		{column: "binary_value", want: binary, binary: true},
	} {
		cell, err := s.ReadCell(context.Background(), store.CellReadRequest{Row: ref, Column: test.column, BlockBytes: 3})
		if err != nil {
			t.Fatalf("ReadCell(%s): %v", test.column, err)
		}
		if cell.Binary != test.binary || cell.SizeBytes == nil || *cell.SizeBytes != int64(len(test.want)) {
			t.Fatalf("cell metadata = %+v, want binary=%v size=%d", cell, test.binary, len(test.want))
		}
		file, size, err := store.SpoolCell(context.Background(), cell, 3)
		if err != nil {
			t.Fatalf("SpoolCell(%s): %v", test.column, err)
		}
		defer func() {
			name := file.Name()
			_ = file.Close()
			_ = os.Remove(name)
		}()
		got, err := io.ReadAll(file)
		if err != nil || size != int64(len(test.want)) || !bytes.Equal(got, test.want) {
			t.Fatalf("spooled %s = size %d bytes %x err %v, want %d bytes %x", test.column, size, got, err, len(test.want), test.want)
		}
	}
}
