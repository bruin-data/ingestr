package sendgrid

import (
	"encoding/json"
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
		want      credentials
		wantErr   bool
		errSubstr string
	}{
		{
			name: "api_key only",
			uri:  "sendgrid://?api_key=SG.abc123",
			want: credentials{apiKey: "SG.abc123"},
		},
		{
			name: "with on_behalf_of",
			uri:  "sendgrid://?api_key=SG.abc123&on_behalf_of=subuser",
			want: credentials{apiKey: "SG.abc123", onBehalfOf: "subuser"},
		},
		{
			name:      "missing api_key",
			uri:       "sendgrid://?on_behalf_of=subuser",
			wantErr:   true,
			errSubstr: "api_key is required",
		},
		{
			name:      "empty after scheme",
			uri:       "sendgrid://",
			wantErr:   true,
			errSubstr: "api_key is required",
		},
		{
			name:      "only question mark",
			uri:       "sendgrid://?",
			wantErr:   true,
			errSubstr: "api_key is required",
		},
		{
			name:      "wrong scheme",
			uri:       "postgres://?api_key=SG.abc123",
			wantErr:   true,
			errSubstr: "must start with sendgrid://",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseURI(tt.uri)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsValidTable(t *testing.T) {
	for table := range supportedTables {
		assert.True(t, isValidTable(table), "expected %q to be valid", table)
	}

	for _, table := range []string{"", "Messages", "MESSAGES", "unknown", "message", "stats"} {
		assert.False(t, isValidTable(table), "expected %q to be invalid", table)
	}
}

func TestBuildMessagesQuery(t *testing.T) {
	start := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	end := time.Date(2024, 2, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name          string
		intervalStart *time.Time
		intervalEnd   *time.Time
		want          string
	}{
		{
			name: "no interval uses default start",
			want: `last_event_time>=TIMESTAMP "1970-01-01T00:00:00Z"`,
		},
		{
			name:          "start only",
			intervalStart: &start,
			want:          `last_event_time>=TIMESTAMP "2024-01-01T10:00:00Z"`,
		},
		{
			name:        "end only",
			intervalEnd: &end,
			want:        `last_event_time<=TIMESTAMP "2024-02-01T12:00:00Z"`,
		},
		{
			name:          "both bounds use BETWEEN",
			intervalStart: &start,
			intervalEnd:   &end,
			want:          `last_event_time BETWEEN TIMESTAMP "2024-01-01T10:00:00Z" AND TIMESTAMP "2024-02-01T12:00:00Z"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildMessagesQuery(readOptions(tt.intervalStart, tt.intervalEnd))
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseTableName(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantTable string
		wantAggr  string
		wantErr   bool
		errSubstr string
	}{
		{name: "plain table", input: "lists", wantTable: "lists", wantAggr: ""},
		{name: "global_stats defaults to day", input: "global_stats", wantTable: "global_stats", wantAggr: "day"},
		{name: "global_stats week", input: "global_stats:week", wantTable: "global_stats", wantAggr: "week"},
		{name: "global_stats month", input: "global_stats:month", wantTable: "global_stats", wantAggr: "month"},
		{name: "global_stats day explicit", input: "global_stats:day", wantTable: "global_stats", wantAggr: "day"},
		{name: "invalid granularity", input: "global_stats:hour", wantErr: true, errSubstr: "invalid granularity"},
		{name: "suffix on non-stats table", input: "lists:week", wantErr: true, errSubstr: "does not support a granularity suffix"},
		{name: "unknown table", input: "contacts", wantErr: true, errSubstr: "unsupported table"},
		{name: "empty", input: "", wantErr: true, errSubstr: "unsupported table"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			table, aggr, err := parseTableName(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantTable, table)
			assert.Equal(t, tt.wantAggr, aggr)
		})
	}
}

func TestFilterByTimestamp(t *testing.T) {
	items := []map[string]interface{}{
		{"id": "1", "updated_at": "2024-01-01T00:00:00Z"},
		{"id": "2", "updated_at": "2024-06-01T00:00:00Z"},
		{"id": "3", "updated_at": "2024-12-01T00:00:00Z"},
		{"id": "4", "updated_at": ""},                        // unparseable -> excluded when filtering
		{"id": "5"},                                          // missing field -> excluded when filtering
		{"id": "6", "updated_at": json.Number("1717200000")}, // 2024-06-01 unix
	}

	start := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 9, 1, 0, 0, 0, 0, time.UTC)

	t.Run("no bounds returns all", func(t *testing.T) {
		got := filterByTimestamp(items, "updated_at", nil, nil)
		assert.Len(t, got, len(items))
	})

	t.Run("start and end filter", func(t *testing.T) {
		got := filterByTimestamp(items, "updated_at", &start, &end)
		ids := idsOf(got)
		assert.ElementsMatch(t, []string{"2", "6"}, ids)
	})

	t.Run("start only", func(t *testing.T) {
		got := filterByTimestamp(items, "updated_at", &start, nil)
		assert.ElementsMatch(t, []string{"2", "3", "6"}, idsOf(got))
	})
}

func TestResolveMessagesRange(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)

	t.Run("both bounds preserved", func(t *testing.T) {
		gotStart, gotEnd := resolveMessagesRange(source.ReadOptions{IntervalStart: &start, IntervalEnd: &end})
		assert.True(t, gotStart.Equal(start))
		assert.True(t, gotEnd.Equal(end))
	})

	t.Run("missing start defaults to epoch", func(t *testing.T) {
		gotStart, _ := resolveMessagesRange(source.ReadOptions{IntervalEnd: &end})
		assert.True(t, gotStart.Equal(time.Unix(0, 0).UTC()))
	})

	t.Run("missing end defaults to now", func(t *testing.T) {
		before := time.Now().UTC()
		_, gotEnd := resolveMessagesRange(source.ReadOptions{IntervalStart: &start})
		assert.False(t, gotEnd.Before(before))
	})
}

func TestParseItemTime(t *testing.T) {
	t.Run("rfc3339 string", func(t *testing.T) {
		got, ok := parseItemTime("2024-06-01T00:00:00Z")
		require.True(t, ok)
		assert.Equal(t, time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC), got)
	})
	t.Run("json number unix", func(t *testing.T) {
		got, ok := parseItemTime(json.Number("1717200000"))
		require.True(t, ok)
		assert.Equal(t, int64(1717200000), got.Unix())
	})
	t.Run("float unix", func(t *testing.T) {
		got, ok := parseItemTime(float64(1717200000))
		require.True(t, ok)
		assert.Equal(t, int64(1717200000), got.Unix())
	})
	t.Run("empty string", func(t *testing.T) {
		_, ok := parseItemTime("")
		assert.False(t, ok)
	})
	t.Run("unparseable string", func(t *testing.T) {
		_, ok := parseItemTime("not-a-time")
		assert.False(t, ok)
	})
	t.Run("nil", func(t *testing.T) {
		_, ok := parseItemTime(nil)
		assert.False(t, ok)
	})
}

func TestExtractItems(t *testing.T) {
	t.Run("nested key", func(t *testing.T) {
		body := map[string]interface{}{
			"result": []interface{}{
				map[string]interface{}{"id": "1"},
				map[string]interface{}{"id": "2"},
			},
		}
		got := extractItems(body, "result")
		assert.Len(t, got, 2)
	})
	t.Run("top-level array under empty key", func(t *testing.T) {
		body := map[string]interface{}{
			"": []interface{}{map[string]interface{}{"date": "2024-01-01"}},
		}
		got := extractItems(body, "")
		require.Len(t, got, 1)
		assert.Equal(t, "2024-01-01", got[0]["date"])
	})
	t.Run("missing key", func(t *testing.T) {
		assert.Nil(t, extractItems(map[string]interface{}{}, "result"))
	})
	t.Run("wrong type", func(t *testing.T) {
		assert.Nil(t, extractItems(map[string]interface{}{"result": "oops"}, "result"))
	})
}

func TestNextPageToken(t *testing.T) {
	t.Run("extracts page_token", func(t *testing.T) {
		body := map[string]interface{}{
			"_metadata": map[string]interface{}{
				"next": "https://api.sendgrid.com/v3/marketing/lists?page_size=100&page_token=ZmFrZQ",
			},
		}
		assert.Equal(t, "ZmFrZQ", nextPageToken(body))
	})
	t.Run("no metadata", func(t *testing.T) {
		assert.Equal(t, "", nextPageToken(map[string]interface{}{}))
	})
	t.Run("empty next", func(t *testing.T) {
		body := map[string]interface{}{"_metadata": map[string]interface{}{"next": ""}}
		assert.Equal(t, "", nextPageToken(body))
	})
	t.Run("next without page_token", func(t *testing.T) {
		body := map[string]interface{}{"_metadata": map[string]interface{}{"next": "https://api.sendgrid.com/v3/marketing/lists?page_size=100"}}
		assert.Equal(t, "", nextPageToken(body))
	})
}

func TestJsonUseNumber(t *testing.T) {
	t.Run("preserves large integers", func(t *testing.T) {
		var arr []map[string]interface{}
		require.NoError(t, jsonUseNumber([]byte(`[{"id": 9007199254740993}]`), &arr))
		require.Len(t, arr, 1)
		num, ok := arr[0]["id"].(json.Number)
		require.True(t, ok)
		assert.Equal(t, "9007199254740993", num.String())
	})
	t.Run("preserves floats", func(t *testing.T) {
		var body map[string]interface{}
		require.NoError(t, jsonUseNumber([]byte(`{"rate": 0.08}`), &body))
		num, ok := body["rate"].(json.Number)
		require.True(t, ok)
		assert.Equal(t, "0.08", num.String())
	})
	t.Run("invalid json", func(t *testing.T) {
		var body map[string]interface{}
		assert.Error(t, jsonUseNumber([]byte(`{not json}`), &body))
	})
}

func readOptions(start, end *time.Time) source.ReadOptions {
	return source.ReadOptions{IntervalStart: start, IntervalEnd: end}
}

func idsOf(items []map[string]interface{}) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if id, ok := item["id"].(string); ok {
			ids = append(ids, id)
		}
	}
	return ids
}
