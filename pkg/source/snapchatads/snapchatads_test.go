package snapchatads

import (
	"encoding/json"
	"testing"

	"github.com/bruin-data/gong/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatsPrimaryKeys(t *testing.T) {
	tests := []struct {
		name    string
		table   string
		wantPKs []string
		wantErr bool
	}{
		{
			name:    "campaigns_stats basic",
			table:   "campaigns_stats:DAY:impressions,spend",
			wantPKs: []string{"campaign_id", "start_time", "end_time"},
		},
		{
			name:    "ads_stats basic",
			table:   "ads_stats:HOUR:impressions",
			wantPKs: []string{"ad_id", "start_time", "end_time"},
		},
		{
			name:    "ad_squads_stats basic",
			table:   "ad_squads_stats:DAY:spend",
			wantPKs: []string{"adsquad_id", "start_time", "end_time"},
		},
		{
			name:    "ad_accounts_stats basic",
			table:   "ad_accounts_stats:TOTAL:impressions",
			wantPKs: []string{"adaccount_id", "start_time", "end_time"},
		},
		{
			name:    "campaigns_stats with adsquad breakdown",
			table:   "campaigns_stats:adsquad,DAY:impressions,spend",
			wantPKs: []string{"campaign_id", "adsquad_id", "start_time", "end_time"},
		},
		{
			name:    "campaigns_stats with ad breakdown",
			table:   "campaigns_stats:ad,HOUR:spend",
			wantPKs: []string{"campaign_id", "ad_id", "start_time", "end_time"},
		},
		{
			name:    "ad_squads_stats with campaign breakdown",
			table:   "ad_squads_stats:campaign,DAY:impressions",
			wantPKs: []string{"adsquad_id", "campaign_id", "start_time", "end_time"},
		},
		{
			name:    "missing granularity",
			table:   "campaigns_stats:",
			wantErr: true,
		},
		{
			name:    "no params",
			table:   "campaigns_stats",
			wantErr: true,
		},
	}

	s := &SnapchatAdsSource{orgID: "test-org"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			table, err := s.GetTable(t.Context(), source.TableRequest{Name: tt.table})
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.wantPKs, table.PrimaryKeys())
		})
	}
}

func TestParseStatsTable(t *testing.T) {
	tests := []struct {
		name       string
		table      string
		wantGran   string
		wantBreak  string
		wantFields string
		wantErr    bool
	}{
		{
			name:       "basic DAY",
			table:      "campaigns_stats:DAY:impressions,spend",
			wantGran:   "DAY",
			wantFields: "impressions,spend",
		},
		{
			name:       "with breakdown",
			table:      "campaigns_stats:adsquad,HOUR:spend",
			wantGran:   "HOUR",
			wantBreak:  "adsquad",
			wantFields: "spend",
		},
		{
			name:       "default fields",
			table:      "ads_stats:TOTAL",
			wantGran:   "TOTAL",
			wantFields: defaultStatsFields,
		},
		{
			name:    "missing params",
			table:   "campaigns_stats:",
			wantErr: true,
		},
		{
			name:    "invalid param",
			table:   "campaigns_stats:INVALID",
			wantErr: true,
		},
		{
			name:    "no granularity only breakdown",
			table:   "campaigns_stats:adsquad",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := parseStatsTable(tt.table)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.wantGran, parsed.config.granularity)
			assert.Equal(t, tt.wantBreak, parsed.config.breakdown)
			assert.Equal(t, tt.wantFields, parsed.config.fields)
		})
	}
}

func TestNormalizeStatsRecord(t *testing.T) {
	t.Run("fills missing fields", func(t *testing.T) {
		record := map[string]interface{}{}
		normalizeStatsRecord(record)

		assert.Equal(t, "no_campaign_id", record["campaign_id"])
		assert.Nil(t, record["adsquad_id"])
		assert.Nil(t, record["ad_id"])
		assert.Equal(t, "no_start_time", record["start_time"])
		assert.Equal(t, "no_end_time", record["end_time"])
	})

	t.Run("preserves existing values", func(t *testing.T) {
		record := map[string]interface{}{
			"campaign_id": "camp123",
			"adsquad_id":  "squad456",
			"ad_id":       "ad789",
			"start_time":  "2026-01-01T00:00:00Z",
			"end_time":    "2026-01-02T00:00:00Z",
		}
		normalizeStatsRecord(record)

		assert.Equal(t, "camp123", record["campaign_id"])
		assert.Equal(t, "squad456", record["adsquad_id"])
		assert.Equal(t, "ad789", record["ad_id"])
		assert.Equal(t, "2026-01-01T00:00:00Z", record["start_time"])
		assert.Equal(t, "2026-01-02T00:00:00Z", record["end_time"])
	})

	t.Run("replaces nil campaign_id", func(t *testing.T) {
		record := map[string]interface{}{
			"campaign_id": nil,
			"start_time":  nil,
		}
		normalizeStatsRecord(record)

		assert.Equal(t, "no_campaign_id", record["campaign_id"])
		assert.Equal(t, "no_start_time", record["start_time"])
	})
}

func toRawMap(t *testing.T, v interface{}) map[string]json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &raw))
	return raw
}

func TestParseTimeseriesStats(t *testing.T) {
	t.Run("without breakdown", func(t *testing.T) {
		raw := toRawMap(t, map[string]interface{}{
			"timeseries_stats": []interface{}{
				map[string]interface{}{
					"sub_request_status": "SUCCESS",
					"timeseries_stat": map[string]interface{}{
						"id":   "campaign-123",
						"type": "CAMPAIGN",
						"timeseries": []interface{}{
							map[string]interface{}{
								"start_time": "2024-01-01T00:00:00.000Z",
								"end_time":   "2024-01-01T01:00:00.000Z",
								"stats":      map[string]interface{}{"impressions": 100, "spend": 50},
							},
						},
					},
				},
			},
		})

		items, err := parseTimeseriesStats(raw)
		require.NoError(t, err)
		require.Len(t, items, 1)

		assert.Equal(t, "campaign-123", items[0]["campaign_id"])
		assert.Nil(t, items[0]["adsquad_id"])
		assert.Nil(t, items[0]["ad_id"])
		assert.Equal(t, float64(100), items[0]["impressions"])
		assert.Equal(t, float64(50), items[0]["spend"])
		assert.Equal(t, "2024-01-01T00:00:00.000Z", items[0]["start_time"])
		assert.Equal(t, "2024-01-01T01:00:00.000Z", items[0]["end_time"])
	})

	t.Run("with ad breakdown", func(t *testing.T) {
		raw := toRawMap(t, map[string]interface{}{
			"timeseries_stats": []interface{}{
				map[string]interface{}{
					"sub_request_status": "SUCCESS",
					"timeseries_stat": map[string]interface{}{
						"id":   "campaign-123",
						"type": "CAMPAIGN",
						"breakdown_stats": map[string]interface{}{
							"ad": []interface{}{
								map[string]interface{}{
									"id": "ad-456",
									"timeseries": []interface{}{
										map[string]interface{}{
											"start_time": "2024-01-01T00:00:00.000Z",
											"end_time":   "2024-01-01T01:00:00.000Z",
											"stats":      map[string]interface{}{"impressions": 50, "spend": 25},
										},
									},
								},
							},
						},
					},
				},
			},
		})

		items, err := parseTimeseriesStats(raw)
		require.NoError(t, err)
		require.Len(t, items, 1)

		assert.Equal(t, "campaign-123", items[0]["campaign_id"])
		assert.Equal(t, "ad-456", items[0]["ad_id"])
		assert.Nil(t, items[0]["adsquad_id"])
		assert.Equal(t, float64(50), items[0]["impressions"])
	})

	t.Run("with adsquad breakdown", func(t *testing.T) {
		raw := toRawMap(t, map[string]interface{}{
			"timeseries_stats": []interface{}{
				map[string]interface{}{
					"sub_request_status": "SUCCESS",
					"timeseries_stat": map[string]interface{}{
						"id":   "campaign-123",
						"type": "CAMPAIGN",
						"breakdown_stats": map[string]interface{}{
							"adsquad": []interface{}{
								map[string]interface{}{
									"id": "adsquad-789",
									"timeseries": []interface{}{
										map[string]interface{}{
											"start_time": "2024-01-01T00:00:00.000Z",
											"end_time":   "2024-01-01T01:00:00.000Z",
											"stats":      map[string]interface{}{"impressions": 75, "spend": 30},
										},
									},
								},
							},
						},
					},
				},
			},
		})

		items, err := parseTimeseriesStats(raw)
		require.NoError(t, err)
		require.Len(t, items, 1)

		assert.Equal(t, "campaign-123", items[0]["campaign_id"])
		assert.Equal(t, "adsquad-789", items[0]["adsquad_id"])
		assert.Nil(t, items[0]["ad_id"])
		assert.Equal(t, float64(75), items[0]["impressions"])
	})

	t.Run("skips non-SUCCESS status", func(t *testing.T) {
		raw := toRawMap(t, map[string]interface{}{
			"timeseries_stats": []interface{}{
				map[string]interface{}{
					"sub_request_status": "FAILED",
					"timeseries_stat": map[string]interface{}{
						"id":   "campaign-fail",
						"type": "CAMPAIGN",
						"timeseries": []interface{}{
							map[string]interface{}{
								"start_time": "2024-01-01T00:00:00.000Z",
								"end_time":   "2024-01-01T01:00:00.000Z",
								"stats":      map[string]interface{}{"impressions": 999},
							},
						},
					},
				},
				map[string]interface{}{
					"sub_request_status": "SUCCESS",
					"timeseries_stat": map[string]interface{}{
						"id":   "campaign-ok",
						"type": "CAMPAIGN",
						"timeseries": []interface{}{
							map[string]interface{}{
								"start_time": "2024-01-01T00:00:00.000Z",
								"end_time":   "2024-01-01T01:00:00.000Z",
								"stats":      map[string]interface{}{"impressions": 100},
							},
						},
					},
				},
			},
		})

		items, err := parseTimeseriesStats(raw)
		require.NoError(t, err)
		require.Len(t, items, 1)

		assert.Equal(t, "campaign-ok", items[0]["campaign_id"])
		assert.Equal(t, float64(100), items[0]["impressions"])
	})

	t.Run("missing timeseries_stats key", func(t *testing.T) {
		raw := toRawMap(t, map[string]interface{}{})
		items, err := parseTimeseriesStats(raw)
		require.NoError(t, err)
		assert.Nil(t, items)
	})
}

func TestParseTotalStats(t *testing.T) {
	t.Run("without breakdown", func(t *testing.T) {
		raw := toRawMap(t, map[string]interface{}{
			"total_stats": []interface{}{
				map[string]interface{}{
					"sub_request_status": "SUCCESS",
					"total_stat": map[string]interface{}{
						"id":         "campaign-123",
						"type":       "CAMPAIGN",
						"start_time": "2024-01-01T00:00:00.000Z",
						"end_time":   "2024-01-31T23:59:59.999Z",
						"stats":      map[string]interface{}{"impressions": 1000, "spend": 500},
					},
				},
			},
		})

		items, err := parseTotalStats(raw)
		require.NoError(t, err)
		require.Len(t, items, 1)

		assert.Equal(t, "campaign-123", items[0]["campaign_id"])
		assert.Nil(t, items[0]["adsquad_id"])
		assert.Nil(t, items[0]["ad_id"])
		assert.Equal(t, float64(1000), items[0]["impressions"])
		assert.Equal(t, float64(500), items[0]["spend"])
		assert.Equal(t, "2024-01-01T00:00:00.000Z", items[0]["start_time"])
		assert.Equal(t, "2024-01-31T23:59:59.999Z", items[0]["end_time"])
	})

	t.Run("with ad breakdown", func(t *testing.T) {
		raw := toRawMap(t, map[string]interface{}{
			"total_stats": []interface{}{
				map[string]interface{}{
					"sub_request_status": "SUCCESS",
					"total_stat": map[string]interface{}{
						"id":         "campaign-123",
						"type":       "CAMPAIGN",
						"start_time": "2024-01-01T00:00:00.000Z",
						"end_time":   "2024-01-31T23:59:59.999Z",
						"breakdown_stats": map[string]interface{}{
							"ad": []interface{}{
								map[string]interface{}{
									"id":    "ad-456",
									"stats": map[string]interface{}{"impressions": 500, "spend": 250},
								},
							},
						},
					},
				},
			},
		})

		items, err := parseTotalStats(raw)
		require.NoError(t, err)
		require.Len(t, items, 1)

		assert.Equal(t, "campaign-123", items[0]["campaign_id"])
		assert.Equal(t, "ad-456", items[0]["ad_id"])
		assert.Nil(t, items[0]["adsquad_id"])
		assert.Equal(t, float64(500), items[0]["impressions"])
		assert.Equal(t, float64(250), items[0]["spend"])
	})

	t.Run("lifetime stats", func(t *testing.T) {
		raw := toRawMap(t, map[string]interface{}{
			"lifetime_stats": []interface{}{
				map[string]interface{}{
					"sub_request_status": "SUCCESS",
					"lifetime_stat": map[string]interface{}{
						"id":         "ad-123",
						"type":       "AD",
						"start_time": "2024-01-01T00:00:00.000Z",
						"end_time":   "2024-12-31T23:59:59.999Z",
						"stats":      map[string]interface{}{"impressions": 5000},
					},
				},
			},
		})

		items, err := parseTotalStats(raw)
		require.NoError(t, err)
		require.Len(t, items, 1)

		assert.Equal(t, "ad-123", items[0]["ad_id"])
		assert.Equal(t, float64(5000), items[0]["impressions"])
	})

	t.Run("skips non-SUCCESS status", func(t *testing.T) {
		raw := toRawMap(t, map[string]interface{}{
			"total_stats": []interface{}{
				map[string]interface{}{
					"sub_request_status": "FAILED",
					"total_stat": map[string]interface{}{
						"id":    "campaign-123",
						"type":  "CAMPAIGN",
						"stats": map[string]interface{}{"impressions": 100},
					},
				},
			},
		})

		items, err := parseTotalStats(raw)
		require.NoError(t, err)
		assert.Empty(t, items)
	})

	t.Run("missing total_stats key", func(t *testing.T) {
		raw := toRawMap(t, map[string]interface{}{})
		items, err := parseTotalStats(raw)
		require.NoError(t, err)
		assert.Nil(t, items)
	})
}

func TestParseStatsTableOrderIndependent(t *testing.T) {
	result1, err := parseStatsTable("campaigns_stats:DAY,campaign:impressions")
	require.NoError(t, err)
	assert.Equal(t, "DAY", result1.config.granularity)
	assert.Equal(t, "campaign", result1.config.breakdown)

	result2, err := parseStatsTable("campaigns_stats:campaign,DAY:impressions")
	require.NoError(t, err)
	assert.Equal(t, "DAY", result2.config.granularity)
	assert.Equal(t, "campaign", result2.config.breakdown)
}

func TestParseStatsTableWithDimensionAndPivot(t *testing.T) {
	result, err := parseStatsTable("campaigns_stats:campaign,DAY,GEO,country:impressions")
	require.NoError(t, err)
	assert.Equal(t, "DAY", result.config.granularity)
	assert.Equal(t, "campaign", result.config.breakdown)
	assert.Equal(t, "GEO", result.config.dimension)
	assert.Equal(t, "country", result.config.pivot)
	assert.Equal(t, "impressions", result.config.fields)
}
