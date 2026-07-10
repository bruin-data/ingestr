package csv

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/source"
)

// readAllValues drains the source and flattens the given column into decoded
// string values ("" for NULL), so parallel and sequential output can be
// compared row by row.
func readAllValues(t *testing.T, path string, opts source.ReadOptions, col int) []string {
	t.Helper()

	src := NewCSVSource()
	if err := src.Connect(context.Background(), "csv://"+path); err != nil {
		t.Fatal(err)
	}
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "t"})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := table.Read(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}

	var values []string
	for res := range ch {
		if res.Err != nil {
			t.Fatal(res.Err)
		}
		storage := res.Batch.Column(col).(array.ExtensionArray).Storage().(*array.String)
		for i := 0; i < storage.Len(); i++ {
			if storage.IsNull(i) {
				values = append(values, "")
			} else {
				values = append(values, storage.Value(i))
			}
		}
		res.Batch.Release()
	}
	return values
}

func TestReadParallel_MatchesSequential(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tricky.csv")

	var sb strings.Builder
	sb.WriteString("id,payload,note\n")
	rows := 5000
	for i := 0; i < rows; i++ {
		switch i % 5 {
		case 0:
			// Quoted field with embedded newlines and escaped quotes: record
			// boundaries must not be detected inside these.
			fmt.Fprintf(&sb, "%d,\"multi\nline \"\"quoted\"\" value %d\",plain\n", i, i)
		case 1:
			fmt.Fprintf(&sb, "%d,,empty-payload\n", i)
		case 2:
			// All-empty row: skipped entirely by both paths.
			sb.WriteString(",,\n")
		case 3:
			fmt.Fprintf(&sb, "%d,short-row-%d\n", i, i)
		default:
			fmt.Fprintf(&sb, "%d,value %d,note %d\n", i, i, i)
		}
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := source.ReadOptions{PageSize: 137}

	sequential := readAllValues(t, path, opts, 1)

	origBlock, origMin := parallelBlockSize, parallelMinFileSize
	parallelBlockSize, parallelMinFileSize = 4096, 1
	defer func() { parallelBlockSize, parallelMinFileSize = origBlock, origMin }()

	parallel := readAllValues(t, path, opts, 1)

	if len(parallel) != len(sequential) {
		t.Fatalf("row count mismatch: parallel=%d sequential=%d", len(parallel), len(sequential))
	}
	for i := range parallel {
		if parallel[i] != sequential[i] {
			t.Fatalf("row %d mismatch:\nparallel:   %q\nsequential: %q", i, parallel[i], sequential[i])
		}
	}
}

func TestReadParallel_ParseErrorSurfaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.csv")

	var sb strings.Builder
	sb.WriteString("a,b\n")
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(&sb, "%d,ok\n", i)
	}
	sb.WriteString("1,\"unterminated\n") // quote never closes: parse error
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	origBlock, origMin := parallelBlockSize, parallelMinFileSize
	parallelBlockSize, parallelMinFileSize = 1024, 1
	defer func() { parallelBlockSize, parallelMinFileSize = origBlock, origMin }()

	src := NewCSVSource()
	if err := src.Connect(context.Background(), "csv://"+path); err != nil {
		t.Fatal(err)
	}
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "t"})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := table.Read(context.Background(), source.ReadOptions{PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}

	sawErr := false
	for res := range ch {
		if res.Err != nil {
			sawErr = true
			continue
		}
		res.Batch.Release()
		if sawErr {
			t.Fatal("received a batch after an error result")
		}
	}
	if !sawErr {
		t.Fatal("expected a parse error to surface")
	}
}

func TestLastRecordBoundary(t *testing.T) {
	tests := []struct {
		name        string
		block       string
		wantCut     int
		wantRecords int
	}{
		{"no newline", "abc", -1, 0},
		{"simple", "a,b\nc,d\ne", 8, 2},
		{"newline inside quotes ignored", "a,\"x\ny\"\nb", 8, 1},
		{"escaped quotes keep parity", "a,\"x\"\"\ny\"\nb,c\n", 14, 2},
		{"open quote no boundary", "a,\"open\nnever closed", -1, 0},
		{"trailing complete", "a,b\n", 4, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cut, records := lastRecordBoundary([]byte(tt.block))
			if cut != tt.wantCut || records != tt.wantRecords {
				t.Errorf("lastRecordBoundary(%q) = (%d, %d); want (%d, %d)", tt.block, cut, records, tt.wantCut, tt.wantRecords)
			}
		})
	}
}
