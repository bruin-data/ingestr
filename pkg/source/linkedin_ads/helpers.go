package linkedinads

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/gong/internal/config"
)

func (s *LinkedInAdsSource) fetch(ctx context.Context, endpoint string, params map[string]string) (map[string]interface{}, error) {
	var result map[string]interface{}
	req := s.client.R(ctx).SetResult(&result)

	// Build URL manually
	fullURL := endpoint
	if len(params) > 0 {
		var parts []string
		for k, v := range params {
			parts = append(parts, k+"="+v)
		}
		if strings.Contains(endpoint, "?") {
			fullURL = endpoint + "&" + strings.Join(parts, "&")
		} else {
			fullURL = endpoint + "?" + strings.Join(parts, "&")
		}
	}

	config.Debug("[LINKEDIN_ADS] Fetching: %s", fullURL)

	resp, err := req.Get(fullURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %w", endpoint, err)
	}

	if resp.StatusCode() == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode() >= 400 {
		return nil, fmt.Errorf("API returned status %d for %s: %s", resp.StatusCode(), endpoint, resp.String())
	}

	return result, nil
}

// fetchTokenPagination fetches data with pageSize/pageToken pagination.
func (s *LinkedInAdsSource) fetchTokenPagination(ctx context.Context, endpoint string, params map[string]string, pageSize int, callback func(elements []interface{}) error) error {
	if pageSize <= 0 {
		pageSize = 1000
	}

	var nextPageToken string

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Build params with pagination
		paginatedParams := make(map[string]string)
		for k, v := range params {
			paginatedParams[k] = v
		}
		paginatedParams["pageSize"] = strconv.Itoa(pageSize)
		if nextPageToken != "" {
			paginatedParams["pageToken"] = url.QueryEscape(nextPageToken)
		}

		data, err := s.fetch(ctx, endpoint, paginatedParams)
		if err != nil {
			return err
		}
		if data == nil {
			break
		}

		elements, ok := data["elements"].([]interface{})
		if !ok || len(elements) == 0 {
			break
		}

		// Call the callback with elements
		if err := callback(elements); err != nil {
			return err
		}

		// Check if we got less than pageSize (last page)
		if len(elements) < pageSize {
			break
		}

		// Get next page token from metadata
		metadata, ok := data["metadata"].(map[string]interface{})
		if !ok {
			break
		}

		nextPageToken, ok = metadata["nextPageToken"].(string)
		if !ok || nextPageToken == "" {
			break
		}

	}

	return nil
}

// fetchCursorPagination fetches data with start/count offset pagination.
func (s *LinkedInAdsSource) fetchCursorPagination(ctx context.Context, endpoint string, params map[string]string, count int, callback func(elements []interface{}) error) error {
	if count <= 0 {
		count = 1000
	}

	start := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Build params with pagination
		paginatedParams := make(map[string]string)
		for k, v := range params {
			paginatedParams[k] = v
		}
		paginatedParams["start"] = strconv.Itoa(start)
		paginatedParams["count"] = strconv.Itoa(count)

		data, err := s.fetch(ctx, endpoint, paginatedParams)
		if err != nil {
			return err
		}
		if data == nil {
			break
		}

		elements, ok := data["elements"].([]interface{})
		if !ok || len(elements) == 0 {
			break
		}

		// Call the callback with elements
		if err := callback(elements); err != nil {
			return err
		}

		// Check if we got less than count (last page)
		if len(elements) < count {
			break
		}

		start += count
	}

	return nil
}

func extractAccountID(account map[string]interface{}) (string, bool) {
	if id, ok := account["id"].(float64); ok {
		// JSON unmarshaling parses numbers as float64
		return strconv.FormatInt(int64(id), 10), true
	}
	if id, ok := account["id"].(int64); ok {
		return strconv.FormatInt(id, 10), true
	}
	return "", false
}

// accountIDToInt64 converts a string account ID to int64 for proper type inference.
func accountIDToInt64(accountID string) interface{} {
	if v, err := strconv.ParseInt(accountID, 10, 64); err == nil {
		return v
	}
	return accountID
}

// ----------------------------------------------------------------------------
// Custom Analytics Helpers

type timeGranularity string

const (
	timeGranularityDaily   timeGranularity = "DAILY"
	timeGranularityMonthly timeGranularity = "MONTHLY"
)

type customAnalyticsConfig struct {
	dimensions      []string
	metrics         []string
	pivot           string
	timeGranularity timeGranularity
	primaryKeys     []string
	incrementalKey  string
}

func parseCustomTableName(tableName string) (*customAnalyticsConfig, error) {
	// Format: custom:<dimensions>:<metrics>
	// Example: custom:campaign,date:impressions,clicks,costInLocalCurrency
	parts := strings.Split(tableName, ":")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid custom table format. Expected: custom:<dimensions>:<metrics>")
	}

	// Parse dimensions
	dimStr := strings.TrimSpace(parts[1])
	var dimensions []string
	for _, d := range strings.Split(dimStr, ",") {
		d = strings.TrimSpace(d)
		if d != "" {
			dimensions = append(dimensions, d)
		}
	}

	// Parse metrics
	metricStr := strings.TrimSpace(parts[2])
	var metrics []string
	for _, m := range strings.Split(metricStr, ",") {
		m = strings.TrimSpace(m)
		if m != "" {
			metrics = append(metrics, m)
		}
	}

	// Validate metrics - at least one is required
	if len(metrics) == 0 {
		return nil, fmt.Errorf("at least one metric is required")
	}

	// Validate dimensions - must have one of campaign, creative, or account
	validDimensions := []string{"campaign", "creative", "account"}
	dimensionIdx := slices.IndexFunc(dimensions, func(d string) bool {
		return slices.Contains(validDimensions, d)
	})
	if dimensionIdx == -1 {
		return nil, fmt.Errorf("'campaign', 'creative' or 'account' is required in dimensions")
	}
	pivot := dimensions[dimensionIdx]

	// Determine time granularity
	var granularity timeGranularity
	var incrementalKey string
	var primaryKeys []string

	switch {
	case slices.Contains(dimensions, "date"):
		granularity = timeGranularityDaily
		incrementalKey = "date"
		primaryKeys = []string{pivot, "date"}
	case slices.Contains(dimensions, "month"):
		granularity = timeGranularityMonthly
		incrementalKey = "start_date"
		primaryKeys = []string{pivot, "start_date", "end_date"}
	default:
		return nil, fmt.Errorf("'date' or 'month' is required in dimensions")
	}

	// Ensure required metrics
	if !slices.Contains(metrics, "dateRange") {
		metrics = append(metrics, "dateRange")
	}
	if !slices.Contains(metrics, "pivotValues") {
		metrics = append(metrics, "pivotValues")
	}

	return &customAnalyticsConfig{
		dimensions:      dimensions,
		metrics:         metrics,
		pivot:           pivot,
		timeGranularity: granularity,
		primaryKeys:     primaryKeys,
		incrementalKey:  incrementalKey,
	}, nil
}

type dateInterval struct {
	start time.Time
	end   time.Time
}

func findIntervals(startDate, endDate time.Time, granularity timeGranularity) ([]dateInterval, error) {
	if startDate.After(endDate) {
		return nil, fmt.Errorf("start date must be before end date")
	}

	var intervals []dateInterval

	current := startDate
	for current.Before(endDate) || current.Equal(endDate) {
		var next time.Time
		if granularity == timeGranularityDaily {
			// 6 months max for daily
			next = current.AddDate(0, 6, 0)
		} else {
			// 2 years max for monthly
			next = current.AddDate(2, 0, 0)
		}

		if next.After(endDate) {
			next = endDate
		}

		intervals = append(intervals, dateInterval{start: current, end: next})
		current = next.AddDate(0, 0, 1)
	}

	return intervals, nil
}

func constructAnalyticsURL(start, end time.Time, accountIDs []string, metrics []string, pivot string, granularity timeGranularity) string {
	dateRange := fmt.Sprintf("(start:(year:%d,month:%d,day:%d),end:(year:%d,month:%d,day:%d))",
		start.Year(), int(start.Month()), start.Day(),
		end.Year(), int(end.Month()), end.Day())

	var encodedAccounts []string
	for _, id := range accountIDs {
		encodedAccounts = append(encodedAccounts, strings.ReplaceAll(fmt.Sprintf("urn:li:sponsoredAccount:%s", id), ":", "%3A"))
	}
	accounts := fmt.Sprintf("List(%s)", strings.Join(encodedAccounts, ","))

	pivotStr := strings.ToUpper(pivot)
	metricsStr := strings.Join(metrics, ",")

	return fmt.Sprintf("/adAnalytics?q=analytics&timeGranularity=%s&dateRange=%s&accounts=%s&pivot=%s&fields=%s",
		granularity, dateRange, accounts, pivotStr, metricsStr)
}

func flattenAnalyticsItems(elements []interface{}, pivot string, granularity timeGranularity) []map[string]interface{} {
	var items []map[string]interface{}

	for _, elem := range elements {
		item, ok := elem.(map[string]interface{})
		if !ok {
			continue
		}

		// Extract pivot values
		if pivotValues, ok := item["pivotValues"].([]interface{}); ok && len(pivotValues) > 0 {
			if len(pivotValues) > 1 {
				item[strings.ToLower(pivot)] = pivotValues
			} else {
				item[strings.ToLower(pivot)] = pivotValues[0]
			}
		}
		delete(item, "pivotValues")

		// Extract date range
		if dateRange, ok := item["dateRange"].(map[string]interface{}); ok {
			if startMap, ok := dateRange["start"].(map[string]interface{}); ok {
				year, yearOk := startMap["year"].(float64)
				month, monthOk := startMap["month"].(float64)
				day, dayOk := startMap["day"].(float64)

				if yearOk && monthOk && dayOk {
					startDt := time.Date(int(year), time.Month(int(month)), int(day), 0, 0, 0, 0, time.UTC)

					if granularity == timeGranularityDaily {
						item["date"] = startDt
					} else {
						item["start_date"] = startDt

						if endMap, ok := dateRange["end"].(map[string]interface{}); ok {
							endYear, endYearOk := endMap["year"].(float64)
							endMonth, endMonthOk := endMap["month"].(float64)
							endDay, endDayOk := endMap["day"].(float64)

							if endYearOk && endMonthOk && endDayOk {
								endDt := time.Date(int(endYear), time.Month(int(endMonth)), int(endDay), 0, 0, 0, 0, time.UTC)
								item["end_date"] = endDt
							}
						}
					}
				}
			}
		}
		delete(item, "dateRange")

		items = append(items, item)
	}

	return items
}
