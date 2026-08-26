package jsonl

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

func writeWideJSONL(t *testing.T, path string, rows, payloadSize int) {
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

func drainJSONL(t *testing.T, ch <-chan source.RecordBatchResult) (batches int, rows int64) {
	t.Helper()
	for r := range ch {
		require.NoError(t, r.Err)
		require.NotNil(t, r.Batch)
		batches++
		rows += r.Batch.NumRows()
		r.Batch.Release()
	}
	return batches, rows
}

func TestJSONLByteCap(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "wide.jsonl")

	const recordCount = 50
	writeWideJSONL(t, jsonlPath, recordCount, 2048)

	// Cap OFF: everything lands in a single batch.
	srcOff := NewJSONLSource()
	require.NoError(t, srcOff.Connect(ctx, "jsonl://"+jsonlPath))
	t.Cleanup(func() { _ = srcOff.Close(ctx) })

	tblOff, err := srcOff.GetTable(ctx, source.TableRequest{Name: "wide"})
	require.NoError(t, err)

	resOff, err := tblOff.Read(ctx, source.ReadOptions{MaxBatchBytes: 0})
	require.NoError(t, err)

	batchesOff, rowsOff := drainJSONL(t, resOff)
	require.Equal(t, 1, batchesOff, "cap off must produce exactly one batch")
	require.Equal(t, int64(recordCount), rowsOff)

	// Cap ON (small): same rows must split across more than one batch, no row loss.
	srcOn := NewJSONLSource()
	require.NoError(t, srcOn.Connect(ctx, "jsonl://"+jsonlPath))
	t.Cleanup(func() { _ = srcOn.Close(ctx) })

	tblOn, err := srcOn.GetTable(ctx, source.TableRequest{Name: "wide"})
	require.NoError(t, err)

	resOn, err := tblOn.Read(ctx, source.ReadOptions{MaxBatchBytes: 4096})
	require.NoError(t, err)

	batchesOn, rowsOn := drainJSONL(t, resOn)
	require.Greater(t, batchesOn, 1, "small cap must split into more than one batch")
	require.Equal(t, int64(recordCount), rowsOn, "byte cap must not drop rows")
}
