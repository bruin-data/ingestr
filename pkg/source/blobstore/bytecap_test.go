package blobstore

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func writeWideBlobstoreJSONL(t *testing.T, path string, rows, payloadSize int) {
	t.Helper()

	var sb strings.Builder
	payload := strings.Repeat("x", payloadSize)
	for i := 0; i < rows; i++ {
		line, err := json.Marshal(map[string]any{
			"id":      i,
			"payload": payload,
		})
		require.NoError(t, err)
		sb.Write(line)
		sb.WriteByte('\n')
	}
	require.NoError(t, os.WriteFile(path, []byte(sb.String()), 0o644))
}

// runReadJSONLFile drives the real byte-cap read loop in readJSONLFile against a
// local file. Blobstore has no local backend (only S3/GCS/ADLS/SFTP), so the
// download/list layers are bypassed while the batching path under test runs for
// real.
func runReadJSONLFile(t *testing.T, path string, opts source.ReadOptions) (batches int, rows int64) {
	t.Helper()

	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	s := NewBlobstoreSource()
	results := make(chan source.RecordBatchResult, 128)

	var totalRows int64
	var batchNum int
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.readJSONLFile(context.Background(), f, results, &totalRows, &batchNum, 10000, opts, blobstoreFileMetadata{})
		close(results)
	}()

	for r := range results {
		require.NoError(t, r.Err)
		require.NotNil(t, r.Batch)
		batches++
		rows += r.Batch.NumRows()
		r.Batch.Release()
	}
	require.NoError(t, <-errCh)
	return batches, rows
}

func TestBlobstoreByteCap(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "wide.jsonl")

	const recordCount = 50
	writeWideBlobstoreJSONL(t, jsonlPath, recordCount, 2048)

	// Cap OFF: everything lands in a single batch.
	batchesOff, rowsOff := runReadJSONLFile(t, jsonlPath, source.ReadOptions{MaxBatchBytes: 0})
	require.Equal(t, 1, batchesOff, "cap off must produce exactly one batch")
	require.Equal(t, int64(recordCount), rowsOff)

	// Cap ON (small): same rows must split across more than one batch, no row loss.
	batchesOn, rowsOn := runReadJSONLFile(t, jsonlPath, source.ReadOptions{MaxBatchBytes: 4096})
	require.Greater(t, batchesOn, 1, "small cap must split into more than one batch")
	require.Equal(t, int64(recordCount), rowsOn, "byte cap must not drop rows")
}
