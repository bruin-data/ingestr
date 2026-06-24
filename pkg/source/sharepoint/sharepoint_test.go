package sharepoint

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/databuffer"
	"github.com/bruin-data/ingestr/pkg/schemainfer"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func TestParseTableSpec(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    tableSpec
		wantErr bool
	}{
		{
			name:  "format and sheet hints",
			input: "Reports/products.xlsx#xlsx,sheet=Sheet1",
			want:  tableSpec{path: "Reports/products.xlsx", format: formatXLSX, sheets: []string{"Sheet1"}},
		},
		{
			name:  "sheet and skip, format from extension, space in path",
			input: "Reports/parameters file.xlsx#sheet=Forecast,skip=4",
			want:  tableSpec{path: "Reports/parameters file.xlsx", format: formatXLSX, sheets: []string{"Forecast"}, skip: 4},
		},
		{
			name:  "sheets union with ampersand in path",
			input: "Reports/budget & forecast.xlsx#sheets=North|South|East",
			want:  tableSpec{path: "Reports/budget & forecast.xlsx", format: formatXLSX, sheets: []string{"North", "South", "East"}},
		},
		{
			name:  "glob with sheets union",
			input: "Reports/monthly/*.xlsx#sheets=Jan|Feb|Mar",
			want:  tableSpec{path: "Reports/monthly/*.xlsx", format: formatXLSX, sheets: []string{"Jan", "Feb", "Mar"}},
		},
		{
			name:  "sheet names with spaces (single)",
			input: "Reports/Quarterly Summary.xlsx#sheet=Dept. Summary",
			want:  tableSpec{path: "Reports/Quarterly Summary.xlsx", format: formatXLSX, sheets: []string{"Dept. Summary"}},
		},
		{
			name:  "sheet names with spaces (union)",
			input: "Reports/Quarterly Summary.xlsx#sheets=North Region|Dept. Summary|Cost Centers",
			want:  tableSpec{path: "Reports/Quarterly Summary.xlsx", format: formatXLSX, sheets: []string{"North Region", "Dept. Summary", "Cost Centers"}},
		},
		{
			name:  "csv with encoding and tab separator",
			input: "Reports/export.csv#csv,encoding=utf-16le,sep=tab",
			want:  tableSpec{path: "Reports/export.csv", format: formatCSV, encoding: "utf-16le", sep: "tab"},
		},
		{
			name:  "path only, format detected",
			input: "folder/products.xlsx",
			want:  tableSpec{path: "folder/products.xlsx", format: formatXLSX},
		},
		{
			name:  "hash in filename without hints stays literal path",
			input: "folder/Report #3.xlsx",
			want:  tableSpec{path: "folder/Report #3.xlsx", format: formatXLSX},
		},
		{
			name:  "hash in filename with a trailing hint",
			input: "folder/Report #3.xlsx#sheet=Foo",
			want:  tableSpec{path: "folder/Report #3.xlsx", format: formatXLSX, sheets: []string{"Foo"}},
		},
		{
			name:  "escaped hash in path",
			input: "folder/A%23B.csv",
			want:  tableSpec{path: "folder/A#B.csv", format: formatCSV},
		},
		{
			name:  "format override without extension",
			input: "folder/export#xlsx",
			want:  tableSpec{path: "folder/export", format: formatXLSX},
		},
		{
			name:  "formatted hint disables raw values",
			input: "a.xlsx#formatted",
			want:  tableSpec{path: "a.xlsx", format: formatXLSX, formatted: true},
		},
		{
			name:  "drop_empty hint",
			input: "a.csv#csv,drop_empty",
			want:  tableSpec{path: "a.csv", format: formatCSV, dropEmpty: true},
		},
		{
			name:  "date_cols hint",
			input: "a.xlsx#sheet=S,date_cols=DATE|MONTH",
			want:  tableSpec{path: "a.xlsx", format: formatXLSX, sheets: []string{"S"}, dateCols: []string{"DATE", "MONTH"}},
		},
		{
			name:  "extensionless glob defers format to matched files",
			input: "Reports/**",
			want:  tableSpec{path: "Reports/**", format: formatUnknown},
		},
		{
			name:    "unknown format errors",
			input:   "folder/data.txt",
			wantErr: true,
		},
		{
			name:    "legacy xls errors",
			input:   "folder/legacy.xls",
			wantErr: true,
		},
		{
			name:    "xls format hint errors",
			input:   "folder/export#xls",
			wantErr: true,
		},
		{
			name:    "negative skip errors",
			input:   "a.xlsx#skip=-1",
			wantErr: true,
		},
		{
			name:    "non-numeric skip errors",
			input:   "a.xlsx#skip=abc",
			wantErr: true,
		},
		{
			name:    "empty path errors",
			input:   "#xlsx",
			wantErr: true,
		},

		// URL-style query parameter form.
		{
			name:  "query: format and sheet",
			input: "Reports/products.xlsx?format=xlsx&sheet=Sheet1",
			want:  tableSpec{path: "Reports/products.xlsx", format: formatXLSX, sheets: []string{"Sheet1"}},
		},
		{
			name:  "query: sheet and skip, format from extension, space in path",
			input: "Reports/parameters file.xlsx?sheet=Forecast&skip=4",
			want:  tableSpec{path: "Reports/parameters file.xlsx", format: formatXLSX, sheets: []string{"Forecast"}, skip: 4},
		},
		{
			name:  "query: repeated sheets key becomes a union, ampersand in path",
			input: "Reports/budget & forecast.xlsx?sheets=North&sheets=South&sheets=East",
			want:  tableSpec{path: "Reports/budget & forecast.xlsx", format: formatXLSX, sheets: []string{"North", "South", "East"}},
		},
		{
			name:  "query: pipe-joined sheets value also unions",
			input: "Reports/monthly/*.xlsx?sheets=Jan|Feb|Mar",
			want:  tableSpec{path: "Reports/monthly/*.xlsx", format: formatXLSX, sheets: []string{"Jan", "Feb", "Mar"}},
		},
		{
			name:  "query: percent-encoded sheet name with spaces",
			input: "Reports/Quarterly Summary.xlsx?sheet=Dept.%20Summary",
			want:  tableSpec{path: "Reports/Quarterly Summary.xlsx", format: formatXLSX, sheets: []string{"Dept. Summary"}},
		},
		{
			name:  "query: csv with encoding and separator",
			input: "Reports/export.csv?format=csv&encoding=utf-16le&sep=tab",
			want:  tableSpec{path: "Reports/export.csv", format: formatCSV, encoding: "utf-16le", sep: "tab"},
		},
		{
			name:  "query: formatted flag",
			input: "a.xlsx?formatted=true",
			want:  tableSpec{path: "a.xlsx", format: formatXLSX, formatted: true},
		},
		{
			name:  "query: bare flag treated as true",
			input: "a.csv?format=csv&drop_empty",
			want:  tableSpec{path: "a.csv", format: formatCSV, dropEmpty: true},
		},
		{
			name:  "query: date_cols repeated key",
			input: "a.xlsx?sheet=S&date_cols=DATE&date_cols=MONTH",
			want:  tableSpec{path: "a.xlsx", format: formatXLSX, sheets: []string{"S"}, dateCols: []string{"DATE", "MONTH"}},
		},
		{
			name:  "query: escaped hash in path",
			input: "folder/A%23B.csv?format=csv",
			want:  tableSpec{path: "folder/A#B.csv", format: formatCSV},
		},
		{
			name:    "query: unknown parameter errors",
			input:   "a.xlsx?sheett=S",
			wantErr: true,
		},
		{
			name:    "query: negative skip errors",
			input:   "a.xlsx?skip=-1",
			wantErr: true,
		},
		{
			name:    "query: non-numeric skip errors",
			input:   "a.xlsx?skip=abc",
			wantErr: true,
		},
		{
			name:    "query: invalid boolean errors",
			input:   "a.xlsx?formatted=maybe",
			wantErr: true,
		},
		{
			name:    "query: empty path errors",
			input:   "?format=xlsx",
			wantErr: true,
		},

		// "?" glob wildcard must survive the query-form detection.
		{
			name:  "query: ? glob with extension stays a path",
			input: "Reports/q?.xlsx",
			want:  tableSpec{path: "Reports/q?.xlsx", format: formatXLSX},
		},
		{
			name:  "query: extensionless ? glob defers format",
			input: "Reports/dump?",
			want:  tableSpec{path: "Reports/dump?", format: formatUnknown},
		},
		{
			name:  "query: ? glob plus real params (split on last ?)",
			input: "Reports/q?.xlsx?sheet=Jan",
			want:  tableSpec{path: "Reports/q?.xlsx", format: formatXLSX, sheets: []string{"Jan"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseTableSpec(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want.path, got.path)
			assert.Equal(t, tt.want.format, got.format)
			assert.Equal(t, tt.want.sheets, got.sheets)
			assert.Equal(t, tt.want.skip, got.skip)
			assert.Equal(t, tt.want.encoding, got.encoding)
			assert.Equal(t, tt.want.sep, got.sep)
			assert.Equal(t, tt.want.formatted, got.formatted)
			assert.Equal(t, tt.want.dropEmpty, got.dropEmpty)
			assert.Equal(t, tt.want.dateCols, got.dateCols)
		})
	}
}

func TestLooksLikeHints(t *testing.T) {
	t.Parallel()
	assert.True(t, looksLikeHints("xlsx,sheet=Sheet1"))
	assert.True(t, looksLikeHints("sheets=A|B"))
	assert.True(t, looksLikeHints("csv"))
	assert.True(t, looksLikeHints("raw"))
	assert.True(t, looksLikeHints("csv,drop_empty"))
	assert.False(t, looksLikeHints("3.xlsx"))
	assert.False(t, looksLikeHints("Sheet"))
	assert.False(t, looksLikeHints(""))
	assert.False(t, looksLikeHints("sheet=A,bogus=1"))
}

func TestDedupHeaders(t *testing.T) {
	t.Parallel()
	got := dedupHeaders([]string{"A", "", "A", "_row_idx"})
	assert.Equal(t, []string{"A", "column_1", "A_2", "_row_idx_2"}, got)

	// A generated suffix that collides with a real header must be re-suffixed,
	// not produce a duplicate (which would drop a column in buildItem's map).
	got = dedupHeaders([]string{"col", "col_2", "col"})
	assert.Equal(t, []string{"col", "col_2", "col_3"}, got)
}

func TestDetectFormat(t *testing.T) {
	t.Parallel()
	assert.Equal(t, formatXLSX, detectFormat("a/b.xlsx"))
	assert.Equal(t, formatXLSX, detectFormat("a/b.XLSX"))
	assert.Equal(t, formatXLSX, detectFormat("a/b.xlsm"))
	assert.Equal(t, formatXLS, detectFormat("a/b.xls"))
	assert.Equal(t, formatCSV, detectFormat("a/b.csv"))
	assert.Equal(t, formatUnknown, detectFormat("a/b.txt"))
}

func TestSplitSheets(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"A", "B", "C"}, splitSheets("A|B| |C"))
	assert.Empty(t, splitSheets(""))
	// Internal spaces are preserved; only each part's leading/trailing space is trimmed.
	assert.Equal(t, []string{"Dept. Summary", "Cost Centers"}, splitSheets("Dept. Summary | Cost Centers"))
}

func TestResolveSep(t *testing.T) {
	t.Parallel()
	assert.Equal(t, rune(0), resolveSep(""))
	assert.Equal(t, '\t', resolveSep("tab"))
	assert.Equal(t, '\t', resolveSep(`\t`))
	assert.Equal(t, ';', resolveSep(";"))
	assert.Equal(t, rune(0), resolveSep("||"))
}

func TestExtractPrefixAndGlobMeta(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Team/Forecast/", extractPrefix("Team/Forecast/*.xlsx"))
	assert.Equal(t, "Team/", extractPrefix("Team/**/x.xlsx"))
	assert.Equal(t, "", extractPrefix("*.xlsx"))
	assert.Equal(t, "a/b.xlsx", extractPrefix("a/b.xlsx"))

	assert.True(t, hasGlobMeta("*.xlsx"))
	assert.True(t, hasGlobMeta("a/{x,y}.xlsx"))
	assert.True(t, hasGlobMeta("a[1].xlsx"))
	assert.False(t, hasGlobMeta("a/b.xlsx"))
}

func TestEncodePath(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Reports/Q1%20Data/products.xlsx", encodePath("Reports/Q1 Data/products.xlsx"))
	assert.Equal(t, "a%23b/c%20d.xlsx", encodePath("/a#b/c d.xlsx/"))
	assert.Equal(t, "", encodePath("/"))
}

func TestParseURI(t *testing.T) {
	t.Parallel()

	cfg, err := parseURI("sharepoint://?tenant_id=t&client_id=c&client_secret=s&site=sites/Example&hostname=example.sharepoint.com")
	require.NoError(t, err)
	assert.Equal(t, "t", cfg.tenantID)
	assert.Equal(t, "c", cfg.clientID)
	assert.Equal(t, "s", cfg.clientSecret)
	assert.Equal(t, "example.sharepoint.com", cfg.hostname)
	assert.Equal(t, "sites/Example", cfg.sitePath)
	assert.Equal(t, "", cfg.library)          // optional, defaults to the Documents library
	assert.EqualValues(t, 0, cfg.maxFileSize) // unlimited by default
	assert.Equal(t, defaultMaxFiles, cfg.maxFiles)

	cfg, err = parseURI("sharepoint://?tenant_id=t&client_id=c&client_secret=s&site=sites/Example&hostname=example.sharepoint.com&library=Finance%20Docs")
	require.NoError(t, err)
	assert.Equal(t, "Finance Docs", cfg.library)

	// optional hardening limits
	cfg, err = parseURI("sharepoint://?tenant_id=t&client_id=c&client_secret=s&site=sites/Example&hostname=h&max_file_size=1048576&max_files=50")
	require.NoError(t, err)
	assert.EqualValues(t, 1048576, cfg.maxFileSize)
	assert.Equal(t, 50, cfg.maxFiles)

	_, err = parseURI("sharepoint://?tenant_id=t&client_id=c&client_secret=s&site=sites/Example&hostname=h&max_file_size=-1")
	require.Error(t, err)
	_, err = parseURI("sharepoint://?tenant_id=t&client_id=c&client_secret=s&site=sites/Example&hostname=h&max_files=abc")
	require.Error(t, err)

	_, err = parseURI("sharepoint://?tenant_id=t")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "client_id")
	assert.Contains(t, err.Error(), "site")

	_, err = parseURI("http://example.com")
	require.Error(t, err)
}

func TestReadCSV(t *testing.T) {
	t.Parallel()

	data := []byte("name,amount\nfoo,1\nbar,\n")
	rec := readAllCSV(t, "dir/x.csv", data, tableSpec{path: "dir/x.csv", format: formatCSV}, source.ReadOptions{})

	require.EqualValues(t, 2, rec.NumRows())
	cols := columnsByName(rec)

	assertStringColumn(t, cols, "_source_file", []string{"dir/x.csv", "dir/x.csv"})
	assertInt64Column(t, cols, "_row_idx", []int64{0, 1})
	assertStringColumn(t, cols, "name", []string{"foo", "bar"})
	// An empty cell lands as "" rather than null.
	assertStringColumn(t, cols, "amount", []string{"1", ""})

	// _sheet_name is null for flat formats.
	sheetCol := cols["_sheet_name"].(*array.String)
	assert.True(t, sheetCol.IsNull(0))
	assert.True(t, sheetCol.IsNull(1))
}

func TestReadCSVSkipAndTab(t *testing.T) {
	t.Parallel()

	data := []byte("junk line\nname\tamount\nfoo\t1\n")
	rec := readAllCSV(t, "x.csv", data, tableSpec{path: "x.csv", format: formatCSV, sep: "tab", skip: 1}, source.ReadOptions{})

	require.EqualValues(t, 1, rec.NumRows())
	cols := columnsByName(rec)
	assertStringColumn(t, cols, "name", []string{"foo"})
	assertStringColumn(t, cols, "amount", []string{"1"})
}

func TestReadCSVLimit(t *testing.T) {
	t.Parallel()

	data := []byte("name\na\nb\nc\n")
	rec := readAllCSV(t, "x.csv", data, tableSpec{path: "x.csv", format: formatCSV}, source.ReadOptions{Limit: 2})
	require.EqualValues(t, 2, rec.NumRows())
}

func TestReadCSVDropEmpty(t *testing.T) {
	t.Parallel()

	// Row 1 (",") is fully empty; with drop_empty it is skipped and _row_idx
	// keeps its true position (0, then 2 — a gap where the blank row was).
	data := []byte("name,amount\nfoo,1\n,\nbar,2\n")
	rec := readAllCSV(t, "x.csv", data, tableSpec{path: "x.csv", format: formatCSV, dropEmpty: true}, source.ReadOptions{})

	require.EqualValues(t, 2, rec.NumRows())
	cols := columnsByName(rec)
	assertStringColumn(t, cols, "name", []string{"foo", "bar"})
	assertInt64Column(t, cols, "_row_idx", []int64{0, 2})
}

func TestReadCSVMultipleBatches(t *testing.T) {
	t.Parallel()

	data := []byte("name\na\nb\nc\n")
	results := make(chan source.RecordBatchResult, 16)
	total := 0
	err := readCSV(context.Background(), "x.csv", writeTemp(t, data),
		tableSpec{path: "x.csv", format: formatCSV}, source.ReadOptions{PageSize: 1}, results, &total)
	close(results)
	require.NoError(t, err)

	rows, batches := 0, 0
	for r := range results {
		require.NoError(t, r.Err)
		rows += int(r.Batch.NumRows())
		batches++
	}
	assert.Equal(t, 3, rows)
	assert.GreaterOrEqual(t, batches, 2, "PageSize=1 should force multiple batches")
	assert.Equal(t, 3, total)
}

func TestReadCSVContextCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	data := []byte("name\na\nb\nc\n")
	results := make(chan source.RecordBatchResult, 16)
	total := 0
	err := readCSV(ctx, "x.csv", writeTemp(t, data),
		tableSpec{path: "x.csv", format: formatCSV}, source.ReadOptions{}, results, &total)
	require.ErrorIs(t, err, context.Canceled)
}

// TestEmitItemsContextAware proves the producer does not block forever when the
// consumer has stopped draining: with a cancelled context and an unbuffered
// channel with no reader, emitItems returns instead of deadlocking.
func TestEmitItemsContextAware(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	results := make(chan source.RecordBatchResult) // unbuffered, no reader
	total := 0
	cols := buildColumns([]string{"a"})
	items := []map[string]interface{}{{colSourceFile: "x", colRowIdx: 0, "a": "1"}}

	stop, err := emitItems(ctx, results, items, cols, source.ReadOptions{}, &total)
	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, stop)
	assert.Equal(t, 0, total)
}

func TestReadExcelDatesAndMetadata(t *testing.T) {
	t.Parallel()

	f := excelize.NewFile()
	sheet := f.GetSheetName(0)
	require.NoError(t, f.SetSheetRow(sheet, "A1", &[]any{"day", "amount", "label"}))
	// row 2: a pure date, a number, text
	require.NoError(t, f.SetCellValue(sheet, "A2", time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)))
	require.NoError(t, f.SetCellValue(sheet, "B2", 1234.5))
	require.NoError(t, f.SetCellValue(sheet, "C2", "hello"))
	// row 3: a datetime, empty number, empty text
	require.NoError(t, f.SetCellValue(sheet, "A3", time.Date(2024, 1, 15, 13, 30, 0, 0, time.UTC)))

	var buf bytes.Buffer
	require.NoError(t, f.Write(&buf))
	require.NoError(t, f.Close())

	rec := readAllExcel(t, "dir/book.xlsx", buf.Bytes(),
		tableSpec{path: "dir/book.xlsx", format: formatXLSX, dateCols: []string{"day"}}, source.ReadOptions{})
	require.EqualValues(t, 2, rec.NumRows())
	cols := columnsByName(rec)

	assertStringColumn(t, cols, "_source_file", []string{"dir/book.xlsx", "dir/book.xlsx"})
	assertStringColumn(t, cols, "_sheet_name", []string{sheet, sheet})
	assertInt64Column(t, cols, "_row_idx", []int64{0, 1})
	// date_cols column: serial values become ISO strings (date or date-time by value);
	// other numeric values stay raw.
	assertStringColumn(t, cols, "day", []string{"2024-01-15", "2024-01-15 13:30:00"})
	assertStringColumn(t, cols, "amount", []string{"1234.5", ""})
	assertStringColumn(t, cols, "label", []string{"hello", ""})
}

func TestReadExcelDropEmpty(t *testing.T) {
	t.Parallel()

	f := excelize.NewFile()
	sheet := f.GetSheetName(0)
	require.NoError(t, f.SetSheetRow(sheet, "A1", &[]any{"name", "amount"}))
	require.NoError(t, f.SetSheetRow(sheet, "A2", &[]any{"foo", 1.0}))
	// row 3 left entirely blank
	require.NoError(t, f.SetSheetRow(sheet, "A4", &[]any{"bar", 2.0}))
	var buf bytes.Buffer
	require.NoError(t, f.Write(&buf))
	require.NoError(t, f.Close())

	rec := readAllExcel(t, "dir/book.xlsx", buf.Bytes(),
		tableSpec{path: "dir/book.xlsx", format: formatXLSX, dropEmpty: true}, source.ReadOptions{})

	require.EqualValues(t, 2, rec.NumRows())
	cols := columnsByName(rec)
	assertStringColumn(t, cols, "name", []string{"foo", "bar"})
	// The blank middle row is dropped; _row_idx keeps true positions (gap at 1).
	assertInt64Column(t, cols, "_row_idx", []int64{0, 2})
}

// TestHeterogeneousColumnsUnion proves the by-name column union that applies
// across a glob/sheet set: differently-shaped per-file batches are unioned by
// name (extra columns appended) and reconciled with NULL where a column was
// absent. The union itself is performed by schemainfer + databuffer, exactly as
// the pipeline does for a schema-less source like this one.
func TestHeterogeneousColumnsUnion(t *testing.T) {
	t.Parallel()

	batchA := readAllCSV(t, "a.csv", []byte("name,a\nx,1\n"),
		tableSpec{path: "a.csv", format: formatCSV}, source.ReadOptions{})
	batchB := readAllCSV(t, "b.csv", []byte("name,b\ny,2\n"),
		tableSpec{path: "b.csv", format: formatCSV}, source.ReadOptions{})

	// Each file yields its own column set.
	assert.ElementsMatch(t, []string{"_source_file", "_sheet_name", "_row_idx", "name", "a"}, fieldNames(batchA.Schema()))
	assert.ElementsMatch(t, []string{"_source_file", "_sheet_name", "_row_idx", "name", "b"}, fieldNames(batchB.Schema()))

	inf := schemainfer.NewSchemaInferrer()
	require.NoError(t, inf.AddBatch(batchA))
	require.NoError(t, inf.AddBatch(batchB))
	union, err := inf.InferSchema()
	require.NoError(t, err)

	// Union contains both file-specific columns, appended in first-seen order.
	assert.ElementsMatch(t, []string{"_source_file", "_sheet_name", "_row_idx", "name", "a", "b"}, fieldNames(union))

	// Reconciling batch A to the union null-fills the column it lacked ("b").
	recA, err := databuffer.CastRecordToSchema(batchA, union, true)
	require.NoError(t, err)
	colsA := columnsByName(recA)
	assert.True(t, colsA["b"].IsNull(0), "column b should be null for rows from a.csv")
	assert.False(t, colsA["a"].IsNull(0), "column a should be present for rows from a.csv")

	// And batch B null-fills "a".
	recB, err := databuffer.CastRecordToSchema(batchB, union, true)
	require.NoError(t, err)
	colsB := columnsByName(recB)
	assert.True(t, colsB["a"].IsNull(0), "column a should be null for rows from b.csv")
	assert.False(t, colsB["b"].IsNull(0), "column b should be present for rows from b.csv")
}

func fieldNames(s *arrow.Schema) []string {
	names := make([]string, s.NumFields())
	for i := 0; i < s.NumFields(); i++ {
		names[i] = s.Field(i).Name
	}
	return names
}

func TestSerialToISO(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw  string
		want string
		ok   bool
	}{
		{"45306", "2024-01-15", true},               // whole serial -> date
		{"45306.5625", "2024-01-15 13:30:00", true}, // fractional -> date-time
		{"0.5", "12:00:00", true},                   // fraction only -> time
		{"hello", "", false},                        // text -> left untouched
		{"", "", false},                             // empty -> left untouched
		{"0", "", false},                            // non-positive -> not a date
		{"-3", "", false},
		{"NaN", "", false}, // ParseFloat accepts these; must not become a date
		{"Inf", "", false},
		{"+Inf", "", false},
		{"1e9", "", false},     // beyond year 9999 -> out of range
		{"1.5e308", "", false}, // huge -> out of range
	}
	for _, c := range cases {
		got, ok := serialToISO(c.raw, false)
		assert.Equalf(t, c.ok, ok, "raw %q ok", c.raw)
		if c.ok {
			assert.Equalf(t, c.want, got, "raw %q value", c.raw)
		}
	}
}

// TestReadExcelDateColsOnly proves date conversion is opt-in: without date_cols a
// serial lands raw; with date_cols it is converted, and only for that column.
func TestReadExcelDateColsOnly(t *testing.T) {
	t.Parallel()

	build := func() []byte {
		f := excelize.NewFile()
		sheet := f.GetSheetName(0)
		require.NoError(t, f.SetSheetRow(sheet, "A1", &[]any{"d", "n"}))
		require.NoError(t, f.SetCellValue(sheet, "A2", 45306.0)) // serial for 2024-01-15
		require.NoError(t, f.SetCellValue(sheet, "B2", 45306.0)) // same number, not a date column
		var buf bytes.Buffer
		require.NoError(t, f.Write(&buf))
		require.NoError(t, f.Close())
		return buf.Bytes()
	}

	// No date_cols: both columns land as the raw serial.
	rec := readAllExcel(t, "book.xlsx", build(), tableSpec{path: "book.xlsx", format: formatXLSX}, source.ReadOptions{})
	cols := columnsByName(rec)
	assertStringColumn(t, cols, "d", []string{"45306"})
	assertStringColumn(t, cols, "n", []string{"45306"})

	// date_cols=D (case-insensitive match of header "d"): only "d" is converted
	// to ISO; "n" stays raw.
	rec = readAllExcel(t, "book.xlsx", build(),
		tableSpec{path: "book.xlsx", format: formatXLSX, dateCols: []string{"D"}}, source.ReadOptions{})
	cols = columnsByName(rec)
	assertStringColumn(t, cols, "d", []string{"2024-01-15"})
	assertStringColumn(t, cols, "n", []string{"45306"})
}

// TestReadExcelMultipleSheets covers the sheets= union: both sheets are stacked,
// _sheet_name records each row's origin, and _row_idx restarts at 0 per sheet.
func TestReadExcelMultipleSheets(t *testing.T) {
	t.Parallel()

	f := excelize.NewFile()
	s1 := f.GetSheetName(0)
	_, err := f.NewSheet("Second")
	require.NoError(t, err)
	require.NoError(t, f.SetSheetRow(s1, "A1", &[]any{"name"}))
	require.NoError(t, f.SetSheetRow(s1, "A2", &[]any{"a"}))
	require.NoError(t, f.SetSheetRow(s1, "A3", &[]any{"b"}))
	require.NoError(t, f.SetSheetRow("Second", "A1", &[]any{"name"}))
	require.NoError(t, f.SetSheetRow("Second", "A2", &[]any{"c"}))
	var buf bytes.Buffer
	require.NoError(t, f.Write(&buf))
	require.NoError(t, f.Close())

	// Each sheet emits its own batch, so collect across batches.
	results := make(chan source.RecordBatchResult, 16)
	total := 0
	require.NoError(t, readExcel(context.Background(), "book.xlsx", writeTemp(t, buf.Bytes()),
		tableSpec{path: "book.xlsx", format: formatXLSX, sheets: []string{s1, "Second"}}, source.ReadOptions{}, results, &total))
	close(results)

	var names, sheetNames []string
	var rowIdx []int64
	for r := range results {
		require.NoError(t, r.Err)
		cols := columnsByName(r.Batch)
		nameArr := cols["name"].(*array.String)
		sheetArr := cols["_sheet_name"].(*array.String)
		idxArr := cols["_row_idx"].(*array.Int64)
		for i := 0; i < nameArr.Len(); i++ {
			names = append(names, nameArr.Value(i))
			sheetNames = append(sheetNames, sheetArr.Value(i))
			rowIdx = append(rowIdx, idxArr.Value(i))
		}
	}
	assert.Equal(t, []string{"a", "b", "c"}, names)
	assert.Equal(t, []string{s1, s1, "Second"}, sheetNames)
	// _row_idx resets per sheet: 0,1 for the first, then 0 for the second.
	assert.Equal(t, []int64{0, 1, 0}, rowIdx)
	assert.Equal(t, 3, total)
}

// TestReadExcelSheetNameWithSpaces proves a requested sheet whose name contains
// spaces (and a period) is resolved by exact match and selected over the first
// sheet, with _sheet_name stamped using the spaced name.
func TestReadExcelSheetNameWithSpaces(t *testing.T) {
	t.Parallel()

	f := excelize.NewFile()
	first := f.GetSheetName(0)
	_, err := f.NewSheet("Dept. Summary")
	require.NoError(t, err)
	require.NoError(t, f.SetSheetRow(first, "A1", &[]any{"other"}))
	require.NoError(t, f.SetSheetRow(first, "A2", &[]any{"x"}))
	require.NoError(t, f.SetSheetRow("Dept. Summary", "A1", &[]any{"name", "amount"}))
	require.NoError(t, f.SetSheetRow("Dept. Summary", "A2", &[]any{"foo", "1"}))
	var buf bytes.Buffer
	require.NoError(t, f.Write(&buf))
	require.NoError(t, f.Close())

	rec := readAllExcel(t, "Quarterly Summary.xlsx", buf.Bytes(),
		tableSpec{path: "Quarterly Summary.xlsx", format: formatXLSX, sheets: []string{"Dept. Summary"}}, source.ReadOptions{})

	require.EqualValues(t, 1, rec.NumRows())
	cols := columnsByName(rec)
	assertStringColumn(t, cols, "name", []string{"foo"})
	assertStringColumn(t, cols, "_sheet_name", []string{"Dept. Summary"})
}

// TestReadExcelBlankRowSemantics covers the default (non-drop_empty) path: an
// internal blank row is preserved as an all-"" row keeping its _row_idx, while
// trailing blank rows are trimmed — mirroring excelize.GetRows.
func TestReadExcelBlankRowSemantics(t *testing.T) {
	t.Parallel()

	f := excelize.NewFile()
	sheet := f.GetSheetName(0)
	require.NoError(t, f.SetSheetRow(sheet, "A1", &[]any{"name", "amount"}))
	require.NoError(t, f.SetSheetRow(sheet, "A2", &[]any{"foo", "1"}))
	// row 3 left blank (internal gap)
	require.NoError(t, f.SetSheetRow(sheet, "A4", &[]any{"bar", "2"}))
	// rows 5 and 6 left blank (trailing) — never written, so trimmed
	var buf bytes.Buffer
	require.NoError(t, f.Write(&buf))
	require.NoError(t, f.Close())

	rec := readAllExcel(t, "book.xlsx", buf.Bytes(),
		tableSpec{path: "book.xlsx", format: formatXLSX}, source.ReadOptions{})

	require.EqualValues(t, 3, rec.NumRows())
	cols := columnsByName(rec)
	// Internal blank row preserved as all-"" at its true position (row_idx 1).
	assertStringColumn(t, cols, "name", []string{"foo", "", "bar"})
	assertStringColumn(t, cols, "amount", []string{"1", "", "2"})
	assertInt64Column(t, cols, "_row_idx", []int64{0, 1, 2})
}

func TestReadExcelSkip(t *testing.T) {
	t.Parallel()

	build := func() []byte {
		f := excelize.NewFile()
		sheet := f.GetSheetName(0)
		require.NoError(t, f.SetSheetRow(sheet, "A1", &[]any{"junk title"}))
		require.NoError(t, f.SetSheetRow(sheet, "A2", &[]any{"name"}))
		require.NoError(t, f.SetSheetRow(sheet, "A3", &[]any{"foo"}))
		var buf bytes.Buffer
		require.NoError(t, f.Write(&buf))
		require.NoError(t, f.Close())
		return buf.Bytes()
	}

	// skip=1 drops the junk title; row 2 becomes the header, row 3 the data.
	rec := readAllExcel(t, "x.xlsx", build(),
		tableSpec{path: "x.xlsx", format: formatXLSX, skip: 1}, source.ReadOptions{})
	require.EqualValues(t, 1, rec.NumRows())
	assertStringColumn(t, columnsByName(rec), "name", []string{"foo"})

	// skip beyond the available rows yields no header and no rows (no error).
	results := make(chan source.RecordBatchResult, 4)
	total := 0
	require.NoError(t, readExcel(context.Background(), "x.xlsx", writeTemp(t, build()),
		tableSpec{path: "x.xlsx", format: formatXLSX, skip: 10}, source.ReadOptions{}, results, &total))
	close(results)
	for r := range results {
		require.NoError(t, r.Err)
		t.Fatalf("expected no batches when skip exceeds row count")
	}
	assert.Equal(t, 0, total)
}

// writeTemp writes data to a temp file and returns its path; the file is removed
// at test cleanup.
func writeTemp(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

func readAllExcel(t *testing.T, path string, data []byte, spec tableSpec, opts source.ReadOptions) arrow.RecordBatch {
	t.Helper()
	results := make(chan source.RecordBatchResult, 16)
	total := 0
	err := readExcel(context.Background(), path, writeTemp(t, data), spec, opts, results, &total)
	close(results)
	require.NoError(t, err)

	var rec arrow.RecordBatch
	for r := range results {
		require.NoError(t, r.Err)
		require.Nil(t, rec, "expected a single batch in this test")
		rec = r.Batch
	}
	require.NotNil(t, rec)
	return rec
}

// readAllCSV runs readCSV and concatenates the emitted batches into one record
// for assertions. It expects at least one batch.
func readAllCSV(t *testing.T, path string, data []byte, spec tableSpec, opts source.ReadOptions) arrow.RecordBatch {
	t.Helper()
	results := make(chan source.RecordBatchResult, 16)
	total := 0
	err := readCSV(context.Background(), path, writeTemp(t, data), spec, opts, results, &total)
	close(results)
	require.NoError(t, err)

	var rec arrow.RecordBatch
	for r := range results {
		require.NoError(t, r.Err)
		require.Nil(t, rec, "expected a single batch in these tests")
		rec = r.Batch
	}
	require.NotNil(t, rec)
	return rec
}

func columnsByName(rec arrow.RecordBatch) map[string]arrow.Array {
	out := make(map[string]arrow.Array, rec.NumCols())
	for i := 0; i < int(rec.NumCols()); i++ {
		out[rec.Schema().Field(i).Name] = rec.Column(i)
	}
	return out
}

func assertStringColumn(t *testing.T, cols map[string]arrow.Array, name string, want []string) {
	t.Helper()
	arr, ok := cols[name].(*array.String)
	require.True(t, ok, "column %q is not a string array", name)
	require.Equal(t, len(want), arr.Len())
	for i, w := range want {
		assert.Equalf(t, w, arr.Value(i), "column %q row %d", name, i)
	}
}

func assertInt64Column(t *testing.T, cols map[string]arrow.Array, name string, want []int64) {
	t.Helper()
	arr, ok := cols[name].(*array.Int64)
	require.True(t, ok, "column %q is not an int64 array", name)
	require.Equal(t, len(want), arr.Len())
	for i, w := range want {
		assert.Equalf(t, w, arr.Value(i), "column %q row %d", name, i)
	}
}
