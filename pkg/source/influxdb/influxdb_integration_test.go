//go:build integration

package influxdb_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/testutil"
	"github.com/bruin-data/ingestr/pkg/schema"
)

func TestInfluxDBPipeline_V2Flux(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	host := os.Getenv("INFLUXDB_HOST")
	token := os.Getenv("INFLUXDB_TOKEN")
	org := os.Getenv("INFLUXDB_ORG")
	bucket := os.Getenv("INFLUXDB_BUCKET")

	if host == "" || token == "" || org == "" || bucket == "" {
		t.Skip("Set INFLUXDB_HOST, INFLUXDB_TOKEN, INFLUXDB_ORG, INFLUXDB_BUCKET to run InfluxDB integration tests")
	}

	ctx := context.Background()
	cleanHost := strings.TrimPrefix(strings.TrimPrefix(host, "https://"), "http://")
	sourceURI := fmt.Sprintf("influxdb://%s?token=%s&org=%s&bucket=%s",
		cleanHost, token, org, bucket)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("influxdb_v2_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	now := time.Now().UTC()
	intervalStart := now.Add(-24 * time.Hour)
	intervalEnd := now

	// v2 Flux returns long/unpivoted format: time, measurement, field, value, plus tag columns.
	// Each field becomes a separate row, so row counts are higher than v3.
	expectations := []testutil.TableExpectation{
		{
			SourceTable:         "cpu",
			DestTable:           "main.influxdb_cpu",
			MinExpectedRowCount: 27,
			IntervalStart:       &intervalStart,
			IntervalEnd:         &intervalEnd,
			ExpectedSchema: []schema.Column{
				{Name: "time", DataType: schema.TypeTimestampTZ},
				{Name: "field", DataType: schema.TypeString},
				{Name: "measurement", DataType: schema.TypeString},
				{Name: "value", DataType: schema.TypeFloat64},
				{Name: "host", DataType: schema.TypeString},
				{Name: "region", DataType: schema.TypeString},
			},
		},
		{
			SourceTable:         "mem",
			DestTable:           "main.influxdb_mem",
			MinExpectedRowCount: 17,
			IntervalStart:       &intervalStart,
			IntervalEnd:         &intervalEnd,
			ExpectedSchema: []schema.Column{
				{Name: "time", DataType: schema.TypeTimestampTZ},
				{Name: "field", DataType: schema.TypeString},
				{Name: "measurement", DataType: schema.TypeString},
				{Name: "value", DataType: schema.TypeInt64},
				{Name: "host", DataType: schema.TypeString},
				{Name: "region", DataType: schema.TypeString},
			},
		},
		{
			SourceTable:         "disk",
			DestTable:           "main.influxdb_disk",
			MinExpectedRowCount: 7,
			IntervalStart:       &intervalStart,
			IntervalEnd:         &intervalEnd,
			ExpectedSchema: []schema.Column{
				{Name: "time", DataType: schema.TypeTimestampTZ},
				{Name: "field", DataType: schema.TypeString},
				{Name: "measurement", DataType: schema.TypeString},
				{Name: "value", DataType: schema.TypeInt64},
				{Name: "host", DataType: schema.TypeString},
				{Name: "region", DataType: schema.TypeString},
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

func TestInfluxDBPipeline_V3SQL(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	host := os.Getenv("INFLUXDB_HOST")
	token := os.Getenv("INFLUXDB_TOKEN")
	org := os.Getenv("INFLUXDB_ORG")
	bucket := os.Getenv("INFLUXDB_BUCKET")

	if host == "" || token == "" || org == "" || bucket == "" {
		t.Skip("Set INFLUXDB_HOST, INFLUXDB_TOKEN, INFLUXDB_ORG, INFLUXDB_BUCKET to run InfluxDB integration tests")
	}

	ctx := context.Background()
	cleanHost := strings.TrimPrefix(strings.TrimPrefix(host, "https://"), "http://")
	sourceURI := fmt.Sprintf("influxdb://%s?token=%s&org=%s&bucket=%s&influxdb3=true",
		cleanHost, token, org, bucket)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("influxdb_v3_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	now := time.Now().UTC()
	intervalStart := now.Add(-24 * time.Hour)
	intervalEnd := now

	// v3 SQL returns pivoted/wide format: fields as columns, one row per timestamp+tags.
	expectations := []testutil.TableExpectation{
		{
			SourceTable:         "cpu",
			DestTable:           "main.influxdb_cpu",
			MinExpectedRowCount: 6,
			IntervalStart:       &intervalStart,
			IntervalEnd:         &intervalEnd,
			ExpectedSchema: []schema.Column{
				{Name: "time", DataType: schema.TypeTimestampTZ},
				{Name: "host", DataType: schema.TypeString},
				{Name: "region", DataType: schema.TypeString},
				{Name: "usage_idle", DataType: schema.TypeFloat64},
				{Name: "usage_system", DataType: schema.TypeFloat64},
				{Name: "usage_user", DataType: schema.TypeFloat64},
			},
		},
		{
			SourceTable:         "mem",
			DestTable:           "main.influxdb_mem",
			MinExpectedRowCount: 4,
			IntervalStart:       &intervalStart,
			IntervalEnd:         &intervalEnd,
			ExpectedSchema: []schema.Column{
				{Name: "time", DataType: schema.TypeTimestampTZ},
				{Name: "host", DataType: schema.TypeString},
				{Name: "region", DataType: schema.TypeString},
				{Name: "free", DataType: schema.TypeInt64},
				{Name: "total", DataType: schema.TypeInt64},
				{Name: "used", DataType: schema.TypeInt64},
			},
		},
		{
			SourceTable:         "disk",
			DestTable:           "main.influxdb_disk",
			MinExpectedRowCount: 3,
			IntervalStart:       &intervalStart,
			IntervalEnd:         &intervalEnd,
			ExpectedSchema: []schema.Column{
				{Name: "time", DataType: schema.TypeTimestampTZ},
				{Name: "host", DataType: schema.TypeString},
				{Name: "region", DataType: schema.TypeString},
				{Name: "free", DataType: schema.TypeInt64},
				{Name: "total", DataType: schema.TypeInt64},
				{Name: "used", DataType: schema.TypeInt64},
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
