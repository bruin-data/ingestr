package avro

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/hamba/avro/v2/ocf"
	"github.com/stretchr/testify/require"
)

const byteCapSchema = `{
  "type": "record",
  "name": "Wide",
  "fields": [
    {"name": "id", "type": "long"},
    {"name": "payload", "type": "string"}
  ]
}`

func writeWideAvro(t *testing.T, path string, rows, payloadSize int) {
	t.Helper()

	f, err := os.Create(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	enc, err := ocf.NewEncoder(byteCapSchema, f)
	require.NoError(t, err)

	payload := strings.Repeat("x", payloadSize)
	for i := 0; i < rows; i++ {
		require.NoError(t, enc.Encode(map[string]any{
			"id":      int64(i),
			"payload": payload,
		}))
	}
	require.NoError(t, enc.Close())
}

func drainAvro(t *testing.T, ch <-chan source.RecordBatchResult) (batches int, rows int64) {
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

func TestAvroByteCap(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	avroPath := filepath.Join(dir, "wide.avro")

	const recordCount = 50
	writeWideAvro(t, avroPath, recordCount, 2048)

	// Cap OFF: everything lands in a single batch.
	srcOff := NewAvroSource()
	require.NoError(t, srcOff.Connect(ctx, "avro://"+avroPath))
	t.Cleanup(func() { _ = srcOff.Close(ctx) })

	tblOff, err := srcOff.GetTable(ctx, source.TableRequest{Name: "wide"})
	require.NoError(t, err)

	resOff, err := tblOff.Read(ctx, source.ReadOptions{MaxBatchBytes: 0})
	require.NoError(t, err)

	batchesOff, rowsOff := drainAvro(t, resOff)
	require.Equal(t, 1, batchesOff, "cap off must produce exactly one batch")
	require.Equal(t, int64(recordCount), rowsOff)

	// Cap ON (small): same rows must split across more than one batch, no row loss.
	srcOn := NewAvroSource()
	require.NoError(t, srcOn.Connect(ctx, "avro://"+avroPath))
	t.Cleanup(func() { _ = srcOn.Close(ctx) })

	tblOn, err := srcOn.GetTable(ctx, source.TableRequest{Name: "wide"})
	require.NoError(t, err)

	resOn, err := tblOn.Read(ctx, source.ReadOptions{MaxBatchBytes: 4096})
	require.NoError(t, err)

	batchesOn, rowsOn := drainAvro(t, resOn)
	require.Greater(t, batchesOn, 1, "small cap must split into more than one batch")
	require.Equal(t, int64(recordCount), rowsOn, "byte cap must not drop rows")
}
