package jsonl

import (
	"archive/zip"
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

func TestJSONLSourceReadsZIPMembers(t *testing.T) {
	ctx := context.Background()
	archivePath := filepath.Join(t.TempDir(), "release.zip")
	file, err := os.Create(archivePath)
	require.NoError(t, err)
	writer := zip.NewWriter(file)
	for _, entry := range []struct {
		name string
		data string
	}{
		{name: "events/day-1.jsonl", data: "{\"id\":1}\n{\"id\":2}\n"},
		{name: "events/day-2.jsonl", data: "{\"id\":3}\n"},
	} {
		member, err := writer.Create(entry.name)
		require.NoError(t, err)
		_, err = member.Write([]byte(entry.data))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	require.NoError(t, file.Close())

	src := NewJSONLSource()
	require.NoError(t, src.Connect(ctx, "jsonl://"+archivePath))
	table, err := src.GetTable(ctx, source.TableRequest{Name: "events"})
	require.NoError(t, err)
	results, err := table.Read(ctx, source.ReadOptions{})
	require.NoError(t, err)

	var totalRows int64
	for result := range results {
		require.NoError(t, result.Err)
		require.Equal(t, int64(1), result.Batch.NumCols())
		totalRows += result.Batch.NumRows()
		result.Batch.Release()
	}
	require.Equal(t, int64(3), totalRows)
}
