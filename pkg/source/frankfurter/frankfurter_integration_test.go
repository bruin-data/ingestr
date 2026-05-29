//go:build integration

package frankfurter_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/testutil"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/bruin-data/ingestr/pkg/schema"
	_ "github.com/bruin-data/ingestr/pkg/source/adbc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFrankfurterPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	sourceURI := "frankfurter://?base=EUR"

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("frankfurter_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	start := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	expectations := []testutil.TableExpectation{
		{
			SourceTable: "currencies",
			DestTable:   "main.frankfurter_currencies",
			KeyColumn:   "currency_code",
			ExpectedSchema: []schema.Column{
				{Name: "currency_code", DataType: schema.TypeString},
				{Name: "currency_name", DataType: schema.TypeString},
			},
			ExpectedRowCount: 30,
			Rows: []testutil.ExpectedRow{
				{
					ID: "EUR",
					Fields: map[string]any{
						"currency_name": "Euro",
					},
				},
				{
					ID: "USD",
					Fields: map[string]any{
						"currency_name": "United States Dollar",
					},
				},
				{
					ID: "GBP",
					Fields: map[string]any{
						"currency_name": "British Pound",
					},
				},
			},
		},
		{
			SourceTable:   "exchange_rates",
			DestTable:     "main.frankfurter_exchange_rates",
			KeyColumn:     "currency_code",
			IntervalStart: &start,
			IntervalEnd:   &end,
			ExpectedSchema: []schema.Column{
				{Name: "date", DataType: schema.TypeDate},
				{Name: "currency_code", DataType: schema.TypeString},
				{Name: "base_currency", DataType: schema.TypeString},
				{Name: "rate", DataType: schema.TypeFloat64},
			},
			ExpectedRowCount: 31,
			Rows: []testutil.ExpectedRow{
				{
					ID: "EUR",
					Fields: map[string]any{
						"base_currency": "EUR",
						"rate":          1.0,
					},
				},
				{
					ID: "USD",
					Fields: map[string]any{
						"base_currency": "EUR",
						"rate":          1.0956,
					},
				},
				{
					ID: "GBP",
					Fields: map[string]any{
						"base_currency": "EUR",
						"rate":          0.86645,
					},
				},
				{
					ID: "JPY",
					Fields: map[string]any{
						"base_currency": "EUR",
						"rate":          155.68,
					},
				},
				{
					ID: "CHF",
					Fields: map[string]any{
						"base_currency": "EUR",
						"rate":          0.9305,
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

func TestFrankfurterPipeline_USDBase(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	sourceURI := "frankfurter://?base=USD"

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("frankfurter_usd_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	start := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	exp := testutil.TableExpectation{
		SourceTable:   "exchange_rates",
		DestTable:     "main.frankfurter_usd_exchange_rates",
		KeyColumn:     "currency_code",
		IntervalStart: &start,
		IntervalEnd:   &end,
		ExpectedSchema: []schema.Column{
			{Name: "date", DataType: schema.TypeDate},
			{Name: "currency_code", DataType: schema.TypeString},
			{Name: "base_currency", DataType: schema.TypeString},
			{Name: "rate", DataType: schema.TypeFloat64},
		},
		ExpectedRowCount: 31,
		Rows: []testutil.ExpectedRow{
			{
				ID: "USD",
				Fields: map[string]any{
					"base_currency": "USD",
					"rate":          1.0,
				},
			},
			{
				ID: "EUR",
				Fields: map[string]any{
					"base_currency": "USD",
					"rate":          0.91274,
				},
			},
			{
				ID: "GBP",
				Fields: map[string]any{
					"base_currency": "USD",
					"rate":          0.79085,
				},
			},
			{
				ID: "JPY",
				Fields: map[string]any{
					"base_currency": "USD",
					"rate":          142.1,
				},
			},
			{
				ID: "CHF",
				Fields: map[string]any{
					"base_currency": "USD",
					"rate":          0.84931,
				},
			},
		},
	}

	t.Run("exchange_rates_usd_base", func(t *testing.T) {
		testutil.RunPipeline(t, ctx, sourceURI, destURI, exp)
		testutil.Check(t, destURI, exp)
	})
}

func TestFrankfurterPipeline_Incremental(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	sourceURI := "frankfurter://?base=EUR"
	destTable := "main.frankfurter_incremental_rates"

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("frankfurter_incr_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	// Run 1: load 2024-01-02 only
	start1 := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	end1 := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	cfg1 := &config.IngestConfig{
		SourceURI:           sourceURI,
		DestURI:             destURI,
		SourceTable:         "exchange_rates",
		DestTable:           destTable,
		IncrementalStrategy: config.StrategyMerge,
		IntervalStart:       &start1,
		IntervalEnd:         &end1,
	}
	require.NoError(t, pipeline.New(cfg1).Run(ctx))

	// Verify run 1: 31 rows (30 currencies + EUR base row)
	{
		db := openTestDB(t, destURI)
		var count1 int
		require.NoError(t, db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", destTable)).Scan(&count1))
		assert.Equal(t, 31, count1, "first run should have 31 rows")
		_ = db.Close()
	}

	// Run 2: load 2024-01-02..2024-01-03 (overlapping with run 1)
	start2 := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	end2 := time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)

	cfg2 := &config.IngestConfig{
		SourceURI:           sourceURI,
		DestURI:             destURI,
		SourceTable:         "exchange_rates",
		DestTable:           destTable,
		IncrementalStrategy: config.StrategyMerge,
		IntervalStart:       &start2,
		IntervalEnd:         &end2,
	}
	require.NoError(t, pipeline.New(cfg2).Run(ctx))

	// Open a fresh connection after run 2 (DuckDB requires new connection to see committed changes)
	db := openTestDB(t, destURI)
	defer func() { _ = db.Close() }()

	// Verify run 2: 62 rows (31 per date, no duplicates from overlap)
	var count2 int
	require.NoError(t, db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", destTable)).Scan(&count2))
	assert.Equal(t, 62, count2, "after incremental run should have 62 rows (31 per date)")

	// Verify no duplicate (date, currency_code, base_currency) combinations
	var distinctCount int
	require.NoError(t, db.QueryRow(fmt.Sprintf(
		"SELECT COUNT(*) FROM (SELECT DISTINCT date, currency_code, base_currency FROM %s)", destTable,
	)).Scan(&distinctCount))
	assert.Equal(t, count2, distinctCount, "should have no duplicate primary key combinations")

	// Verify both dates exist
	var dateCount int
	require.NoError(t, db.QueryRow(fmt.Sprintf("SELECT COUNT(DISTINCT date) FROM %s", destTable)).Scan(&dateCount))
	assert.Equal(t, 2, dateCount, "should have 2 distinct dates")

	// Verify USD rate for 2024-01-02 (should not be overwritten by run 2)
	var usdRate0102 float64
	require.NoError(t, db.QueryRow(fmt.Sprintf(
		"SELECT rate FROM %s WHERE currency_code = 'USD' AND date = '2024-01-02'", destTable,
	)).Scan(&usdRate0102))
	assert.InDelta(t, 1.0956, usdRate0102, 0.001, "USD rate for 2024-01-02")

	// Verify USD rate for 2024-01-03 (new data from run 2)
	var usdRate0103 float64
	require.NoError(t, db.QueryRow(fmt.Sprintf(
		"SELECT rate FROM %s WHERE currency_code = 'USD' AND date = '2024-01-03'", destTable,
	)).Scan(&usdRate0103))
	assert.InDelta(t, 1.0919, usdRate0103, 0.001, "USD rate for 2024-01-03")

	// Verify base currency rows exist for both dates with rate 1.0
	var eurRate0102, eurRate0103 float64
	require.NoError(t, db.QueryRow(fmt.Sprintf(
		"SELECT rate FROM %s WHERE currency_code = 'EUR' AND date = '2024-01-02'", destTable,
	)).Scan(&eurRate0102))
	require.NoError(t, db.QueryRow(fmt.Sprintf(
		"SELECT rate FROM %s WHERE currency_code = 'EUR' AND date = '2024-01-03'", destTable,
	)).Scan(&eurRate0103))
	assert.Equal(t, 1.0, eurRate0102, "EUR base rate should be 1.0 for 2024-01-02")
	assert.Equal(t, 1.0, eurRate0103, "EUR base rate should be 1.0 for 2024-01-03")
}

func openTestDB(t *testing.T, destURI string) *sql.DB {
	t.Helper()
	path := fmt.Sprintf("driver=duckdb;path=%s", destURI[len("duckdb:///"):])
	db, err := sql.Open("adbc_generic", path)
	require.NoError(t, err)
	return db
}
