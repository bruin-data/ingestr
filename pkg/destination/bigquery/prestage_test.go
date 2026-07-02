package bigquery

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
)

func buildPreStageTestRecord(t *testing.T, ids []int64, names []string) arrow.RecordBatch {
	t.Helper()

	arrowSchema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "userName", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)

	builder := array.NewRecordBuilder(memory.DefaultAllocator, arrowSchema)
	defer builder.Release()
	builder.Field(0).(*array.Int64Builder).AppendValues(ids, nil)
	builder.Field(1).(*array.StringBuilder).AppendValues(names, nil)

	return builder.NewRecordBatch()
}

func snakeUpperTransform(name string) string {
	if name == "userName" {
		return "user_name"
	}
	return name
}

func TestNewPreStageWriterRejectsStorageWriteMethod(t *testing.T) {
	d := &BigQueryDestination{loadMethod: loadMethodStorageWrite}
	_, err := d.NewPreStageWriter(context.Background(), destination.PreStageOptions{})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected ErrPreStageUnsupported, got %v", err)
	}
}

func TestNewPreStageWriterRejectsExplicitParquet(t *testing.T) {
	d := &BigQueryDestination{loadMethod: loadMethodLoadJob}
	_, err := d.NewPreStageWriter(context.Background(), destination.PreStageOptions{LoaderFileFormat: "parquet"})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected ErrPreStageUnsupported for parquet, got %v", err)
	}
}

func TestPreStageWriterStagesJSONLWithTransformAndTimestamp(t *testing.T) {
	d := &BigQueryDestination{loadMethod: loadMethodLoadJob}
	loadTS := time.Date(2026, 7, 2, 10, 30, 0, 123456000, time.UTC)

	writer, err := d.NewPreStageWriter(context.Background(), destination.PreStageOptions{
		Table:               "ds.users",
		KeyTransform:        snakeUpperTransform,
		LoadTimestampColumn: "_ingestr_loaded_at",
		LoadTimestamp:       loadTS,
		StagingTable:        true,
		LoaderFileSize:      2,
	})
	if err != nil {
		t.Fatalf("NewPreStageWriter: %v", err)
	}

	record := buildPreStageTestRecord(t, []int64{1, 2, 3, 4, 5}, []string{"a", "b", "c", "d", "e"})
	if err := writer.Append(context.Background(), record); err != nil {
		t.Fatalf("Append: %v", err)
	}
	record.Release()

	data, err := writer.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if data == nil {
		t.Fatal("expected pre-staged data, got nil")
	}
	defer data.Close()

	if data.RowCount() != 5 {
		t.Fatalf("RowCount = %d, want 5", data.RowCount())
	}

	ps, ok := data.(*preStagedLoadSet)
	if !ok {
		t.Fatalf("unexpected PreStagedData type %T", data)
	}
	if !ps.staged.ignoreUnknownValues {
		t.Fatal("pre-staged load set must set ignoreUnknownValues")
	}
	if len(ps.staged.chunks) != 3 {
		t.Fatalf("chunk count = %d, want 3 (5 rows / 2 rows per file)", len(ps.staged.chunks))
	}

	var rows []map[string]any
	for _, chunk := range ps.staged.chunks {
		f, err := os.Open(chunk.localPath)
		if err != nil {
			t.Fatalf("open chunk: %v", err)
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			var row map[string]any
			if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
				t.Fatalf("invalid JSONL row: %v", err)
			}
			rows = append(rows, row)
		}
		_ = f.Close()
	}

	if len(rows) != 5 {
		t.Fatalf("row count in files = %d, want 5", len(rows))
	}
	first := rows[0]
	if _, hasOriginal := first["userName"]; hasOriginal {
		t.Fatal("expected key transform to rename userName")
	}
	if first["user_name"] != "a" {
		t.Fatalf("user_name = %v, want a", first["user_name"])
	}
	if first["id"] != float64(1) {
		t.Fatalf("id = %v, want 1", first["id"])
	}
	wantTS := loadTS.Format(time.RFC3339Nano)
	if first["_ingestr_loaded_at"] != wantTS {
		t.Fatalf("_ingestr_loaded_at = %v, want %s", first["_ingestr_loaded_at"], wantTS)
	}
}

func TestPreStageWriterFinishWithNoRowsReturnsNil(t *testing.T) {
	d := &BigQueryDestination{loadMethod: loadMethodLoadJob}
	writer, err := d.NewPreStageWriter(context.Background(), destination.PreStageOptions{Table: "ds.empty"})
	if err != nil {
		t.Fatalf("NewPreStageWriter: %v", err)
	}

	data, err := writer.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if data != nil {
		t.Fatalf("expected nil PreStagedData for empty extract, got %v", data)
	}

	tempDir := writer.(*preStageWriter).tempDir
	if _, statErr := os.Stat(tempDir); !os.IsNotExist(statErr) {
		t.Fatalf("expected temp dir %s to be removed", tempDir)
	}
}

func TestPreStageWriterDiscardRemovesFiles(t *testing.T) {
	d := &BigQueryDestination{loadMethod: loadMethodLoadJob}
	writer, err := d.NewPreStageWriter(context.Background(), destination.PreStageOptions{
		Table:          "ds.users",
		StagingTable:   true,
		LoaderFileSize: 100,
	})
	if err != nil {
		t.Fatalf("NewPreStageWriter: %v", err)
	}

	record := buildPreStageTestRecord(t, []int64{1}, []string{"a"})
	if err := writer.Append(context.Background(), record); err != nil {
		t.Fatalf("Append: %v", err)
	}
	record.Release()

	tempDir := writer.(*preStageWriter).tempDir
	writer.Discard()

	if _, statErr := os.Stat(tempDir); !os.IsNotExist(statErr) {
		t.Fatalf("expected temp dir %s to be removed after Discard", tempDir)
	}
}

func TestPreStagedDataCloseIsIdempotent(t *testing.T) {
	d := &BigQueryDestination{loadMethod: loadMethodLoadJob}
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "part-000001.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ps := &preStagedLoadSet{
		dest: d,
		staged: &stagedLoadSet{
			tempDir: tempDir,
			format:  loadJobFormatJSONL,
			chunks:  []stagedLoadChunk{{index: 0, localPath: path, rows: 1}},
		},
		rows: 1,
	}

	ps.Close()
	ps.Close()

	if _, statErr := os.Stat(tempDir); !os.IsNotExist(statErr) {
		t.Fatalf("expected temp dir %s to be removed", tempDir)
	}
}

func TestWritePreStagedRejectsUnexpectedBatches(t *testing.T) {
	d := &BigQueryDestination{loadMethod: loadMethodLoadJob}
	ps := &preStagedLoadSet{dest: d, staged: &stagedLoadSet{format: loadJobFormatJSONL}, rows: 1}

	record := buildPreStageTestRecord(t, []int64{1}, []string{"a"})
	ch := make(chan source.RecordBatchResult, 1)
	ch <- source.RecordBatchResult{Batch: record}
	close(ch)

	err := d.writePreStaged(context.Background(), "p", "ds", "t", ch, ps, destination.WriteOptions{})
	if err == nil || !strings.Contains(err.Error(), "unexpected record batches") {
		t.Fatalf("expected unexpected-batches error, got %v", err)
	}
}

func TestWritePreStagedDetectsRowCountMismatch(t *testing.T) {
	d := &BigQueryDestination{loadMethod: loadMethodLoadJob}
	tempDir := t.TempDir()
	leftoverPath := filepath.Join(tempDir, "part-000001.jsonl")
	if err := os.WriteFile(leftoverPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Zero staged chunks load zero rows; the writer expected 5.
	ps := &preStagedLoadSet{
		dest: d,
		staged: &stagedLoadSet{
			tempDir: tempDir,
			format:  loadJobFormatJSONL,
		},
		rows: 5,
	}

	ch := make(chan source.RecordBatchResult)
	close(ch)

	err := d.writePreStaged(context.Background(), "p", "ds", "t", ch, ps, destination.WriteOptions{})
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("expected row count mismatch error, got %v", err)
	}
	if _, statErr := os.Stat(tempDir); !os.IsNotExist(statErr) {
		t.Fatalf("expected temp dir %s to be removed after mismatch", tempDir)
	}
}

func TestWriteParallelRejectsForeignPreStagedData(t *testing.T) {
	d := &BigQueryDestination{loadMethod: loadMethodStorageWrite, projectID: "p"}
	ps := &preStagedLoadSet{dest: d, staged: &stagedLoadSet{format: loadJobFormatJSONL}, rows: 1}

	ch := make(chan source.RecordBatchResult)
	close(ch)

	err := d.WriteParallel(context.Background(), ch, destination.WriteOptions{
		Table:     "ds.t",
		PreStaged: ps,
	})
	if err == nil || !strings.Contains(err.Error(), "not compatible") {
		t.Fatalf("expected incompatibility error, got %v", err)
	}
}

func TestLoadJobOutputRowsNilSafety(t *testing.T) {
	if got := loadJobOutputRows(nil); got != -1 {
		t.Fatalf("loadJobOutputRows(nil) = %d, want -1", got)
	}
}
