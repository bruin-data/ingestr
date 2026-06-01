package linkedinads

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFlattenAnalyticsItems(t *testing.T) {
	t.Run("daily granularity", func(t *testing.T) {
		items := []interface{}{
			map[string]interface{}{
				"clicks":      0,
				"impressions": 43,
				"pivotValues": []interface{}{
					"urn:li:sponsoredCampaign:123456",
				},
				"dateRange": map[string]interface{}{
					"start": map[string]interface{}{"month": float64(12), "day": float64(10), "year": float64(2024)},
					"end":   map[string]interface{}{"month": float64(12), "day": float64(10), "year": float64(2024)},
				},
				"likes": 0,
			},
		}

		result := flattenAnalyticsItems(items, "campaign", timeGranularityDaily)

		assert.Len(t, result, 1)
		assert.Equal(t, 0, result[0]["clicks"])
		assert.Equal(t, 43, result[0]["impressions"])
		assert.Equal(t, "urn:li:sponsoredCampaign:123456", result[0]["campaign"])
		assert.Equal(t, time.Date(2024, 12, 10, 0, 0, 0, 0, time.UTC), result[0]["date"])
		assert.Equal(t, 0, result[0]["likes"])
		assert.NotContains(t, result[0], "pivotValues")
		assert.NotContains(t, result[0], "dateRange")
	})

	t.Run("monthly granularity with multiple pivot values", func(t *testing.T) {
		items := []interface{}{
			map[string]interface{}{
				"clicks":      0,
				"impressions": 43,
				"pivotValues": []interface{}{
					"urn:li:sponsoredCampaign:123456",
					"urn:li:sponsoredCampaign:7891011",
				},
				"dateRange": map[string]interface{}{
					"start": map[string]interface{}{"month": float64(12), "day": float64(10), "year": float64(2024)},
					"end":   map[string]interface{}{"month": float64(12), "day": float64(30), "year": float64(2024)},
				},
				"likes": 0,
			},
		}

		result := flattenAnalyticsItems(items, "campaign", timeGranularityMonthly)

		assert.Len(t, result, 1)
		assert.Equal(t, 0, result[0]["clicks"])
		assert.Equal(t, 43, result[0]["impressions"])
		// Multiple pivot values should be kept as array
		assert.Equal(t, []interface{}{
			"urn:li:sponsoredCampaign:123456",
			"urn:li:sponsoredCampaign:7891011",
		}, result[0]["campaign"])
		assert.Equal(t, time.Date(2024, 12, 10, 0, 0, 0, 0, time.UTC), result[0]["start_date"])
		assert.Equal(t, time.Date(2024, 12, 30, 0, 0, 0, 0, time.UTC), result[0]["end_date"])
		assert.Equal(t, 0, result[0]["likes"])
		assert.NotContains(t, result[0], "pivotValues")
		assert.NotContains(t, result[0], "dateRange")
	})
}

func TestFindIntervals(t *testing.T) {
	t.Run("monthly granularity within 2 years", func(t *testing.T) {
		startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		endDate := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)

		intervals, err := findIntervals(startDate, endDate, timeGranularityMonthly)

		assert.NoError(t, err)
		assert.Len(t, intervals, 1)
		assert.Equal(t, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), intervals[0].start)
		assert.Equal(t, time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC), intervals[0].end)
	})

	t.Run("monthly granularity over 2 years", func(t *testing.T) {
		startDate := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		endDate := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)

		intervals, err := findIntervals(startDate, endDate, timeGranularityMonthly)

		assert.NoError(t, err)
		assert.Len(t, intervals, 3)

		// First interval: 2020-01-01 to 2022-01-01
		assert.Equal(t, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), intervals[0].start)
		assert.Equal(t, time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC), intervals[0].end)

		// Second interval: 2022-01-02 to 2024-01-02
		assert.Equal(t, time.Date(2022, 1, 2, 0, 0, 0, 0, time.UTC), intervals[1].start)
		assert.Equal(t, time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), intervals[1].end)

		// Third interval: 2024-01-03 to 2024-12-31
		assert.Equal(t, time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), intervals[2].start)
		assert.Equal(t, time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC), intervals[2].end)
	})

	t.Run("monthly granularity edge case", func(t *testing.T) {
		startDate := time.Date(2022, 2, 1, 0, 0, 0, 0, time.UTC)
		endDate := time.Date(2024, 2, 8, 0, 0, 0, 0, time.UTC)

		intervals, err := findIntervals(startDate, endDate, timeGranularityMonthly)

		assert.NoError(t, err)
		assert.Len(t, intervals, 2)

		// First interval: 2022-02-01 to 2024-02-01
		assert.Equal(t, time.Date(2022, 2, 1, 0, 0, 0, 0, time.UTC), intervals[0].start)
		assert.Equal(t, time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC), intervals[0].end)

		// Second interval: 2024-02-02 to 2024-02-08
		assert.Equal(t, time.Date(2024, 2, 2, 0, 0, 0, 0, time.UTC), intervals[1].start)
		assert.Equal(t, time.Date(2024, 2, 8, 0, 0, 0, 0, time.UTC), intervals[1].end)
	})

	t.Run("daily granularity over 6 months", func(t *testing.T) {
		startDate := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		endDate := time.Date(2024, 12, 20, 0, 0, 0, 0, time.UTC)

		intervals, err := findIntervals(startDate, endDate, timeGranularityDaily)

		assert.NoError(t, err)
		assert.Len(t, intervals, 4)

		// First interval: 2023-01-01 to 2023-07-01
		assert.Equal(t, time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC), intervals[0].start)
		assert.Equal(t, time.Date(2023, 7, 1, 0, 0, 0, 0, time.UTC), intervals[0].end)

		// Second interval: 2023-07-02 to 2024-01-02
		assert.Equal(t, time.Date(2023, 7, 2, 0, 0, 0, 0, time.UTC), intervals[1].start)
		assert.Equal(t, time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), intervals[1].end)

		// Third interval: 2024-01-03 to 2024-07-03
		assert.Equal(t, time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), intervals[2].start)
		assert.Equal(t, time.Date(2024, 7, 3, 0, 0, 0, 0, time.UTC), intervals[2].end)

		// Fourth interval: 2024-07-04 to 2024-12-20
		assert.Equal(t, time.Date(2024, 7, 4, 0, 0, 0, 0, time.UTC), intervals[3].start)
		assert.Equal(t, time.Date(2024, 12, 20, 0, 0, 0, 0, time.UTC), intervals[3].end)
	})

	t.Run("start after end returns error", func(t *testing.T) {
		startDate := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)
		endDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

		_, err := findIntervals(startDate, endDate, timeGranularityDaily)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "start date must be before end date")
	})
}

func TestConstructAnalyticsURL(t *testing.T) {
	t.Run("monthly granularity with multiple accounts", func(t *testing.T) {
		start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)
		accountIDs := []string{"123456", "456789"}
		metrics := []string{"impressions", "clicks", "likes"}
		pivot := "campaign"

		url := constructAnalyticsURL(start, end, accountIDs, metrics, pivot, timeGranularityMonthly)

		expected := "/adAnalytics?q=analytics&timeGranularity=MONTHLY&dateRange=(start:(year:2024,month:1,day:1),end:(year:2024,month:12,day:31))&accounts=List(urn%3Ali%3AsponsoredAccount%3A123456,urn%3Ali%3AsponsoredAccount%3A456789)&pivot=CAMPAIGN&fields=impressions,clicks,likes"
		assert.Equal(t, expected, url)
	})

	t.Run("monthly granularity with creative pivot", func(t *testing.T) {
		start := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)
		accountIDs := []string{"123456"}
		metrics := []string{"impressions", "clicks", "likes"}
		pivot := "creative"

		url := constructAnalyticsURL(start, end, accountIDs, metrics, pivot, timeGranularityMonthly)

		expected := "/adAnalytics?q=analytics&timeGranularity=MONTHLY&dateRange=(start:(year:2019,month:1,day:1),end:(year:2024,month:12,day:31))&accounts=List(urn%3Ali%3AsponsoredAccount%3A123456)&pivot=CREATIVE&fields=impressions,clicks,likes"
		assert.Equal(t, expected, url)
	})

	t.Run("daily granularity", func(t *testing.T) {
		start := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)
		end := time.Date(2024, 12, 15, 0, 0, 0, 0, time.UTC)
		accountIDs := []string{"999888"}
		metrics := []string{"impressions", "clicks"}
		pivot := "account"

		url := constructAnalyticsURL(start, end, accountIDs, metrics, pivot, timeGranularityDaily)

		expected := "/adAnalytics?q=analytics&timeGranularity=DAILY&dateRange=(start:(year:2024,month:6,day:15),end:(year:2024,month:12,day:15))&accounts=List(urn%3Ali%3AsponsoredAccount%3A999888)&pivot=ACCOUNT&fields=impressions,clicks"
		assert.Equal(t, expected, url)
	})

	t.Run("daily granularity with impression device pivot", func(t *testing.T) {
		start := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)
		end := time.Date(2024, 12, 15, 0, 0, 0, 0, time.UTC)
		accountIDs := []string{"999888"}
		metrics := []string{"impressions", "clicks"}
		pivot := "impression_device"

		url := constructAnalyticsURL(start, end, accountIDs, metrics, pivot, timeGranularityDaily)

		expected := "/adAnalytics?q=analytics&timeGranularity=DAILY&dateRange=(start:(year:2024,month:6,day:15),end:(year:2024,month:12,day:15))&accounts=List(urn%3Ali%3AsponsoredAccount%3A999888)&pivot=IMPRESSION_DEVICE_TYPE&fields=impressions,clicks"
		assert.Equal(t, expected, url)
	})
}

func TestParseCustomTable(t *testing.T) {
	t.Run("valid custom table with campaign and date", func(t *testing.T) {
		cfg, err := parseCustomTableName("custom:campaign,date:impressions,clicks")

		assert.NoError(t, err)
		assert.NotNil(t, cfg)
		assert.Equal(t, []string{"campaign", "date"}, cfg.dimensions)
		assert.Contains(t, cfg.metrics, "impressions")
		assert.Contains(t, cfg.metrics, "clicks")
		assert.Contains(t, cfg.metrics, "pivotValues")
		assert.Equal(t, "campaign", cfg.pivot)
		assert.Equal(t, timeGranularityDaily, cfg.timeGranularity)
		assert.Equal(t, []string{"campaign", "date"}, cfg.primaryKeys)
		assert.Equal(t, "date", cfg.incrementalKey)
	})

	t.Run("valid custom table with creative and month", func(t *testing.T) {
		cfg, err := parseCustomTableName("custom:creative,month:impressions")

		assert.NoError(t, err)
		assert.NotNil(t, cfg)
		assert.Equal(t, []string{"creative", "month"}, cfg.dimensions)
		assert.Equal(t, "creative", cfg.pivot)
		assert.Equal(t, timeGranularityMonthly, cfg.timeGranularity)
		assert.Equal(t, []string{"creative", "start_date", "end_date"}, cfg.primaryKeys)
		assert.Equal(t, "start_date", cfg.incrementalKey)
	})

	t.Run("valid custom table with account", func(t *testing.T) {
		cfg, err := parseCustomTableName("custom:account,date:clicks")

		assert.NoError(t, err)
		assert.NotNil(t, cfg)
		assert.Equal(t, "account", cfg.pivot)
	})

	t.Run("valid custom table with member country", func(t *testing.T) {
		cfg, err := parseCustomTableName("custom:member_country,date:impressions")

		assert.NoError(t, err)
		assert.NotNil(t, cfg)
		assert.Equal(t, "member_country", cfg.pivot)
		assert.Equal(t, []string{"member_country", "date"}, cfg.primaryKeys)
	})

	t.Run("invalid format - missing parts", func(t *testing.T) {
		_, err := parseCustomTableName("custom:campaign")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid custom table format")
	})

	t.Run("invalid format - missing dimension", func(t *testing.T) {
		_, err := parseCustomTableName("custom:date:impressions")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "dimensions must include one of")
	})

	t.Run("invalid format - missing time dimension", func(t *testing.T) {
		_, err := parseCustomTableName("custom:campaign:impressions")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "'date' or 'month' is required")
	})

	t.Run("invalid format - empty metrics", func(t *testing.T) {
		_, err := parseCustomTableName("custom:campaign,date:")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "at least one metric is required")
	})

	t.Run("invalid format - unsupported pivot", func(t *testing.T) {
		_, err := parseCustomTableName("custom:objective_type,date:impressions")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "dimensions must include one of")
	})
}

func TestAnalyticsTables(t *testing.T) {
	s := NewLinkedInAdsSource()
	tables := s.getTables()

	testCases := []struct {
		name string
		pks  []string
	}{
		{name: "ad_campaign_analytics", pks: []string{"campaign", "date"}},
		{name: "ad_creative_analytics", pks: []string{"creative", "date"}},
		{name: "ad_impression_device_analytics", pks: []string{"impression_device", "date"}},
		{name: "ad_member_company_size_analytics", pks: []string{"member_company_size", "date"}},
		{name: "ad_member_country_analytics", pks: []string{"member_country", "date"}},
		{name: "ad_member_job_function_analytics", pks: []string{"member_job_function", "date"}},
		{name: "ad_member_job_title_analytics", pks: []string{"member_job_title", "date"}},
		{name: "ad_member_industry_analytics", pks: []string{"member_industry", "date"}},
		{name: "ad_member_region_analytics", pks: []string{"member_region", "date"}},
		{name: "ad_member_company_analytics", pks: []string{"member_company", "date"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			table, ok := tables[tc.name]

			assert.True(t, ok)
			assert.Equal(t, tc.name, table.Name())
			assert.Equal(t, tc.pks, table.PrimaryKeys())
			assert.Equal(t, "date", table.IncrementalKey())
		})
	}
}

func TestParseTimeInterval(t *testing.T) {
	t.Run("error when interval_start is nil", func(t *testing.T) {
		endTime := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)

		_, _, err := parseTimeInterval(nil, endTime)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "interval_start is required")
	})

	t.Run("error when interval_end is nil", func(t *testing.T) {
		startTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

		_, _, err := parseTimeInterval(startTime, nil)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "interval_end is required")
	})

	t.Run("success with time.Time values", func(t *testing.T) {
		startTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		endTime := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)

		start, end, err := parseTimeInterval(startTime, endTime)

		assert.NoError(t, err)
		assert.Equal(t, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), start)
		assert.Equal(t, time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC), end)
	})
}
