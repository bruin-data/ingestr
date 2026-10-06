package schema

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseBigQueryPartitionBy(t *testing.T) {
	t.Parallel()

	valid := map[string]PartitionSpec{
		"created_at":                  {Column: "created_at"},
		" created_at ":                {Column: "created_at"},
		"DATE(created_at)":            {Column: "created_at", Granularity: "DAY"},
		"timestamp_trunc(`ts`, hour)": {Column: "ts", Granularity: "HOUR"},
		"DATETIME_TRUNC(dt, MONTH)":   {Column: "dt", Granularity: "MONTH"},
		"DATE_TRUNC(day, YEAR)":       {Column: "day", Granularity: "YEAR"},
		"TIMESTAMP_TRUNC( ts , DAY )": {Column: "ts", Granularity: "DAY"},
		"RANGE_BUCKET(id, GENERATE_ARRAY(0, 1000, 10))": {
			Column: "id", Range: &PartitionRange{Start: 0, End: 1000, Interval: 10},
		},
		"range_bucket(`id`, generate_array(-100, 100, 5))": {
			Column: "id", Range: &PartitionRange{Start: -100, End: 100, Interval: 5},
		},
	}
	for value, want := range valid {
		got, err := ParseBigQueryPartitionBy(value)
		require.NoError(t, err, value)
		require.Equal(t, want, got, value)
	}

	invalid := []string{
		"HOUR(ts)",
		"DATE(ts, HOUR)",
		"TIMESTAMP_TRUNC(ts)",
		"TIMESTAMP_TRUNC(ts, WEEK)",
		"RANGE_BUCKET(id, GENERATE_ARRAY(0, 1000))",
		"RANGE_BUCKET(id, GENERATE_ARRAY(0, 1000, 0))",
		"RANGE_BUCKET(id, GENERATE_ARRAY(1000, 0, 10))",
		"hour(ts)",
	}
	for _, value := range invalid {
		_, err := ParseBigQueryPartitionBy(value)
		require.Error(t, err, value)
	}
}

func TestParseIcebergPartitionBy(t *testing.T) {
	t.Parallel()

	valid := map[string]PartitionSpec{
		"created_at":         {Column: "created_at"},
		"hour(ts)":           {Column: "ts", Granularity: "HOUR"},
		"DAY(`ts`)":          {Column: "ts", Granularity: "DAY"},
		`day("eventTime")`:   {Column: "eventTime", Granularity: "DAY"},
		"months(ts)":         {Column: "ts", Granularity: "MONTH"},
		"years( ts )":        {Column: "ts", Granularity: "YEAR"},
		"bucket(16, id)":     {Column: "id", Bucket: 16},
		"bucket(id, 16)":     {Column: "id", Bucket: 16},
		"truncate(10, name)": {Column: "name", Truncate: 10},
		"truncate(name, 10)": {Column: "name", Truncate: 10},
	}
	for value, want := range valid {
		got, err := ParseIcebergPartitionBy(value)
		require.NoError(t, err, value)
		require.Equal(t, want, got, value)
	}

	for _, value := range []string{"TIMESTAMP_TRUNC(ts, HOUR)", "DATE(ts)", "bucket(0, id)", "bucket(id)", "week(ts)"} {
		_, err := ParseIcebergPartitionBy(value)
		require.Error(t, err, value)
	}
}

func TestMapPartitionColumn(t *testing.T) {
	t.Parallel()

	upper := strings.ToUpper
	require.Equal(t, "CREATED_AT", MapPartitionColumn("created_at", upper))
	require.Equal(t, "TIMESTAMP_TRUNC(UPDATEDAT, HOUR)", MapPartitionColumn("TIMESTAMP_TRUNC(updatedAt, HOUR)", upper))
	require.Equal(t, "DATE(`TS`)", MapPartitionColumn("DATE(`ts`)", upper))
	require.Equal(t, "RANGE_BUCKET(ID, GENERATE_ARRAY(0, 100, 10))", MapPartitionColumn("RANGE_BUCKET(id, GENERATE_ARRAY(0, 100, 10))", upper))
	require.Equal(t, "hour(TS)", MapPartitionColumn("hour(ts)", upper))
	require.Equal(t, "bucket(16, ID)", MapPartitionColumn("bucket(16, id)", upper))
	require.Equal(t, "truncate(NAME, 10)", MapPartitionColumn("truncate(name, 10)", upper))
	require.Equal(t, "WEEK(ts)", MapPartitionColumn("WEEK(ts)", upper))
}

func TestHasPartitionColumn(t *testing.T) {
	t.Parallel()

	ts := &TableSchema{Columns: []Column{{Name: "id"}, {Name: "Date (UTC)"}}}
	require.True(t, ts.HasPartitionColumn("Date (UTC)"))
	require.True(t, ts.HasPartitionColumn(" id "))
	require.False(t, ts.HasPartitionColumn("DATE(id)"))
	require.False(t, (*TableSchema)(nil).HasPartitionColumn("id"))
}

func TestPartitionColumn(t *testing.T) {
	t.Parallel()

	require.Equal(t, "ts", PartitionColumn("ts"))
	require.Equal(t, "ts", PartitionColumn("TIMESTAMP_TRUNC(ts, HOUR)"))
	require.Equal(t, "id", PartitionColumn("RANGE_BUCKET(id, GENERATE_ARRAY(0, 100, 10))"))
	require.Equal(t, "id", PartitionColumn("bucket(16, id)"))
	require.Empty(t, PartitionColumn("WEEK(ts)"))
}
