package store

import (
	"context"
	"fmt"
	"io"
	"os"
)

// SpoolCell copies a CellRead to a private temporary file using a fixed-size
// buffer. The returned file is rewound; its caller owns closing and removing it.
func SpoolCell(ctx context.Context, cell *CellRead, blockBytes int) (*os.File, int64, error) {
	return SpoolCellProgress(ctx, cell, blockBytes, nil)
}

// SpoolCellProgress reports bytes committed to the temporary file after each
// block. total is -1 when the source did not provide a reliable size.
func SpoolCellProgress(ctx context.Context, cell *CellRead, blockBytes int, progress func(done, total int64)) (*os.File, int64, error) {
	if cell == nil || cell.IsNull || cell.Reader == nil || blockBytes <= 0 {
		return nil, 0, fmt.Errorf("spooling requires a non-NULL cell reader and positive block size")
	}
	defer cell.Reader.Close()
	file, err := os.CreateTemp("", "picklock-cell-*")
	if err != nil {
		return nil, 0, fmt.Errorf("create private cell temporary: %w", err)
	}
	ok := false
	defer func() {
		if !ok {
			_ = file.Close()
			_ = os.Remove(file.Name())
		}
	}()
	buffer := make([]byte, blockBytes)
	var total int64
	size := int64(-1)
	if cell.SizeBytes != nil {
		size = *cell.SizeBytes
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		n, readErr := cell.Reader.Read(buffer)
		if n > 0 {
			written, writeErr := file.Write(buffer[:n])
			total += int64(written)
			if writeErr != nil {
				return nil, 0, fmt.Errorf("write cell temporary: %w", writeErr)
			}
			if written != n {
				return nil, 0, io.ErrShortWrite
			}
			if progress != nil {
				progress(total, size)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, 0, fmt.Errorf("read cell block: %w", readErr)
		}
		if n == 0 {
			return nil, 0, io.ErrNoProgress
		}
	}
	if err := cell.Reader.Close(); err != nil {
		return nil, 0, fmt.Errorf("close cell reader: %w", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, 0, fmt.Errorf("rewind cell temporary: %w", err)
	}
	ok = true
	return file, total, nil
}
