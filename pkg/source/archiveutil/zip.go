package archiveutil

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"github.com/bruin-data/ingestr/pkg/source"
)

func IsZIP(filePath string) bool {
	return strings.HasSuffix(strings.ToLower(filePath), ".zip")
}

func SpoolMember(ctx context.Context, member *zip.File) (*os.File, error) {
	if member.UncompressedSize64 >= math.MaxInt64 {
		return nil, fmt.Errorf("ZIP member %q is too large to spool", member.Name)
	}
	reader, err := member.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open ZIP member %q: %w", member.Name, err)
	}
	defer func() { _ = reader.Close() }()

	tempFile, err := os.CreateTemp("", "ingestr-zip-member-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create ZIP member spool file: %w", err)
	}
	cleanup := func() {
		name := tempFile.Name()
		_ = tempFile.Close()
		_ = os.Remove(name)
	}

	written, err := io.Copy(tempFile, io.LimitReader(&contextReader{ctx: ctx, reader: reader}, int64(member.UncompressedSize64)+1))
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to spool ZIP member %q: %w", member.Name, err)
	}
	if written > int64(member.UncompressedSize64) {
		cleanup()
		return nil, fmt.Errorf("ZIP member %q exceeds its declared uncompressed size", member.Name)
	}
	if _, err := tempFile.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to rewind ZIP member %q: %w", member.Name, err)
	}
	return tempFile, nil
}

func ForwardBatches(ctx context.Context, destination chan<- source.RecordBatchResult, batches <-chan source.RecordBatchResult, limit int) (int, error) {
	rows := 0
	var firstErr error
	for result := range batches {
		if result.Err != nil {
			if firstErr == nil && (limit <= 0 || rows < limit) {
				firstErr = result.Err
			}
			continue
		}
		if result.Batch == nil {
			continue
		}
		if firstErr != nil || (limit > 0 && rows >= limit) {
			result.Batch.Release()
			continue
		}

		batch := result.Batch
		if limit > 0 && rows+int(batch.NumRows()) > limit {
			sliced := batch.NewSlice(0, int64(limit-rows))
			batch.Release()
			batch = sliced
		}

		select {
		case destination <- source.RecordBatchResult{Batch: batch}:
			rows += int(batch.NumRows())
		case <-ctx.Done():
			batch.Release()
			firstErr = ctx.Err()
		}
	}
	return rows, firstErr
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.reader.Read(p)
	}
}
