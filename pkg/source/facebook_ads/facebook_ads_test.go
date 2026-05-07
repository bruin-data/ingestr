package facebook_ads

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name            string
		uri             string
		wantAccessToken string
		wantAccountID   string
		wantErr         bool
	}{
		{
			name:            "valid with both params",
			uri:             "facebookads://?access_token=abc123&account_id=1234567890",
			wantAccessToken: "abc123",
			wantAccountID:   "act_1234567890",
		},
		{
			name:            "valid with act_ prefix already",
			uri:             "facebookads://?access_token=abc123&account_id=act_1234567890",
			wantAccessToken: "abc123",
			wantAccountID:   "act_1234567890",
		},
		{
			name:            "valid without account_id",
			uri:             "facebookads://?access_token=abc123",
			wantAccessToken: "abc123",
			wantAccountID:   "",
		},
		{
			name:    "missing access_token",
			uri:     "facebookads://?account_id=123",
			wantErr: true,
		},
		{
			name:    "missing query params",
			uri:     "facebookads://",
			wantErr: true,
		},
		{
			name:    "wrong scheme",
			uri:     "postgres://localhost",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			accessToken, accountID, err := parseURI(tt.uri)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantAccessToken, accessToken)
			assert.Equal(t, tt.wantAccountID, accountID)
		})
	}
}

func TestParseTableName(t *testing.T) {
	tests := []struct {
		raw            string
		wantTable      string
		wantAccountIDs []string
	}{
		{"campaigns", "campaigns", nil},
		{"campaigns:1234567890", "campaigns", []string{"1234567890"}},
		{"campaigns:1234567890,9876543210", "campaigns", []string{"1234567890", "9876543210"}},
		{"ad_sets:act_9999", "ad_sets", []string{"act_9999"}},
		{"facebook_insights", "facebook_insights", nil},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			table, accountIDs := parseTableName(tt.raw)
			assert.Equal(t, tt.wantTable, table)
			assert.Equal(t, tt.wantAccountIDs, accountIDs)
		})
	}
}

func TestResolveAccountIDs(t *testing.T) {
	tests := []struct {
		name            string
		uriAccountID    string
		tableAccountIDs []string
		want            []string
	}{
		{
			name:            "table IDs take priority",
			uriAccountID:    "act_111",
			tableAccountIDs: []string{"222"},
			want:            []string{"act_222"},
		},
		{
			name:            "multiple table IDs",
			uriAccountID:    "act_111",
			tableAccountIDs: []string{"222", "333"},
			want:            []string{"act_222", "act_333"},
		},
		{
			name:            "falls back to URI",
			uriAccountID:    "act_111",
			tableAccountIDs: nil,
			want:            []string{"act_111"},
		},
		{
			name:            "adds act_ prefix",
			uriAccountID:    "",
			tableAccountIDs: []string{"333"},
			want:            []string{"act_333"},
		},
		{
			name:            "preserves act_ prefix",
			uriAccountID:    "",
			tableAccountIDs: []string{"act_444"},
			want:            []string{"act_444"},
		},
		{
			name:            "empty when neither provided",
			uriAccountID:    "",
			tableAccountIDs: nil,
			want:            []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &FacebookAdsSource{accountID: tt.uriAccountID}
			got := s.resolveAccountIDs(tt.tableAccountIDs)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsValidTable(t *testing.T) {
	assert.True(t, isValidTable("campaigns"))
	assert.True(t, isValidTable("ad_sets"))
	assert.True(t, isValidTable("ads"))
	assert.True(t, isValidTable("ad_creatives"))
	assert.True(t, isValidTable("leads"))
	assert.True(t, isValidTable("facebook_insights"))
	assert.False(t, isValidTable("unknown"))
	assert.False(t, isValidTable(""))
}

func TestGetTable_AccountIDRequired(t *testing.T) {
	s := &FacebookAdsSource{accountID: ""}

	_, err := s.GetTable(context.Background(), source.TableRequest{Name: "campaigns"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "account_id is required")
}

func TestGetTable_AccountIDFromURI(t *testing.T) {
	s := &FacebookAdsSource{accountID: "act_111"}

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "campaigns"})
	require.NoError(t, err)
	assert.Equal(t, "campaigns", table.Name())
	assert.Equal(t, config.StrategyMerge, table.Strategy())
}

func TestGetTable_AccountIDFromTableName(t *testing.T) {
	s := &FacebookAdsSource{accountID: ""}

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "campaigns:1234567890"})
	require.NoError(t, err)
	assert.Equal(t, "campaigns", table.Name())
}

func TestGetTable_TableNamePriorityOverURI(t *testing.T) {
	s := &FacebookAdsSource{accountID: "act_from_uri"}

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "campaigns:from_table"})
	require.NoError(t, err)
	assert.Equal(t, "campaigns", table.Name())
}

func TestGetTable_UnsupportedTable(t *testing.T) {
	s := &FacebookAdsSource{accountID: "act_111"}

	_, err := s.GetTable(context.Background(), source.TableRequest{Name: "invalid_table"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported table")
}

func TestGetTable_Strategies(t *testing.T) {
	s := &FacebookAdsSource{accountID: "act_111"}

	tests := []struct {
		table          string
		wantStrategy   config.IncrementalStrategy
		wantIncKey     string
		wantPrimaryKey []string
	}{
		{"campaigns", config.StrategyMerge, "updated_time", []string{"id"}},
		{"ad_sets", config.StrategyMerge, "updated_time", []string{"id"}},
		{"ads", config.StrategyMerge, "updated_time", []string{"id"}},
		{"ad_creatives", config.StrategyMerge, "", []string{"id"}},
		{"leads", config.StrategyMerge, "created_time", []string{"id", "created_time"}},
		{"facebook_insights", config.StrategyMerge, "date_start", []string{"campaign_id", "adset_id", "ad_id", "date_start"}},
	}

	for _, tt := range tests {
		t.Run(tt.table, func(t *testing.T) {
			table, err := s.GetTable(context.Background(), source.TableRequest{Name: tt.table})
			require.NoError(t, err)
			assert.Equal(t, tt.wantStrategy, table.Strategy())
			assert.Equal(t, tt.wantIncKey, table.IncrementalKey())
			assert.Equal(t, tt.wantPrimaryKey, table.PrimaryKeys())
		})
	}
}

func TestParseInsightsTableName(t *testing.T) {
	tests := []struct {
		name           string
		raw            string
		wantAccountIDs []string
		wantBreakdown  string
		wantDimensions []string
		wantFields     []string
		wantLevel      string
		wantErr        bool
	}{
		{
			name: "bare facebook_insights",
			raw:  "facebook_insights",
		},
		{
			name:          "predefined breakdown",
			raw:           "facebook_insights:ads_insights_age_and_gender",
			wantBreakdown: "ads_insights_age_and_gender",
		},
		{
			name:          "predefined breakdown with custom fields",
			raw:           "facebook_insights:ads_insights_country:impressions,clicks,spend",
			wantBreakdown: "ads_insights_country",
			wantFields:    []string{"impressions", "clicks", "spend"},
		},
		{
			name:           "custom dimensions with fields",
			raw:            "facebook_insights:age,gender:impressions,clicks,spend",
			wantDimensions: []string{"age", "gender"},
			wantFields:     []string{"impressions", "clicks", "spend"},
		},
		{
			name:           "level with custom dimensions",
			raw:            "facebook_insights:campaign,age,gender:impressions,clicks",
			wantDimensions: []string{"age", "gender"},
			wantFields:     []string{"impressions", "clicks"},
			wantLevel:      "campaign",
		},
		{
			name:           "multi-account basic",
			raw:            "facebook_insights_with_account_ids:123,456",
			wantAccountIDs: []string{"123", "456"},
		},
		{
			name:           "multi-account with predefined breakdown",
			raw:            "facebook_insights_with_account_ids:123,456:ads_insights_age_and_gender",
			wantAccountIDs: []string{"123", "456"},
			wantBreakdown:  "ads_insights_age_and_gender",
		},
		{
			name:           "multi-account with breakdown and fields",
			raw:            "facebook_insights_with_account_ids:123,456:ads_insights_country:impressions,clicks,spend",
			wantAccountIDs: []string{"123", "456"},
			wantBreakdown:  "ads_insights_country",
			wantFields:     []string{"impressions", "clicks", "spend"},
		},
		{
			name:    "multi-account missing IDs",
			raw:     "facebook_insights_with_account_ids",
			wantErr: true,
		},
		{
			name:       "custom fields without breakdown",
			raw:        "facebook_insights::impressions,clicks,spend",
			wantFields: []string{"impressions", "clicks", "spend"},
		},
		{
			name:      "level only",
			raw:       "facebook_insights:campaign",
			wantLevel: "campaign",
		},
		{
			name:       "level only with fields",
			raw:        "facebook_insights:campaign:impressions,clicks",
			wantLevel:  "campaign",
			wantFields: []string{"impressions", "clicks"},
		},
		{
			name:           "multi-account with custom dimensions",
			raw:            "facebook_insights_with_account_ids:123:age,gender",
			wantAccountIDs: []string{"123"},
			wantDimensions: []string{"age", "gender"},
		},
		{
			name:          "bare ads_insights predefined",
			raw:           "facebook_insights:ads_insights",
			wantBreakdown: "ads_insights",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			accountIDs, ic, err := parseInsightsTableName(tt.raw)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantAccountIDs, accountIDs)
			assert.Equal(t, tt.wantBreakdown, ic.breakdown)
			assert.Equal(t, tt.wantDimensions, ic.dimensions)
			assert.Equal(t, tt.wantFields, ic.fields)
			assert.Equal(t, tt.wantLevel, ic.level)
		})
	}
}

func TestGetTable_InsightsWithAccountIDs(t *testing.T) {
	s := &FacebookAdsSource{accountID: ""}

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "facebook_insights_with_account_ids:123,456"})
	require.NoError(t, err)
	assert.Equal(t, "facebook_insights", table.Name())
	assert.Equal(t, config.StrategyMerge, table.Strategy())
	assert.Equal(t, "date_start", table.IncrementalKey())
}

func TestGetTable_InsightsWithBreakdown(t *testing.T) {
	s := &FacebookAdsSource{accountID: "act_111"}

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "facebook_insights:ads_insights_age_and_gender"})
	require.NoError(t, err)
	assert.Equal(t, "facebook_insights", table.Name())
	assert.Equal(t, config.StrategyMerge, table.Strategy())
}

func TestGetTable_StandaloneAdsInsightsPrefix(t *testing.T) {
	s := &FacebookAdsSource{accountID: "act_111"}

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "ads_insights_platform_and_device"})
	require.NoError(t, err)
	assert.Equal(t, "facebook_insights", table.Name())
	assert.Equal(t, config.StrategyMerge, table.Strategy())
	assert.Equal(t, []string{"campaign_id", "adset_id", "ad_id", "date_start", "publisher_platform", "platform_position", "impression_device"}, table.PrimaryKeys())
}

func TestAccountEndpoint(t *testing.T) {
	s := &FacebookAdsSource{}
	assert.Equal(t, "/act_123/campaigns", s.accountEndpoint("act_123", "campaigns"))
	assert.Equal(t, "/act_123/adsets", s.accountEndpoint("act_123", "adsets"))
}

func TestBuildFiltering(t *testing.T) {
	midnight := time.Date(2026, 2, 25, 0, 0, 0, 0, time.UTC)
	precise := time.Date(2026, 2, 25, 15, 30, 0, 0, time.UTC)

	t.Run("no filter field returns empty", func(t *testing.T) {
		result := buildFiltering("", source.ReadOptions{IntervalStart: &midnight})
		assert.Equal(t, "", result)
	})

	t.Run("no intervals returns empty", func(t *testing.T) {
		result := buildFiltering("campaign.updated_time", source.ReadOptions{})
		assert.Equal(t, "", result)
	})

	t.Run("start uses GREATER_THAN with -1s for inclusive boundary", func(t *testing.T) {
		result := buildFiltering("campaign.updated_time", source.ReadOptions{IntervalStart: &midnight})
		assert.Contains(t, result, `"operator":"GREATER_THAN"`)
		// midnight is 1772006400, -1s = 1772006399
		expectedTS := strconv.FormatInt(midnight.Unix()-1, 10)
		assert.Contains(t, result, expectedTS)
	})

	t.Run("end midnight uses LESS_THAN with +86400s for full day inclusion", func(t *testing.T) {
		result := buildFiltering("campaign.updated_time", source.ReadOptions{IntervalEnd: &midnight})
		assert.Contains(t, result, `"operator":"LESS_THAN"`)
		// midnight + 86400 = next day midnight
		expectedTS := strconv.FormatInt(midnight.Unix()+86400, 10)
		assert.Contains(t, result, expectedTS)
	})

	t.Run("end precise timestamp uses LESS_THAN with +1s", func(t *testing.T) {
		result := buildFiltering("campaign.updated_time", source.ReadOptions{IntervalEnd: &precise})
		assert.Contains(t, result, `"operator":"LESS_THAN"`)
		expectedTS := strconv.FormatInt(precise.Unix()+1, 10)
		assert.Contains(t, result, expectedTS)
	})

	t.Run("both start and end produce two filters", func(t *testing.T) {
		start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2026, 2, 25, 0, 0, 0, 0, time.UTC)
		result := buildFiltering("campaign.updated_time", source.ReadOptions{
			IntervalStart: &start,
			IntervalEnd:   &end,
		})
		// Should have two filter objects
		assert.Contains(t, result, `"operator":"GREATER_THAN"`)
		assert.Contains(t, result, `"operator":"LESS_THAN"`)
	})
}

func TestFilterByEndTime(t *testing.T) {
	items := []map[string]interface{}{
		{"id": "1", "updated_time": "2026-01-10T12:00:00+0000"},
		{"id": "2", "updated_time": "2026-01-20T08:30:00+0000"},
		{"id": "3", "updated_time": "2026-01-25T23:59:59+0000"},
		{"id": "4", "updated_time": "2026-01-26T00:00:01+0000"},
		{"id": "5", "updated_time": "2026-02-15T10:00:00+0000"},
	}

	t.Run("midnight end includes entire day", func(t *testing.T) {
		end := time.Date(2026, 1, 25, 0, 0, 0, 0, time.UTC)
		filtered := filterByEndTime(items, "updated_time", &end)
		assert.Len(t, filtered, 3)
		assert.Equal(t, "1", filtered[0]["id"])
		assert.Equal(t, "2", filtered[1]["id"])
		assert.Equal(t, "3", filtered[2]["id"])
	})

	t.Run("precise end excludes items at or after", func(t *testing.T) {
		end := time.Date(2026, 1, 20, 10, 0, 0, 0, time.UTC)
		filtered := filterByEndTime(items, "updated_time", &end)
		assert.Len(t, filtered, 2)
		assert.Equal(t, "1", filtered[0]["id"])
		assert.Equal(t, "2", filtered[1]["id"])
	})

	t.Run("nil end returns all items", func(t *testing.T) {
		filtered := filterByEndTime(items, "updated_time", nil)
		assert.Len(t, filtered, 5)
	})

	t.Run("missing field keeps item", func(t *testing.T) {
		noField := []map[string]interface{}{
			{"id": "1"},
			{"id": "2", "updated_time": "2026-02-15T10:00:00+0000"},
		}
		end := time.Date(2026, 1, 25, 0, 0, 0, 0, time.UTC)
		filtered := filterByEndTime(noField, "updated_time", &end)
		assert.Len(t, filtered, 1)
		assert.Equal(t, "1", filtered[0]["id"])
	})

	t.Run("end far in future keeps all", func(t *testing.T) {
		end := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
		filtered := filterByEndTime(items, "updated_time", &end)
		assert.Len(t, filtered, 5)
	})
}

func TestParseFacebookTime(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  time.Time
	}{
		{
			"facebook format",
			"2026-01-15T10:30:00+0000",
			time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC),
		},
		{
			"RFC3339",
			"2026-01-15T10:30:00Z",
			time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC),
		},
		{
			"date only",
			"2026-01-15",
			time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		},
		{
			"invalid returns zero",
			"not-a-date",
			time.Time{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseFacebookTime(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildTimeChunks(t *testing.T) {
	jan1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	jan3 := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	jan5 := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)

	t.Run("no interval returns single empty chunk", func(t *testing.T) {
		chunks := buildTimeChunks(nil, nil, 1)
		assert.Equal(t, []string{""}, chunks)
	})

	t.Run("single day", func(t *testing.T) {
		chunks := buildTimeChunks(&jan1, &jan1, 1)
		require.Len(t, chunks, 1)
		assert.Contains(t, chunks[0], `"since":"2026-01-01"`)
		assert.Contains(t, chunks[0], `"until":"2026-01-01"`)
	})

	t.Run("multi-day with chunkDays=1 produces daily chunks", func(t *testing.T) {
		chunks := buildTimeChunks(&jan1, &jan3, 1)
		require.Len(t, chunks, 3)
		assert.Contains(t, chunks[0], `"since":"2026-01-01"`)
		assert.Contains(t, chunks[0], `"until":"2026-01-01"`)
		assert.Contains(t, chunks[1], `"since":"2026-01-02"`)
		assert.Contains(t, chunks[1], `"until":"2026-01-02"`)
		assert.Contains(t, chunks[2], `"since":"2026-01-03"`)
		assert.Contains(t, chunks[2], `"until":"2026-01-03"`)
	})

	t.Run("end date is inclusive", func(t *testing.T) {
		chunks := buildTimeChunks(&jan1, &jan1, 1)
		require.Len(t, chunks, 1)
		// Should include jan1, not be empty
	})

	t.Run("larger chunk size", func(t *testing.T) {
		chunks := buildTimeChunks(&jan1, &jan5, 3)
		require.Len(t, chunks, 2)
		assert.Contains(t, chunks[0], `"since":"2026-01-01"`)
		assert.Contains(t, chunks[0], `"until":"2026-01-03"`)
		assert.Contains(t, chunks[1], `"since":"2026-01-04"`)
		assert.Contains(t, chunks[1], `"until":"2026-01-05"`)
	})
}

func TestEdgeConfig_AdSetsUsesUpdatedSince(t *testing.T) {
	s := &FacebookAdsSource{accountID: "act_111"}

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "ad_sets"})
	require.NoError(t, err)
	assert.Equal(t, "ad_sets", table.Name())
	assert.Equal(t, config.StrategyMerge, table.Strategy())
	assert.Equal(t, "updated_time", table.IncrementalKey())
}
