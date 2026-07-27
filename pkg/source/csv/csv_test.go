package csv

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestExtractFilePath(t *testing.T) {
	tests := []struct {
		name     string
		uri      string
		wantPath string
		wantEnc  string
	}{
		{"hierarchical csv://", "csv:///tmp/file.csv", "/tmp/file.csv", ""},
		{"opaque csv:", "csv:/tmp/file.csv", "/tmp/file.csv", ""},
		{"percent-encoded space", "csv:///tmp/my%20file.csv", "/tmp/my file.csv", ""},
		{"with encoding param", "csv:///tmp/file.csv?encoding=windows-1252", "/tmp/file.csv", "windows-1252"},
		{"encoding among other params", "csv:///tmp/file.csv?other=foo&encoding=cp1252", "/tmp/file.csv", "cp1252"},
		{"empty encoding value", "csv:///tmp/file.csv?encoding=", "/tmp/file.csv", ""},
		{"non-csv scheme rejected", "http://example.com/file.csv", "", ""},
		{"empty input rejected", "", "", ""},
		{"windows path with backslashes", `csv://C:\actions-runner\bruin\assets\seed.csv`, `C:\actions-runner\bruin\assets\seed.csv`, ""},
		{"windows path with forward slashes", "csv://C:/data/seed.csv", "C:/data/seed.csv", ""},
		{"windows path with encoding", `csv://C:\data\seed.csv?encoding=cp1252`, `C:\data\seed.csv`, "cp1252"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPath, gotEnc := extractFilePath(tt.uri)
			if gotPath != tt.wantPath || gotEnc != tt.wantEnc {
				t.Errorf("extractFilePath(%q) = (%q, %q); want (%q, %q)",
					tt.uri, gotPath, gotEnc, tt.wantPath, tt.wantEnc)
			}
		})
	}
}

func TestDecode_ExplicitEncoding(t *testing.T) {
	t.Run("windows-1252 turns 0x92 into curly apostrophe", func(t *testing.T) {
		// 0x92 is the cp1252 byte for U+2019 ("right single quotation mark").
		assertDecodes(t, []byte{'C', 'a', 'n', 0x92, 't'}, "windows-1252", "Can’t")
	})

	t.Run("aliases all resolve to cp1252", func(t *testing.T) {
		// 0xE9 is "é" in cp1252 / latin1 / iso-8859-1.
		input := []byte{'h', 0xE9, 'l', 'l', 'o'}
		for _, alias := range []string{"cp1252", "latin1", "iso-8859-1"} {
			assertDecodes(t, input, alias, "héllo")
		}
	})

	t.Run("explicit encoding overrides BOM", func(t *testing.T) {
		// File starts with a UTF-8 BOM (EF BB BF) but user declares cp1252.
		// In cp1252 those three bytes are "ï»¿" — proves explicit wins.
		input := []byte{0xEF, 0xBB, 0xBF, 'x'}
		got, err := Decode(bytes.NewReader(input), "windows-1252")
		if err != nil {
			t.Fatal(err)
		}
		out, _ := io.ReadAll(got)
		if !strings.HasPrefix(string(out), "ï»¿") {
			t.Errorf("expected cp1252 decoding of BOM bytes, got %q", out)
		}
	})

	t.Run("unknown encoding returns error", func(t *testing.T) {
		_, err := Decode(bytes.NewReader([]byte("x")), "klingon-7")
		if err == nil || !strings.Contains(err.Error(), "unsupported encoding") {
			t.Errorf("want 'unsupported encoding' error, got %v", err)
		}
	})
}

// assertDecodes runs Decode and checks the output matches want.
func assertDecodes(t *testing.T, input []byte, encName, want string) {
	t.Helper()
	r, err := Decode(bytes.NewReader(input), encName)
	if err != nil {
		t.Fatalf("Decode(_, %q) error: %v", encName, err)
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if string(got) != want {
		t.Errorf("Decode(_, %q) = %q; want %q", encName, got, want)
	}
}

func TestDecode_NoEncoding_BOMHandling(t *testing.T) {
	t.Run("utf-8 BOM stripped", func(t *testing.T) {
		assertDecodes(t, []byte{0xEF, 0xBB, 0xBF, 'a', ',', 'b'}, "", "a,b")
	})
	t.Run("plain passthrough", func(t *testing.T) {
		assertDecodes(t, []byte("a,b\n1,2\n"), "", "a,b\n1,2\n")
	})
	t.Run("utf-16le BOM decoded", func(t *testing.T) {
		assertDecodes(t, []byte{0xFF, 0xFE, 'h', 0x00, 'i', 0x00}, "", "hi")
	})
	t.Run("utf-16be BOM decoded", func(t *testing.T) {
		assertDecodes(t, []byte{0xFE, 0xFF, 0x00, 'h', 0x00, 'i'}, "", "hi")
	})
	t.Run("short file", func(t *testing.T) {
		assertDecodes(t, []byte("x"), "", "x")
	})
	t.Run("empty file", func(t *testing.T) {
		assertDecodes(t, []byte{}, "", "")
	})
}

func TestRead_BuildsBatchesDirectly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.csv")
	content := "id,name,score,name\n" + // duplicate header: last occurrence wins
		"1,alice,3.5,alice2\n" +
		",  ,skipé,\n" + // empty id and whitespace name become NULLs
		"   ,,,\n" + // all-empty row is skipped entirely
		"3,\"quo\"\"ted\",7,x\n" +
		"4,short\n" // short row: the earlier duplicate value survives
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	src := NewCSVSource()
	if err := src.Connect(context.Background(), "csv://"+path); err != nil {
		t.Fatal(err)
	}
	table, err := src.GetTable(context.Background(), source.TableRequest{Name: "t"})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := table.Read(context.Background(), source.ReadOptions{PageSize: 100, ExcludeColumns: []string{"SCORE"}})
	if err != nil {
		t.Fatal(err)
	}

	var batches []arrow.RecordBatch
	for res := range ch {
		if res.Err != nil {
			t.Fatal(res.Err)
		}
		batches = append(batches, res.Batch)
	}
	if len(batches) != 1 {
		t.Fatalf("expected 1 batch, got %d", len(batches))
	}
	rec := batches[0]
	defer rec.Release()

	if rec.NumCols() != 2 {
		t.Fatalf("expected 2 columns (score excluded, name deduped), got %d: %v", rec.NumCols(), rec.Schema())
	}
	if rec.ColumnName(0) != "id" || rec.ColumnName(1) != "name" {
		t.Fatalf("unexpected columns: %s, %s", rec.ColumnName(0), rec.ColumnName(1))
	}
	if rec.NumRows() != 4 {
		t.Fatalf("expected 4 rows, got %d", rec.NumRows())
	}

	valueAt := func(col, row int) (string, bool) {
		arr := rec.Column(col)
		if arr.IsNull(row) {
			return "", false
		}
		storage := arr.(array.ExtensionArray).Storage().(*array.String)
		return storage.Value(row), true
	}

	// Duplicate headers use the last non-empty field, matching the old map-based
	// behavior; values are stored JSON-encoded.
	wantID := []struct {
		val  string
		null bool
	}{{`"1"`, false}, {"", true}, {`"3"`, false}, {`"4"`, false}}
	wantName := []struct {
		val  string
		null bool
	}{{`"alice2"`, false}, {"", true}, {`"x"`, false}, {`"short"`, false}}

	for i := range wantID {
		got, ok := valueAt(0, i)
		if ok == wantID[i].null || (ok && got != wantID[i].val) {
			t.Errorf("id[%d] = (%q, notnull=%v); want (%q, notnull=%v)", i, got, ok, wantID[i].val, !wantID[i].null)
		}
		got, ok = valueAt(1, i)
		if ok == wantName[i].null || (ok && got != wantName[i].val) {
			t.Errorf("name[%d] = (%q, notnull=%v); want (%q, notnull=%v)", i, got, ok, wantName[i].val, !wantName[i].null)
		}
	}
}
