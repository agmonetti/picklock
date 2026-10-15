package store_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"reflect"
	"testing"

	"github.com/agmonetti/picklock/internal/store"
)

type chunkReader struct {
	reader io.Reader
}

func (r chunkReader) Read(p []byte) (int, error) { return r.reader.Read(p) }
func (r chunkReader) Close() error               { return nil }

func TestSpoolCellProgressTracksCommittedBytes(t *testing.T) {
	payload := []byte("abcdefgh")
	size := int64(len(payload))
	var updates [][2]int64
	file, got, err := store.SpoolCellProgress(context.Background(), &store.CellRead{
		Reader: chunkReader{reader: bytes.NewReader(payload)}, SizeBytes: &size,
	}, 3, func(done, total int64) { updates = append(updates, [2]int64{done, total}) })
	if err != nil {
		t.Fatalf("SpoolCellProgress: %v", err)
	}
	defer func() { name := file.Name(); _ = file.Close(); _ = os.Remove(name) }()
	if got != size {
		t.Fatalf("size = %d, want %d", got, size)
	}
	want := [][2]int64{{3, 8}, {6, 8}, {8, 8}}
	if !reflect.DeepEqual(updates, want) {
		t.Fatalf("progress = %v, want %v", updates, want)
	}
}
