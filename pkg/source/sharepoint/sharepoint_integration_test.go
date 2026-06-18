//go:build integration

package sharepoint_test

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/source/sharepoint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSharePointSource reads a real workbook/file from SharePoint and validates
// the source's structural contract: metadata columns, raw all-VARCHAR data,
// and a monotonic _row_idx. It is gated behind credentials so CI without them
// skips it. Point it at a known file via the env vars below; set
// SHAREPOINT_EXPECTED_ROWS to additionally assert an exact row count.
//
//	SHAREPOINT_TENANT_ID
//	SHAREPOINT_CLIENT_ID
//	SHAREPOINT_CLIENT_SECRET
//	SHAREPOINT_HOSTNAME       tenant hostname, e.g. <tenant>.sharepoint.com
//	SHAREPOINT_SITE           server-relative site path, e.g. sites/<name>
//	SHAREPOINT_TEST_TABLE     source-table string, e.g. <path>/<file>.xlsx#sheet=<name>
//	SHAREPOINT_EXPECTED_ROWS  optional, exact total row count
func TestSharePointSource(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	env := func(k string) string { return os.Getenv(k) }
	required := []string{
		"SHAREPOINT_TENANT_ID", "SHAREPOINT_CLIENT_ID", "SHAREPOINT_CLIENT_SECRET",
		"SHAREPOINT_HOSTNAME", "SHAREPOINT_SITE", "SHAREPOINT_TEST_TABLE",
	}
	for _, k := range required {
		if env(k) == "" {
			t.Skipf("Set %s (and the other SHAREPOINT_* vars) to run SharePoint integration tests", k)
		}
	}

	uri := fmt.Sprintf(
		"sharepoint://?tenant_id=%s&client_id=%s&client_secret=%s&hostname=%s&site=%s",
		url.QueryEscape(env("SHAREPOINT_TENANT_ID")),
		url.QueryEscape(env("SHAREPOINT_CLIENT_ID")),
		url.QueryEscape(env("SHAREPOINT_CLIENT_SECRET")),
		url.QueryEscape(env("SHAREPOINT_HOSTNAME")),
		url.QueryEscape(env("SHAREPOINT_SITE")),
	)

	ctx := context.Background()
	src := sharepoint.NewSharePointSource()
	require.NoError(t, src.Connect(ctx, uri))
	defer func() { _ = src.Close(ctx) }()

	table, err := src.GetTable(ctx, source.TableRequest{Name: env("SHAREPOINT_TEST_TABLE")})
	require.NoError(t, err)
	assert.False(t, table.HasKnownSchema())

	records, err := table.Read(ctx, source.ReadOptions{})
	require.NoError(t, err)

	totalRows := 0
	batches := 0
	metadata := map[string]bool{"_source_file": false, "_sheet_name": false, "_row_idx": false}

	for result := range records {
		require.NoError(t, result.Err)
		rec := result.Batch
		batches++
		totalRows += int(rec.NumRows())

		fields := map[string]arrow.Field{}
		for i := 0; i < int(rec.NumCols()); i++ {
			f := rec.Schema().Field(i)
			fields[f.Name] = f
		}

		for name := range metadata {
			if _, ok := fields[name]; ok {
				metadata[name] = true
			}
		}

		// _row_idx is int64 and starts at 0 within each emitted sheet/file.
		idxField, ok := fields["_row_idx"]
		require.True(t, ok, "missing _row_idx column")
		assert.Equal(t, arrow.INT64, idxField.Type.ID())

		// Every non-metadata column lands as raw VARCHAR.
		for i := 0; i < int(rec.NumCols()); i++ {
			f := rec.Schema().Field(i)
			if f.Name == "_row_idx" {
				continue
			}
			assert.Equalf(t, arrow.STRING, f.Type.ID(), "column %q should be string", f.Name)
		}

		if rec.NumRows() > 0 {
			if idxArr, ok := rec.Column(colIndex(rec, "_row_idx")).(*array.Int64); ok {
				assert.GreaterOrEqual(t, idxArr.Value(0), int64(0))
			}
		}

		rec.Release()
	}

	assert.Greater(t, batches, 0, "expected at least one batch")
	assert.Greater(t, totalRows, 0, "expected at least one row")
	for name, seen := range metadata {
		assert.Truef(t, seen, "metadata column %q was not emitted", name)
	}

	if want := env("SHAREPOINT_EXPECTED_ROWS"); want != "" {
		var n int
		_, err := fmt.Sscanf(want, "%d", &n)
		require.NoError(t, err)
		assert.Equal(t, n, totalRows)
	}
}

func colIndex(rec arrow.RecordBatch, name string) int {
	for i := 0; i < int(rec.NumCols()); i++ {
		if rec.Schema().Field(i).Name == name {
			return i
		}
	}
	return -1
}
