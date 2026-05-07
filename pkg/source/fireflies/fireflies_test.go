package fireflies

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// TestBuildErrorMap - equivalent to TestExtractItemErrors
// ============================================================================

func TestBuildErrorMap_NoErrors(t *testing.T) {
	// Should return empty map when no errors
	errors := []graphQLError{}

	result := buildErrorMap(errors, "transcripts")

	assert.Empty(t, result)
}

func TestBuildErrorMap_ExtractsPerItemErrors(t *testing.T) {
	// Should extract errors mapped to item indices
	errors := []graphQLError{
		{Path: []interface{}{"transcripts", float64(0), "speakers"}, Message: "Not found"},
		{Path: []interface{}{"transcripts", float64(1), "apps_preview"}, Message: "Access denied"},
	}

	result := buildErrorMap(errors, "transcripts")

	assert.Equal(t, map[int][]string{
		0: {"speakers"},
		1: {"apps_preview"},
	}, result)
}

func TestBuildErrorMap_MultipleErrorsSameItem(t *testing.T) {
	// Should collect multiple errors for the same item
	errors := []graphQLError{
		{Path: []interface{}{"transcripts", float64(0), "speakers"}, Message: "Error 1"},
		{Path: []interface{}{"transcripts", float64(0), "apps_preview"}, Message: "Error 2"},
	}

	result := buildErrorMap(errors, "transcripts")

	assert.Equal(t, map[int][]string{
		0: {"speakers", "apps_preview"},
	}, result)
}

func TestBuildErrorMap_IgnoresNonMatchingRootField(t *testing.T) {
	// Should ignore errors for different root fields
	errors := []graphQLError{
		{Path: []interface{}{"users", float64(0), "email"}, Message: "Error"},
	}

	result := buildErrorMap(errors, "transcripts")

	assert.Empty(t, result)
}

func TestBuildErrorMap_HandlesInvalidPathStructure(t *testing.T) {
	// Should handle paths that don't have expected structure
	errors := []graphQLError{
		{Path: []interface{}{"transcripts"}, Message: "Root error"},           // Too short
		{Path: []interface{}{"transcripts", "invalid"}, Message: "Bad index"}, // Non-numeric index
	}

	result := buildErrorMap(errors, "transcripts")

	assert.Empty(t, result)
}

// ============================================================================
// TestExtractDateRange - Tests for date range extraction
// ============================================================================

func TestExtractDateRange_NoInterval(t *testing.T) {
	opts := source.ReadOptions{}

	dr := extractDateRange(opts)

	assert.Nil(t, dr.start)
	assert.Nil(t, dr.end)
}

func TestExtractDateRange_WithTimeValues(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)
	opts := source.ReadOptions{
		IntervalStart: &start,
		IntervalEnd:   &end,
	}

	dr := extractDateRange(opts)

	require.NotNil(t, dr.start)
	require.NotNil(t, dr.end)
	assert.Equal(t, start, *dr.start)
	assert.Equal(t, end, *dr.end)
}

// ============================================================================
// TestCalculateChunkEnd - Analytics chunking tests
// ============================================================================

func TestCalculateChunkEnd_Default30Days(t *testing.T) {
	s := &FirefliesSource{}
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC) // 60 days later

	chunkEnd := s.calculateChunkEnd(start, end, "")

	// Default should be 30 days
	expected := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, expected, chunkEnd)
}

func TestCalculateChunkEnd_CapsToEndDate(t *testing.T) {
	s := &FirefliesSource{}
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC) // Only 14 days

	chunkEnd := s.calculateChunkEnd(start, end, "")

	// Should cap to end date
	assert.Equal(t, end, chunkEnd)
}

func TestCalculateChunkEnd_HourlyGranularity(t *testing.T) {
	s := &FirefliesSource{}
	start := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 1, 15, 0, 0, 0, time.UTC)

	chunkEnd := s.calculateChunkEnd(start, end, "HOUR")

	// Should be 1 hour later, truncated to hour boundary
	expected := time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC)
	assert.Equal(t, expected, chunkEnd)
}

func TestCalculateChunkEnd_DailyGranularity(t *testing.T) {
	s := &FirefliesSource{}
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)

	chunkEnd := s.calculateChunkEnd(start, end, "DAY")

	// Should be 1 day later
	expected := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, expected, chunkEnd)
}

func TestCalculateChunkEnd_MonthlyGranularity(t *testing.T) {
	s := &FirefliesSource{}
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	chunkEnd := s.calculateChunkEnd(start, end, "MONTH")

	// Should be first day of next month
	expected := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, expected, chunkEnd)
}

func TestCalculateChunkEnd_MonthlyLeapYear(t *testing.T) {
	s := &FirefliesSource{}
	start := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 4, 15, 0, 0, 0, 0, time.UTC)

	chunkEnd := s.calculateChunkEnd(start, end, "MONTH")

	// Should be first day of March
	expected := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, expected, chunkEnd)
}

func TestCalculateChunkEnd_MonthlyCapsToEnd(t *testing.T) {
	s := &FirefliesSource{}
	start := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 20, 0, 0, 0, 0, time.UTC) // End is before next month

	chunkEnd := s.calculateChunkEnd(start, end, "MONTH")

	// Should cap to end date since Feb 1 > Jan 20
	assert.Equal(t, end, chunkEnd)
}

// ============================================================================
// TestIsValidTable - Table validation tests
// ============================================================================

func TestIsValidTable_ValidTables(t *testing.T) {
	validTables := []string{
		"transcripts",
		"users",
		"user_groups",
		"channels",
		"bites",
		"contacts",
		"active_meetings",
		"analytics",
	}

	for _, table := range validTables {
		t.Run(table, func(t *testing.T) {
			assert.True(t, isValidTable(table), "expected %q to be valid", table)
		})
	}
}

func TestIsValidTable_InvalidTables(t *testing.T) {
	invalidTables := []string{
		"invalid_table",
		"",
		"TRANSCRIPTS", // Case sensitive
		"user",        // Wrong name
	}

	for _, table := range invalidTables {
		t.Run(table, func(t *testing.T) {
			assert.False(t, isValidTable(table), "expected %q to be invalid", table)
		})
	}
}

// ============================================================================
// TestParseAPIKeyFromURI - URI parsing tests
// ============================================================================

func TestParseAPIKeyFromURI_ValidURI(t *testing.T) {
	uri := "fireflies://?api_key=test-api-key-123"

	apiKey, err := parseAPIKeyFromURI(uri)

	require.NoError(t, err)
	assert.Equal(t, "test-api-key-123", apiKey)
}

func TestParseAPIKeyFromURI_InvalidScheme(t *testing.T) {
	uri := "postgres://?api_key=test-key"

	_, err := parseAPIKeyFromURI(uri)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid fireflies URI")
}

func TestParseAPIKeyFromURI_MissingAPIKey(t *testing.T) {
	uri := "fireflies://"

	_, err := parseAPIKeyFromURI(uri)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "api_key")
}

func TestParseAPIKeyFromURI_EmptyAPIKey(t *testing.T) {
	uri := "fireflies://?api_key="

	_, err := parseAPIKeyFromURI(uri)

	assert.Error(t, err)
}

// ============================================================================
// TestParseFirefliesTimestamp - Timestamp parsing tests
// ============================================================================

func TestParseFirefliesTimestamp_Milliseconds(t *testing.T) {
	// Fireflies returns timestamps as milliseconds since epoch
	val := float64(1726819200000) // 2024-09-20 08:00:00 UTC

	result := parseFirefliesTimestamp(val)

	require.NotNil(t, result)
	assert.Equal(t, 2024, result.Year())
	assert.Equal(t, time.September, result.Month())
	assert.Equal(t, 20, result.Day())
}

func TestParseFirefliesTimestamp_String(t *testing.T) {
	val := "2024-09-20T08:00:00Z"

	result := parseFirefliesTimestamp(val)

	require.NotNil(t, result)
	assert.Equal(t, 2024, result.Year())
}

func TestParseFirefliesTimestamp_Nil(t *testing.T) {
	result := parseFirefliesTimestamp(nil)

	assert.Nil(t, result)
}

func TestParseFirefliesTimestamp_InvalidString(t *testing.T) {
	result := parseFirefliesTimestamp("not-a-date")

	assert.Nil(t, result)
}

// ============================================================================
// TestTransformTranscript - Transcript transformation tests
// ============================================================================
func TestTransformTranscript_BasicFields(t *testing.T) {
	s := &FirefliesSource{}
	input := map[string]interface{}{
		"id":             "abc123",
		"title":          "Test Meeting",
		"date":           float64(1726819200000),
		"duration":       float64(60),
		"transcript_url": "https://example.com/transcript",
	}

	result := s.transformTranscript(input)

	assert.Equal(t, "abc123", result["id"])
	require.Equal(t, "Test Meeting", result["title"])
	assert.NotNil(t, result["date"])
}

func TestTransformTranscript_NestedFieldsToJSON(t *testing.T) {
	s := &FirefliesSource{}
	input := map[string]interface{}{
		"id": "abc123",
		"speakers": []interface{}{
			map[string]interface{}{"id": 0, "name": "Speaker 1"},
		},
		"participants": []interface{}{"user1@example.com", "user2@example.com"},
	}

	result := s.transformTranscript(input)

	speakers, ok := result["speakers"].([]interface{})
	require.True(t, ok, "speakers should be a list")
	require.Len(t, speakers, 1)
	speaker, ok := speakers[0].(map[string]interface{})
	require.True(t, ok, "speaker should be a map")
	assert.Equal(t, "Speaker 1", speaker["name"])
}

// ============================================================================
// TestExecuteGraphQL - GraphQL execution tests (with mocking)
// ============================================================================

func TestGraphQLError_Marshaling(t *testing.T) {
	errJSON := `{
		"message": "Something went wrong",
		"path": ["transcripts", 0, "speakers"],
		"locations": [{"line": 10, "column": 5}]
	}`

	var gqlErr graphQLError
	err := json.Unmarshal([]byte(errJSON), &gqlErr)

	require.NoError(t, err)
	assert.Equal(t, "Something went wrong", gqlErr.Message)
	assert.Len(t, gqlErr.Path, 3)
	assert.Equal(t, "transcripts", gqlErr.Path[0])
	assert.Equal(t, float64(0), gqlErr.Path[1])
	assert.Equal(t, "speakers", gqlErr.Path[2])
}

func TestGraphQLResponse_WithDataAndErrors(t *testing.T) {
	respJSON := `{
		"data": {"transcripts": [{"id": "123"}]},
		"errors": [{"message": "Partial error", "path": ["transcripts", 0, "speakers"]}]
	}`

	var resp graphQLResponse
	err := json.Unmarshal([]byte(respJSON), &resp)

	require.NoError(t, err)
	assert.NotNil(t, resp.Data)
	assert.Len(t, resp.Errors, 1)
	assert.Equal(t, "Partial error", resp.Errors[0].Message)
}

// ============================================================================
// TestConstants - Verify constants are set correctly
// ============================================================================

func TestConstants(t *testing.T) {
	// Verify important constants have reasonable values
	assert.Equal(t, 50, maxPageSize, "maxPageSize should be 50 (Fireflies API limit)")
	assert.GreaterOrEqual(t, parallelWorkers, 1, "parallelWorkers should be at least 1")
	assert.LessOrEqual(t, parallelWorkers, 10, "parallelWorkers should not exceed 10 to avoid rate limits")
	assert.Equal(t, 30, maxAnalyticsDays, "maxAnalyticsDays should be 30")
}

// ============================================================================
// TestChunkGeneration - Test that chunks are generated correctly
// ============================================================================

func TestChunkGeneration_Daily(t *testing.T) {
	s := &FirefliesSource{}
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC) // 4 days

	// Simulate chunk generation like readAnalytics does
	var chunks []struct{ start, end time.Time }
	currentStart := start
	for currentStart.Before(end) {
		chunkEnd := s.calculateChunkEnd(currentStart, end, "DAY")
		chunks = append(chunks, struct{ start, end time.Time }{currentStart, chunkEnd})
		currentStart = chunkEnd
	}

	assert.Len(t, chunks, 4, "should generate 4 daily chunks for 4 days")
	assert.Equal(t, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), chunks[0].start)
	assert.Equal(t, time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), chunks[0].end)
	assert.Equal(t, time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC), chunks[3].start)
	assert.Equal(t, time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC), chunks[3].end)
}

func TestChunkGeneration_Hourly(t *testing.T) {
	s := &FirefliesSource{}
	start := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 1, 14, 0, 0, 0, time.UTC) // 4 hours

	var chunks []struct{ start, end time.Time }
	currentStart := start
	for currentStart.Before(end) {
		chunkEnd := s.calculateChunkEnd(currentStart, end, "HOUR")
		chunks = append(chunks, struct{ start, end time.Time }{currentStart, chunkEnd})
		currentStart = chunkEnd
	}

	assert.Len(t, chunks, 4, "should generate 4 hourly chunks for 4 hours")
}

func TestChunkGeneration_Monthly(t *testing.T) {
	s := &FirefliesSource{}
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC) // 3 months

	var chunks []struct{ start, end time.Time }
	currentStart := start
	for currentStart.Before(end) {
		chunkEnd := s.calculateChunkEnd(currentStart, end, "MONTH")
		chunks = append(chunks, struct{ start, end time.Time }{currentStart, chunkEnd})
		currentStart = chunkEnd
	}

	assert.Len(t, chunks, 3, "should generate 3 monthly chunks for 3 months")
	// January -> February 1
	assert.Equal(t, time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC), chunks[0].end)
	// February -> March 1
	assert.Equal(t, time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC), chunks[1].end)
	// March -> April 1
	assert.Equal(t, time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC), chunks[2].end)
}

func TestChunkGeneration_Default30Days(t *testing.T) {
	s := &FirefliesSource{}
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC) // 60 days

	var chunks []struct{ start, end time.Time }
	currentStart := start
	for currentStart.Before(end) {
		chunkEnd := s.calculateChunkEnd(currentStart, end, "") // Default granularity
		chunks = append(chunks, struct{ start, end time.Time }{currentStart, chunkEnd})
		currentStart = chunkEnd
	}

	assert.Len(t, chunks, 2, "should generate 2 chunks for 60 days with 30-day default")
}

// ============================================================================
// TestDateRangeStruct - Test dateRange struct
// ============================================================================

func TestDateRange_BothNil(t *testing.T) {
	dr := dateRange{}
	assert.Nil(t, dr.start)
	assert.Nil(t, dr.end)
}

func TestDateRange_WithValues(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)
	dr := dateRange{start: &start, end: &end}

	require.NotNil(t, dr.start)
	require.NotNil(t, dr.end)
	assert.Equal(t, 2024, dr.start.Year())
	assert.Equal(t, time.December, dr.end.Month())
}
