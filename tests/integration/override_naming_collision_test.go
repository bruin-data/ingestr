package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/pipeline"
	_ "github.com/bruin-data/gong/pkg/source/adbc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const applovinShapedCSV = `Date,Ad Unit ID,Ad Unit Name,Waterfall,Ad Format,Placement,Country,Device Type,IDFA,IDFV,User ID,sessionId,Network,Network.Name,Revenue,Custom-Data
2026-05-05,abc123,banner_main,default,BANNER,top,US,iPhone,,fa-1,user-1,sess-1,AdMob,admob_us,1.50,extra-1
2026-05-05,def456,reward_main,default,REWARD,bottom,GB,iPad,,fa-2,user-2,sess-2,Meta,meta_uk,2.25,extra-2
2026-05-05,ghi789,inter_main,default,INTER,full,DE,iPhone,,fa-3,user-3,sess-3,Unity,unity_de,0.75,extra-3
`

func runCSVtoDuckDB(t *testing.T, csvBody string, mutate func(*config.IngestConfig)) string {
	t.Helper()
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	tmpDir := t.TempDir()

	csvPath := filepath.Join(tmpDir, "input.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(csvBody), 0o644))

	duckDBPath := filepath.Join(tmpDir, "out.duckdb")
	cfg := &config.IngestConfig{
		SourceURI:           fmt.Sprintf("csv://%s", csvPath),
		SourceTable:         "input",
		DestURI:             fmt.Sprintf("duckdb:///%s", duckDBPath),
		DestTable:           "main.input",
		IncrementalStrategy: config.StrategyReplace,
	}
	if mutate != nil {
		mutate(cfg)
	}
	require.NoError(t, cfg.Validate())
	require.NoError(t, pipeline.New(cfg).Run(ctx))
	return duckDBPath
}

func TestAppLovinAssetShape_OverridesAndTypesAppliedAcrossRenames(t *testing.T) {
	duckDBPath := runCSVtoDuckDB(t, applovinShapedCSV, func(cfg *config.IngestConfig) {
		cfg.Columns = "ad_format:string," +
			"ad_unit_id:string," +
			"ad_unit_name:string," +
			"waterfall:string," +
			"placement:string," +
			"country:string," +
			"device_type:string," +
			"idfa:string," +
			"idfv:string," +
			"user_id:string," +
			"session_id:string," +
			"network:string," +
			"network_name:string," +
			"revenue:double," +
			"custom_data:string," +
			"date:date"
	})

	types := readDuckDBColumnTypes(t, duckDBPath, "main.input")
	require.Equal(t, 16, len(types), "got: %v", types)
	assert.Equal(t, "VARCHAR", types["ad_format"])
	assert.Equal(t, "VARCHAR", types["ad_unit_id"])
	assert.Equal(t, "VARCHAR", types["device_type"])
	assert.Equal(t, "VARCHAR", types["user_id"])
	assert.Equal(t, "VARCHAR", types["session_id"])
	assert.Equal(t, "VARCHAR", types["network_name"])
	assert.Equal(t, "DOUBLE", types["revenue"])
	assert.Equal(t, "VARCHAR", types["custom_data"])
	assert.Equal(t, "DATE", types["date"])
	assert.Equal(t, 3, readDuckDBRowCount(t, duckDBPath, "main.input"))
}

func TestSnakeCase_NoDuplicateAppended(t *testing.T) {
	cases := []struct {
		name     string
		csv      string
		override string
	}{
		{"override snake matches source space", "Ad Format,Country\nINTER,US\n", "ad_format:string"},
		{"override snake matches source camelCase", "adFormat,country\nINTER,US\n", "ad_format:string"},
		{"override snake matches source hyphen", "ad-format,country\nINTER,US\n", "ad_format:string"},
		{"override snake matches source dot", "ad.format,country\nINTER,US\n", "ad_format:string"},
		{"override source-form matches space source", "Ad Format,Country\nINTER,US\n", "Ad Format:string"},
		{"override snake already matches snake source", "ad_format,country\nINTER,US\n", "ad_format:string"},
		{"override matches multi-word with extra source col", "Ad Unit ID,Country\nabc,US\n", "ad_unit_id:bigint"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			duckDBPath := runCSVtoDuckDB(t, tc.csv, func(cfg *config.IngestConfig) {
				cfg.Columns = tc.override + ",country:string"
			})
			types := readDuckDBColumnTypes(t, duckDBPath, "main.input")
			require.Equal(t, 2, len(types), "got: %v", types)
		})
	}
}

func TestSnakeCase_TrulyMissingOverrideColumnIsStillAppended(t *testing.T) {
	csv := "Ad Format,Country\nINTER,US\nBANNER,GB\n"
	duckDBPath := runCSVtoDuckDB(t, csv, func(cfg *config.IngestConfig) {
		cfg.Columns = "ad_format:string,country:string,extra_col:bigint"
	})

	types := readDuckDBColumnTypes(t, duckDBPath, "main.input")
	require.Equal(t, 3, len(types), "got: %v", types)
	assert.Equal(t, "VARCHAR", types["ad_format"])
	assert.Equal(t, "VARCHAR", types["country"])
	assert.Equal(t, "BIGINT", types["extra_col"])
}

func TestDirectNaming_OverrideInSourceFormStillWorks(t *testing.T) {
	csv := "Ad Format,Country,Revenue\nINTER,US,1.5\nBANNER,GB,2.0\n"
	duckDBPath := runCSVtoDuckDB(t, csv, func(cfg *config.IngestConfig) {
		cfg.SchemaNaming = "direct"
		cfg.Columns = "Ad Format:string,Country:string,Revenue:double"
	})

	types := readDuckDBColumnTypes(t, duckDBPath, "main.input")
	require.Equal(t, 3, len(types))
	assert.Equal(t, "DOUBLE", types["revenue"], "got: %v", types)
}

func TestInvalidSchemaNaming_FailsFast(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	tmpDir := t.TempDir()

	csvPath := filepath.Join(tmpDir, "input.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte("Ad Format,Country\nINTER,US\n"), 0o644))

	duckDBPath := filepath.Join(tmpDir, "out.duckdb")
	cfg := &config.IngestConfig{
		SourceURI:           fmt.Sprintf("csv://%s", csvPath),
		SourceTable:         "input",
		DestURI:             fmt.Sprintf("duckdb:///%s", duckDBPath),
		DestTable:           "main.input",
		IncrementalStrategy: config.StrategyReplace,
		SchemaNaming:        "bogus",
		Columns:             "ad_format:string",
	}
	if err := cfg.Validate(); err != nil {
		return
	}
	err := pipeline.New(cfg).Run(ctx)
	require.Error(t, err)
}
