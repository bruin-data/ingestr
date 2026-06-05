//go:build integration

package mixpanel_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/testutil"
	"github.com/bruin-data/ingestr/pkg/schema"
)

func TestMixpanelPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	username := os.Getenv("MIXPANEL_USERNAME")
	password := os.Getenv("MIXPANEL_PASSWORD")
	projectID := os.Getenv("MIXPANEL_PROJECT_ID")
	if username == "" || password == "" || projectID == "" {
		t.Skip("Set MIXPANEL_USERNAME, MIXPANEL_PASSWORD, and MIXPANEL_PROJECT_ID to run Mixpanel integration tests")
	}

	ctx := context.Background()
	sourceURI := fmt.Sprintf("mixpanel://?username=%s&password=%s&project_id=%s&server=us", username, password, projectID)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("mixpanel_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	expectations := []testutil.TableExpectation{
		{
			SourceTable:      "events",
			DestTable:        "main.mixpanel_events",
			KeyColumn:        "distinct_id",
			ExpectedRowCount: 5,
			ExpectedSchema: []schema.Column{
				{Name: "amount", DataType: schema.TypeFloat64},
				{Name: "browser", DataType: schema.TypeString},
				{Name: "city", DataType: schema.TypeString},
				{Name: "country_code", DataType: schema.TypeString},
				{Name: "currency", DataType: schema.TypeString},
				{Name: "device", DataType: schema.TypeString},
				{Name: "distinct_id", DataType: schema.TypeString},
				{Name: "duration_seconds", DataType: schema.TypeInt64},
				{Name: "error_code", DataType: schema.TypeInt64},
				{Name: "error_message", DataType: schema.TypeString},
				{Name: "event", DataType: schema.TypeString},
				{Name: "feature", DataType: schema.TypeString},
				{Name: "import", DataType: schema.TypeBoolean},
				{Name: "insert_id", DataType: schema.TypeString},
				{Name: "item", DataType: schema.TypeString},
				{Name: "mp_api_endpoint", DataType: schema.TypeString},
				{Name: "mp_api_timestamp_ms", DataType: schema.TypeInt64},
				{Name: "mp_event_size", DataType: schema.TypeInt64},
				{Name: "mp_processing_time_ms", DataType: schema.TypeInt64},
				{Name: "os", DataType: schema.TypeString},
				{Name: "page", DataType: schema.TypeString},
				{Name: "plan", DataType: schema.TypeString},
				{Name: "referrer", DataType: schema.TypeString},
				{Name: "region", DataType: schema.TypeString},
				{Name: "source", DataType: schema.TypeString},
				{Name: "success", DataType: schema.TypeBoolean},
				{Name: "time", DataType: schema.TypeInt64},
				{Name: "user_id", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "user_003",
					Fields: map[string]any{
						"event":           "signup",
						"import":          true,
						"insert_id":       "ins_007",
						"mp_api_endpoint": "api.mixpanel.com",
						"city":            "London",
						"plan":            "enterprise",
						"source":          "referral",
						"user_id":         "user_003",
					},
				},
				{
					ID: "user_004",
					Fields: map[string]any{
						"event":        "signup",
						"import":       true,
						"insert_id":    "ins_011",
						"city":         "Berlin",
						"plan":         "starter",
						"source":       "organic",
						"country_code": "DE",
						"user_id":      "user_004",
					},
				},
				{
					ID: "user_005",
					Fields: map[string]any{
						"event":    "purchase",
						"import":   true,
						"amount":   199.99,
						"currency": "EUR",
						"item":     "annual_plan",
						"region":   "Bavaria",
						"user_id":  "user_005",
					},
				},
			},
		},
		{
			SourceTable:      "profiles",
			DestTable:        "main.mixpanel_profiles",
			KeyColumn:        "distinct_id",
			ExpectedRowCount: 5,
			ExpectedSchema: []schema.Column{
				{Name: "active", DataType: schema.TypeBoolean},
				{Name: "city", DataType: schema.TypeString},
				{Name: "country_code", DataType: schema.TypeString},
				{Name: "distinct_id", DataType: schema.TypeString},
				{Name: "email", DataType: schema.TypeString},
				{Name: "first_name", DataType: schema.TypeString},
				{Name: "last_name", DataType: schema.TypeString},
				{Name: "last_seen", DataType: schema.TypeTimestampTZ},
				{Name: "plan", DataType: schema.TypeString},
				{Name: "region", DataType: schema.TypeString},
				{Name: "signup_date", DataType: schema.TypeTimestampTZ},
				{Name: "timezone", DataType: schema.TypeString},
				{Name: "user_id", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "user_001",
					Fields: map[string]any{
						"first_name":   "Alice",
						"last_name":    "Smith",
						"email":        "alice@example.com",
						"city":         "San Francisco",
						"country_code": "TR",
						"plan":         "free",
						"region":       "Istanbul",
						"timezone":     "Europe/Istanbul",
						"user_id":      "user_001",
					},
				},
				{
					ID: "user_004",
					Fields: map[string]any{
						"first_name":   "Diana",
						"last_name":    "Mueller",
						"email":        "diana@example.de",
						"city":         "Berlin",
						"country_code": "DE",
						"plan":         "starter",
						"active":       true,
						"user_id":      "user_004",
					},
				},
				{
					ID: "user_005",
					Fields: map[string]any{
						"first_name":   "Eve",
						"last_name":    "Johnson",
						"email":        "eve@example.com",
						"city":         "Chicago",
						"country_code": "US",
						"plan":         "annual",
						"active":       false,
						"user_id":      "user_005",
					},
				},
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
