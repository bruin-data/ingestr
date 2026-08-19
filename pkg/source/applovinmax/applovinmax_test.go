package applovinmax

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name      string
		uri       string
		wantKey   string
		wantError bool
	}{
		{
			name:    "valid URI",
			uri:     "applovinmax://?api_key=test_key_123",
			wantKey: "test_key_123",
		},
		{
			name:    "valid URI with extra params",
			uri:     "applovinmax://?api_key=my_key&other=value",
			wantKey: "my_key",
		},
		{
			name:      "missing api_key",
			uri:       "applovinmax://?other=value",
			wantError: true,
		},
		{
			name:      "empty api_key",
			uri:       "applovinmax://?api_key=",
			wantError: true,
		},
		{
			name:      "wrong scheme",
			uri:       "applovin://?api_key=test",
			wantError: true,
		},
		{
			name:      "empty URI",
			uri:       "applovinmax://",
			wantError: true,
		},
		{
			name:      "just question mark",
			uri:       "applovinmax://?",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := parseURI(tt.uri)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantKey, key)
			}
		})
	}
}

func TestParseTableName(t *testing.T) {
	tests := []struct {
		name      string
		table     string
		wantTable string
		wantApps  []string
		wantError bool
	}{
		{
			name:      "single app",
			table:     "user_ad_revenue:com.example.app1",
			wantTable: "user_ad_revenue",
			wantApps:  []string{"com.example.app1"},
		},
		{
			name:      "multiple apps",
			table:     "user_ad_revenue:com.example.app1,com.example.app2",
			wantTable: "user_ad_revenue",
			wantApps:  []string{"com.example.app1", "com.example.app2"},
		},
		{
			name:      "apps with spaces",
			table:     "user_ad_revenue:com.example.app1, com.example.app2",
			wantTable: "user_ad_revenue",
			wantApps:  []string{"com.example.app1", "com.example.app2"},
		},
		{
			name:      "missing colon",
			table:     "user_ad_revenue",
			wantError: true,
		},
		{
			name:      "unsupported table",
			table:     "invalid_table:com.example.app1",
			wantError: true,
		},
		{
			name:      "empty app list",
			table:     "user_ad_revenue:",
			wantError: true,
		},
		{
			name:      "only commas",
			table:     "user_ad_revenue:,,,",
			wantError: true,
		},
		{
			name:      "duplicate apps",
			table:     "user_ad_revenue:com.example.app1,com.example.app1",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tableName, apps, err := parseTableName(tt.table)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantTable, tableName)
				assert.Equal(t, tt.wantApps, apps)
			}
		})
	}
}

func TestIsValidTable(t *testing.T) {
	assert.True(t, isValidTable("user_ad_revenue"))
	assert.False(t, isValidTable("invalid"))
	assert.False(t, isValidTable(""))
	assert.False(t, isValidTable("USER_AD_REVENUE"))
}

func TestResolveDateRange(t *testing.T) {
	now := time.Now().UTC()

	t.Run("both provided", func(t *testing.T) {
		start := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)
		s, e := resolveDateRange(&start, &end)
		assert.Equal(t, start, s)
		assert.Equal(t, end, e)
	})

	t.Run("nil start defaults to 30 days ago", func(t *testing.T) {
		end := time.Date(2027, 6, 15, 0, 0, 0, 0, time.UTC)
		s, e := resolveDateRange(nil, &end)
		expected := truncateToDate(now.AddDate(0, 0, -defaultDays))
		assert.Equal(t, expected, s)
		assert.Equal(t, end, e)
	})

	t.Run("nil end defaults to yesterday", func(t *testing.T) {
		start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		s, e := resolveDateRange(&start, nil)
		assert.Equal(t, start, s)
		yesterday := truncateToDate(now.AddDate(0, 0, -1))
		assert.Equal(t, yesterday, e)
	})

	t.Run("both nil", func(t *testing.T) {
		s, e := resolveDateRange(nil, nil)
		expected := truncateToDate(now.AddDate(0, 0, -defaultDays))
		assert.Equal(t, expected, s)
		yesterday := truncateToDate(now.AddDate(0, 0, -1))
		assert.Equal(t, yesterday, e)
	})

	t.Run("end before start gets clamped", func(t *testing.T) {
		start := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)
		end := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
		s, e := resolveDateRange(&start, &end)
		assert.Equal(t, start, s)
		assert.Equal(t, start, e)
	})

	t.Run("timestamps truncated to date", func(t *testing.T) {
		start := time.Date(2024, 6, 1, 14, 30, 45, 0, time.UTC)
		end := time.Date(2024, 6, 15, 22, 10, 5, 0, time.UTC)
		s, e := resolveDateRange(&start, &end)
		assert.Equal(t, time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC), s)
		assert.Equal(t, time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC), e)
	})
}

func TestParseTimestamp(t *testing.T) {
	now := time.Now()

	assert.Equal(t, now, parseTimestamp(now))
	assert.Equal(t, now, parseTimestamp(&now))
	assert.True(t, parseTimestamp(nil).IsZero())
	assert.True(t, parseTimestamp((*time.Time)(nil)).IsZero())
}

func TestTryParseNumeric(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want any
	}{
		{"integer", "42", json.Number("42")},
		{"negative integer", "-7", json.Number("-7")},
		{"float", "0.010910", json.Number("0.010910")},
		{"negative float", "-3.14", json.Number("-3.14")},
		{"scientific", "1e6", json.Number("1e6")},
		{"plain string", "hello", "hello"},
		{"empty string", "", ""},
		{"uuid", "032c6e1b-b6f0-4663-a377-05218cee2ca9", "032c6e1b-b6f0-4663-a377-05218cee2ca9"},
		{"date string", "2026-03-24", "2026-03-24"},
		{"zero", "0", json.Number("0")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tryParseNumeric(tt.in)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestStreamCSVBatchesRows(t *testing.T) {
	input := strings.NewReader("event_id,revenue\n1,0.1\n2,0.2\n3,0.3\n4,0.4\n5,0.5\n")
	results := make(chan source.RecordBatchResult, 3)

	rows, err := streamCSV(
		context.Background(),
		input,
		"2026-08-16",
		"ios",
		source.ReadOptions{PageSize: 2},
		newRowLimiter(0),
		results,
	)
	require.NoError(t, err)
	assert.Equal(t, 5, rows)

	close(results)
	var batchRows []int64
	for result := range results {
		require.NoError(t, result.Err)
		require.NotNil(t, result.Batch)
		batchRows = append(batchRows, result.Batch.NumRows())
		fields := result.Batch.Schema().Fields()
		fieldNames := make([]string, len(fields))
		for i, field := range fields {
			fieldNames[i] = field.Name
		}
		assert.Equal(t, []string{"partition_date", "event_id", "platform", "revenue"}, fieldNames)
		result.Batch.Release()
	}
	assert.Equal(t, []int64{2, 2, 1}, batchRows)
}

func TestStreamCSVHonorsLimitAcrossBatches(t *testing.T) {
	input := strings.NewReader("event_id\n1\n2\n3\n4\n5\n6\n7\n")
	results := make(chan source.RecordBatchResult, 2)
	limiter := newRowLimiter(5)

	rows, err := streamCSV(
		context.Background(),
		input,
		"2026-08-16",
		"android",
		source.ReadOptions{PageSize: 3},
		limiter,
		results,
	)
	require.NoError(t, err)
	assert.Equal(t, 5, rows)
	assert.True(t, limiter.exhausted())

	close(results)
	var batchRows []int64
	for result := range results {
		require.NoError(t, result.Err)
		batchRows = append(batchRows, result.Batch.NumRows())
		result.Batch.Release()
	}
	assert.Equal(t, []int64{3, 2}, batchRows)
}

func TestStreamCSVEmitsBeforeInputCompletes(t *testing.T) {
	reader, writer := io.Pipe()
	results := make(chan source.RecordBatchResult)
	finished := make(chan struct {
		rows int
		err  error
	}, 1)

	go func() {
		rows, err := streamCSV(
			context.Background(),
			reader,
			"2026-08-16",
			"fireos",
			source.ReadOptions{PageSize: 2},
			newRowLimiter(0),
			results,
		)
		finished <- struct {
			rows int
			err  error
		}{rows: rows, err: err}
	}()

	_, err := io.WriteString(writer, "event_id,revenue\n1,0.1\n2,0.2\n")
	require.NoError(t, err)

	select {
	case result := <-results:
		require.NoError(t, result.Err)
		require.EqualValues(t, 2, result.Batch.NumRows())
		result.Batch.Release()
	case <-time.After(time.Second):
		t.Fatal("expected a batch before the CSV input was closed")
	}

	require.NoError(t, writer.Close())
	completion := <-finished
	require.NoError(t, completion.err)
	assert.Equal(t, 2, completion.rows)
}

func TestEffectiveBatchLimitsCapsProductionDefaults(t *testing.T) {
	rows, bytes := effectiveBatchLimits(source.ReadOptions{
		PageSize:      25_000,
		MaxBatchBytes: 512 << 20,
	})
	assert.Equal(t, maxBatchRows, rows)
	assert.EqualValues(t, maxBatchBytes, bytes)

	rows, bytes = effectiveBatchLimits(source.ReadOptions{
		PageSize:      100,
		MaxBatchBytes: 1 << 20,
	})
	assert.Equal(t, 100, rows)
	assert.EqualValues(t, 1<<20, bytes)
}
