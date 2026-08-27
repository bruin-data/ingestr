//go:build integration

package airtable_test

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/testutil"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/source/airtable"
	"github.com/stretchr/testify/require"
)

func TestAirtablePipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	accessToken := os.Getenv("AIRTABLE_ACCESS_TOKEN")
	baseID := os.Getenv("AIRTABLE_BASE_ID")
	if accessToken == "" || baseID == "" {
		t.Skip("Set AIRTABLE_ACCESS_TOKEN and AIRTABLE_BASE_ID to run Airtable integration tests")
	}

	ctx := context.Background()
	sourceURI := fmt.Sprintf("airtable://?access_token=%s", accessToken)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("airtable_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	expectations := []testutil.TableExpectation{
		{
			SourceTable:      baseID + "/Table 1",
			DestTable:        "main.airtable_table_1",
			KeyColumn:        "id",
			ExpectedRowCount: 3,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "createdtime", DataType: schema.TypeTimestampTZ},
				{Name: "fields__name", DataType: schema.TypeString},
				{Name: "fields__notes", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "recAC8CMuJNfN4fpk",
					Fields: map[string]any{
						"fields__name":  "Alice Johnson",
						"fields__notes": "Engineering lead",
					},
				},
				{
					ID: "recM8e1jCFJJqOXu2",
					Fields: map[string]any{
						"fields__name":  "Bob Smith",
						"fields__notes": "Product manager",
					},
				},
				{
					ID: "recdfDnfEVAITJoPk",
					Fields: map[string]any{
						"fields__name":  "Charlie Brown",
						"fields__notes": "Designer",
					},
				},
			},
		},
	}

	for _, exp := range expectations {
		t.Run(exp.SourceTable, func(t *testing.T) {
			testutil.RunPipeline(t, ctx, sourceURI, destURI, exp)
			testutil.Check(t, destURI, exp)
		})
	}
}

func TestAirtableByteLimitedBatches(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	accessToken := os.Getenv("AIRTABLE_ACCESS_TOKEN")
	baseID := os.Getenv("AIRTABLE_BASE_ID")
	if accessToken == "" || baseID == "" {
		t.Skip("Set AIRTABLE_ACCESS_TOKEN and AIRTABLE_BASE_ID to run Airtable integration tests")
	}

	ctx := context.Background()
	sourceURI := fmt.Sprintf(
		"airtable://?access_token=%s&base_id=%s",
		url.QueryEscape(accessToken),
		url.QueryEscape(baseID),
	)

	airtableSource := airtable.NewAirtableSource()
	require.NoError(t, airtableSource.Connect(ctx, sourceURI))
	t.Cleanup(func() {
		require.NoError(t, airtableSource.Close(ctx))
	})

	table, err := airtableSource.GetTable(ctx, source.TableRequest{Name: "Table 1"})
	require.NoError(t, err)

	uncapped, uncappedBatchRows := readAirtableTable(t, ctx, table, source.ReadOptions{})
	defer uncapped.Release()
	capped, cappedBatchRows := readAirtableTable(t, ctx, table, source.ReadOptions{MaxBatchBytes: 1})
	defer capped.Release()

	require.True(t, array.TableEqual(uncapped, capped))
	require.Equal(t, int64(3), uncapped.NumRows())
	require.Equal(t, []int64{3}, uncappedBatchRows)
	require.Equal(t, []int64{1, 1, 1}, cappedBatchRows)
}

func readAirtableTable(
	t *testing.T,
	ctx context.Context,
	table source.SourceTable,
	opts source.ReadOptions,
) (arrow.Table, []int64) {
	t.Helper()

	results, err := table.Read(ctx, opts)
	require.NoError(t, err)

	var records []arrow.RecordBatch
	defer func() {
		for _, record := range records {
			record.Release()
		}
	}()

	var batchRows []int64
	for result := range results {
		require.NoError(t, result.Err)
		require.NotNil(t, result.Batch)
		records = append(records, result.Batch)
		batchRows = append(batchRows, result.Batch.NumRows())
	}

	require.NotEmpty(t, records)
	return array.NewTableFromRecords(records[0].Schema(), records), batchRows
}
