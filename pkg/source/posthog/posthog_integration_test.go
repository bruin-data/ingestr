//go:build integration

package posthog_test

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

func TestPostHogPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	apiKey := os.Getenv("POSTHOG_API_KEY")
	projectID := os.Getenv("POSTHOG_PROJECT_ID")
	if apiKey == "" || projectID == "" {
		t.Skip("Set POSTHOG_API_KEY and POSTHOG_PROJECT_ID to run PostHog integration tests")
	}

	ctx := context.Background()
	sourceURI := fmt.Sprintf("posthog://?personal_api_key=%s&project_id=%s", apiKey, projectID)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("posthog_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	expectations := []testutil.TableExpectation{
		{
			SourceTable: "persons",
			DestTable:   "main.posthog_persons",
			KeyColumn:   "id",
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "distinct_ids", DataType: schema.TypeJSON},
				{Name: "properties", DataType: schema.TypeJSON},
				{Name: "created_at", DataType: schema.TypeTimestampTZ},
				{Name: "last_seen_at", DataType: schema.TypeTimestampTZ},
			},
			ExpectedRowCount: 5,
			Rows: []testutil.ExpectedRow{
				{
					ID: "22bdb2a9-0d06-57f1-b9ef-1ad05dfd3b58",
					Fields: map[string]any{
						"name":          "test_user_5",
						"is_identified": false,
						"type":          "person",
						"uuid":          "22bdb2a9-0d06-57f1-b9ef-1ad05dfd3b58",
					},
				},
				{
					ID: "9d8f74ca-5328-547d-9cae-52023dd21bd0",
					Fields: map[string]any{
						"name":          "test2@example.com",
						"is_identified": false,
						"type":          "person",
						"uuid":          "9d8f74ca-5328-547d-9cae-52023dd21bd0",
					},
				},
				{
					ID: "5bbb69dd-dd3a-50ea-8db2-daec0d28fed8",
					Fields: map[string]any{
						"name":          "test3@example.com",
						"is_identified": false,
						"type":          "person",
						"uuid":          "5bbb69dd-dd3a-50ea-8db2-daec0d28fed8",
					},
				},
				{
					ID: "19fde908-0a6a-5364-9ad5-0b9f52736a9e",
					Fields: map[string]any{
						"name":          "test_user_4",
						"is_identified": false,
						"type":          "person",
						"uuid":          "19fde908-0a6a-5364-9ad5-0b9f52736a9e",
					},
				},
				{
					ID: "fee0b02a-d4a0-52c8-aebf-78988e68b903",
					Fields: map[string]any{
						"name":          "test1@example.com",
						"is_identified": false,
						"type":          "person",
						"uuid":          "fee0b02a-d4a0-52c8-aebf-78988e68b903",
					},
				},
			},
		},
		{
			SourceTable: "feature_flags",
			DestTable:   "main.posthog_feature_flags",
			KeyColumn:   "id",
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "name", DataType: schema.TypeString},
				{Name: "key", DataType: schema.TypeString},
				{Name: "active", DataType: schema.TypeBoolean},
				{Name: "filters", DataType: schema.TypeJSON},
				{Name: "tags", DataType: schema.TypeJSON},
				{Name: "created_at", DataType: schema.TypeTimestampTZ},
				{Name: "updated_at", DataType: schema.TypeTimestampTZ},
			},
			ExpectedRowCount: 1,
			Rows: []testutil.ExpectedRow{
				{
					ID: "614700",
					Fields: map[string]any{
						"name":                         "Test Flag",
						"key":                          "test-flag",
						"active":                       true,
						"deleted":                      false,
						"ensure_experience_continuity": false,
						"performed_rollback":           false,
						"can_edit":                     true,
						"is_remote_configuration":      false,
						"has_enriched_analytics":       false,
						"has_encrypted_payloads":       false,
						"is_used_in_replay_settings":   false,
						"bucketing_identifier":         "distinct_id",
						"status":                       "ACTIVE",
						"evaluation_runtime":           "all",
						"user_access_level":            "manager",
						"version":                      int64(1),
						"usage_dashboard":              int64(1373520),
					},
				},
			},
		},
		{
			SourceTable: "events",
			DestTable:   "main.posthog_events",
			KeyColumn:   "id",
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "distinct_id", DataType: schema.TypeString},
				{Name: "event", DataType: schema.TypeString},
				{Name: "timestamp", DataType: schema.TypeTimestampTZ},
				{Name: "properties", DataType: schema.TypeJSON},
				{Name: "person", DataType: schema.TypeJSON},
			},
			ExpectedRowCount: 13,
			Rows: []testutil.ExpectedRow{
				{
					ID: "019cffe7-443a-77e6-a458-b3970c8f2b87",
					Fields: map[string]any{
						"distinct_id":    "test_user_3",
						"event":          "$identify",
						"elements_chain": "",
					},
				},
				{
					ID: "019cffe7-4234-7083-938a-b9bbf780c4a6",
					Fields: map[string]any{
						"distinct_id":    "test_user_2",
						"event":          "$identify",
						"elements_chain": "",
					},
				},
				{
					ID: "019cffe7-401c-7708-a21a-ff7de52e01b5",
					Fields: map[string]any{
						"distinct_id":    "test_user_1",
						"event":          "$identify",
						"elements_chain": "",
					},
				},
				{
					ID: "019cffe7-1f51-723d-a3ac-270ce83224ea",
					Fields: map[string]any{
						"distinct_id":    "test_user_5",
						"event":          "test_event_5",
						"elements_chain": "",
					},
				},
				{
					ID: "019cffe7-1d51-7ce8-929c-c9e54fd1dfcc",
					Fields: map[string]any{
						"distinct_id":    "test_user_4",
						"event":          "test_event_4",
						"elements_chain": "",
					},
				},
				{
					ID: "019cffe7-1b4a-72be-b705-a9e11def1725",
					Fields: map[string]any{
						"distinct_id":    "test_user_3",
						"event":          "test_event_3",
						"elements_chain": "",
					},
				},
				{
					ID: "019cffe7-1938-7139-bf70-af53425b3931",
					Fields: map[string]any{
						"distinct_id":    "test_user_2",
						"event":          "test_event_2",
						"elements_chain": "",
					},
				},
				{
					ID: "019cffe7-172f-7bfc-a912-c525b860de88",
					Fields: map[string]any{
						"distinct_id":    "test_user_1",
						"event":          "test_event_1",
						"elements_chain": "",
					},
				},
			},
		},
		{
			SourceTable: "cohorts",
			DestTable:   "main.posthog_cohorts",
			KeyColumn:   "id",
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "name", DataType: schema.TypeString},
				{Name: "created_at", DataType: schema.TypeTimestampTZ},
				{Name: "last_calculation", DataType: schema.TypeTimestampTZ},
				{Name: "is_calculating", DataType: schema.TypeBoolean},
				{Name: "is_static", DataType: schema.TypeBoolean},
			},
			ExpectedRowCount: 1,
			Rows: []testutil.ExpectedRow{
				{
					ID: "224684",
					Fields: map[string]any{
						"name":               "Test Cohort",
						"description":        "Gong test cohort",
						"cohort_type":        "realtime",
						"count":              int64(3),
						"errors_calculating": int64(0),
						"is_calculating":     false,
						"is_static":          false,
						"deleted":            false,
					},
				},
			},
		},
		{
			SourceTable: "event_definitions",
			DestTable:   "main.posthog_event_definitions",
			KeyColumn:   "id",
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "created_at", DataType: schema.TypeTimestampTZ},
				{Name: "last_seen_at", DataType: schema.TypeTimestampTZ},
			},
			ExpectedRowCount: 8,
			Rows: []testutil.ExpectedRow{
				{
					ID: "019cffe7-45bf-7f50-96ce-e41d1cad7a94",
					Fields: map[string]any{
						"name":             "test_event_1",
						"is_action":        false,
						"post_to_slack":    false,
						"enforcement_mode": "allow",
					},
				},
				{
					ID: "019cffe7-4e5f-76c3-8508-1caed566edd3",
					Fields: map[string]any{
						"name":             "test_event_4",
						"is_action":        false,
						"post_to_slack":    false,
						"enforcement_mode": "allow",
					},
				},
				{
					ID: "019cffe7-2d89-7592-befa-725744425cf7",
					Fields: map[string]any{
						"name":             "test_event_3",
						"is_action":        false,
						"post_to_slack":    false,
						"enforcement_mode": "allow",
					},
				},
				{
					ID: "019cffe7-3603-7502-af8a-5a0be01c2d27",
					Fields: map[string]any{
						"name":             "test_event_5",
						"is_action":        false,
						"post_to_slack":    false,
						"enforcement_mode": "allow",
					},
				},
				{
					ID: "019cffe7-561a-7052-bf1f-58d66e002655",
					Fields: map[string]any{
						"name":             "$identify",
						"is_action":        false,
						"post_to_slack":    false,
						"enforcement_mode": "allow",
					},
				},
				{
					ID: "019cffe7-45bf-7f50-96ce-e420a77639a9",
					Fields: map[string]any{
						"name":             "test_event_2",
						"is_action":        false,
						"post_to_slack":    false,
						"enforcement_mode": "allow",
					},
				},
			},
		},
		{
			SourceTable: "property_definitions:event",
			DestTable:   "main.posthog_property_definitions_event",
			KeyColumn:   "id",
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "is_numerical", DataType: schema.TypeBoolean},
			},
			ExpectedRowCount: 33,
			Rows: []testutil.ExpectedRow{
				{
					ID: "019cffe7-2d8c-71e0-9594-24f2678b3c52",
					Fields: map[string]any{
						"name":          "$ip",
						"is_numerical":  false,
						"property_type": "String",
					},
				},
			},
		},
		{
			SourceTable: "property_definitions:person",
			DestTable:   "main.posthog_property_definitions_person",
			KeyColumn:   "id",
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "is_numerical", DataType: schema.TypeBoolean},
			},
			ExpectedRowCount: 51,
			Rows: []testutil.ExpectedRow{
				{
					ID: "019cffe7-561d-7c81-a769-ab2b78ba9044",
					Fields: map[string]any{
						"name":          "email",
						"is_numerical":  false,
						"property_type": "String",
					},
				},
			},
		},
		{
			SourceTable: "annotations",
			DestTable:   "main.posthog_annotations",
			KeyColumn:   "id",
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "content", DataType: schema.TypeString},
				{Name: "date_marker", DataType: schema.TypeTimestampTZ},
				{Name: "created_at", DataType: schema.TypeTimestampTZ},
				{Name: "updated_at", DataType: schema.TypeTimestampTZ},
			},
			ExpectedRowCount: 1,
			Rows: []testutil.ExpectedRow{
				{
					ID: "321402",
					Fields: map[string]any{
						"content":       "Gong test annotation",
						"scope":         "project",
						"deleted":       false,
						"creation_type": "USR",
					},
				},
			},
		},
		{
			SourceTable:      "property_definitions:session",
			DestTable:        "main.posthog_property_definitions_session",
			KeyColumn:        "id",
			ExpectedRowCount: 0,
		},
	}

	for _, exp := range expectations {
		t.Run(exp.SourceTable, func(t *testing.T) {
			testutil.RunPipeline(t, ctx, sourceURI, destURI, exp)
			if exp.ExpectedRowCount > 0 || exp.MinExpectedRowCount > 0 {
				testutil.Check(t, destURI, exp)
			}
		})
	}
}
