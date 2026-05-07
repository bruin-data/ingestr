package chess

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/arrowconv"
	gonghttp "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePlayersFromURI(t *testing.T) {
	tests := []struct {
		name     string
		uri      string
		expected []string
		wantErr  bool
	}{
		{
			name:     "with players parameter",
			uri:      "chess://?players=hikaru,magnuscarlsen",
			expected: []string{"hikaru", "magnuscarlsen"},
		},
		{
			name:     "single player",
			uri:      "chess://?players=hikaru",
			expected: []string{"hikaru"},
		},
		{
			name:     "empty URI uses defaults",
			uri:      "chess://",
			expected: []string{"hikaru", "magnuscarlsen", "gothamchess", "fabianocaruana"},
		},
		{
			name:     "empty players uses defaults",
			uri:      "chess://?players=",
			expected: []string{"hikaru", "magnuscarlsen", "gothamchess", "fabianocaruana"},
		},
		{
			name:     "with spaces",
			uri:      "chess://?players=hikaru, magnuscarlsen , gothamchess",
			expected: []string{"hikaru", "magnuscarlsen", "gothamchess"},
		},
		{
			name:    "invalid URI",
			uri:     "http://example.com",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePlayersFromURI(tt.uri)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestChessSource_Schemes(t *testing.T) {
	s := NewChessSource()
	assert.Equal(t, []string{"chess"}, s.Schemes())
}

func TestChessSource_GetTable(t *testing.T) {
	s := NewChessSource()
	err := s.Connect(context.Background(), "chess://?players=testuser")
	require.NoError(t, err)

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "profiles"})
	require.NoError(t, err)
	assert.NotNil(t, table)
	assert.Equal(t, "profiles", table.Name())
	assert.False(t, table.HasKnownSchema())
}

func TestChessSource_Connect(t *testing.T) {
	s := NewChessSource()
	err := s.Connect(context.Background(), "chess://?players=hikaru,magnus")
	require.NoError(t, err)
	assert.Equal(t, []string{"hikaru", "magnus"}, s.players)
}

func TestChessSource_ReadProfiles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/player/testuser" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"username":    "testuser",
				"player_id":   12345,
				"title":       "GM",
				"status":      "premium",
				"name":        "Test User",
				"avatar":      "https://example.com/avatar.jpg",
				"location":    "Test City",
				"country":     "https://api.chess.com/pub/country/US",
				"joined":      1234567890,
				"last_online": 1234567899,
				"followers":   1000,
				"is_streamer": false,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	s := NewChessSource()
	err := s.Connect(context.Background(), "chess://?players=testuser")
	require.NoError(t, err)
	// Replace the client with one pointing to our test server
	_ = s.client.Close()
	s.client = gonghttp.New(
		gonghttp.WithBaseURL(server.URL),
		gonghttp.WithDisableRetry(),
	)
	defer func() { _ = s.client.Close() }()

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "profiles"})
	require.NoError(t, err)

	results, err := table.Read(context.Background(), source.ReadOptions{})
	require.NoError(t, err)

	var batches []source.RecordBatchResult
	for result := range results {
		batches = append(batches, result)
	}

	require.Len(t, batches, 1)
	require.NoError(t, batches[0].Err)
	require.NotNil(t, batches[0].Batch)

	batch := batches[0].Batch
	assert.Equal(t, int64(1), batch.NumRows())
}

func TestChessSource_ReadUnsupportedTable(t *testing.T) {
	s := NewChessSource()
	err := s.Connect(context.Background(), "chess://?players=testuser")
	require.NoError(t, err)

	_, err = s.GetTable(context.Background(), source.TableRequest{Name: "invalid"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported table")
}

func TestItemsToArrowRecordWithSchema(t *testing.T) {
	items := []map[string]interface{}{
		{
			"string_field": "value1",
			"int_field":    float64(42),
			"bool_field":   true,
		},
		{
			"string_field": "value2",
			"int_field":    float64(43),
			"bool_field":   false,
		},
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, record)

	assert.Equal(t, int64(2), record.NumRows())
	assert.Equal(t, 3, int(record.NumCols()))
}

func TestItemsToArrowRecordWithSchema_WithExcludeColumns(t *testing.T) {
	items := []map[string]interface{}{
		{
			"keep_field":    "value1",
			"exclude_field": "should_not_appear",
		},
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, []string{"exclude_field"})
	require.NoError(t, err)
	require.NotNil(t, record)

	assert.Equal(t, 1, int(record.NumCols()))

	hasExcluded := false
	for i := 0; i < int(record.NumCols()); i++ {
		if record.ColumnName(i) == "exclude_field" {
			hasExcluded = true
		}
	}
	assert.False(t, hasExcluded)
}

func TestItemsToArrowRecordWithSchema_EmptyItems(t *testing.T) {
	record, err := arrowconv.ItemsToArrowRecordWithSchema([]map[string]interface{}{}, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(0), record.NumRows())
}

func TestParseArchiveDate(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantYear  int
		wantMonth int
		wantOk    bool
	}{
		{
			name:      "valid archive URL",
			url:       "https://api.chess.com/pub/player/hikaru/games/2024/01",
			wantYear:  2024,
			wantMonth: 1,
			wantOk:    true,
		},
		{
			name:      "valid archive URL December",
			url:       "https://api.chess.com/pub/player/hikaru/games/2023/12",
			wantYear:  2023,
			wantMonth: 12,
			wantOk:    true,
		},
		{
			name:   "invalid URL",
			url:    "https://api.chess.com/pub/player/hikaru",
			wantOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			year, month, ok := parseArchiveDate(tt.url)
			assert.Equal(t, tt.wantOk, ok)
			if ok {
				assert.Equal(t, tt.wantYear, year)
				assert.Equal(t, tt.wantMonth, month)
			}
		})
	}
}

func TestIsArchiveInInterval(t *testing.T) {
	jan2024 := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	mar2024 := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name          string
		archiveURL    string
		intervalStart interface{}
		intervalEnd   interface{}
		expected      bool
	}{
		{
			name:          "no interval - include all",
			archiveURL:    "https://api.chess.com/pub/player/hikaru/games/2024/02",
			intervalStart: nil,
			intervalEnd:   nil,
			expected:      true,
		},
		{
			name:          "within interval",
			archiveURL:    "https://api.chess.com/pub/player/hikaru/games/2024/02",
			intervalStart: &jan2024,
			intervalEnd:   &mar2024,
			expected:      true,
		},
		{
			name:          "before interval start",
			archiveURL:    "https://api.chess.com/pub/player/hikaru/games/2023/12",
			intervalStart: &jan2024,
			intervalEnd:   nil,
			expected:      false,
		},
		{
			name:          "after interval end",
			archiveURL:    "https://api.chess.com/pub/player/hikaru/games/2024/05",
			intervalStart: nil,
			intervalEnd:   &mar2024,
			expected:      false,
		},
		{
			name:          "exactly at interval start month",
			archiveURL:    "https://api.chess.com/pub/player/hikaru/games/2024/01",
			intervalStart: &jan2024,
			intervalEnd:   nil,
			expected:      true,
		},
		{
			name:          "exactly at interval end month",
			archiveURL:    "https://api.chess.com/pub/player/hikaru/games/2024/03",
			intervalStart: nil,
			intervalEnd:   &mar2024,
			expected:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isArchiveInInterval(tt.archiveURL, tt.intervalStart, tt.intervalEnd)
			assert.Equal(t, tt.expected, result)
		})
	}
}
