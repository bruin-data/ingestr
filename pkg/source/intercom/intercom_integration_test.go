package intercom_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bruin-data/gong/internal/testutil"
	"github.com/bruin-data/gong/pkg/schema"
)

func TestIntercomPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	accessToken := os.Getenv("INTERCOM_ACCESS_TOKEN")
	if accessToken == "" {
		t.Skip("Set INTERCOM_ACCESS_TOKEN to run Intercom integration tests")
	}

	ctx := context.Background()
	sourceURI := fmt.Sprintf("intercom://?access_token=%s", accessToken)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("intercom_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	expectations := []testutil.TableExpectation{
		{
			SourceTable:      "contacts",
			DestTable:        "main.intercom_contacts",
			KeyColumn:        "id",
			ExpectedRowCount: 6,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "email", DataType: schema.TypeString},
				{Name: "role", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
				{Name: "workspace_id", DataType: schema.TypeString},
				{Name: "has_hard_bounced", DataType: schema.TypeBoolean},
				{Name: "unsubscribed_from_emails", DataType: schema.TypeBoolean},
				{Name: "created_at", DataType: schema.TypeInt64},
				{Name: "updated_at", DataType: schema.TypeInt64},
				{Name: "companies_count", DataType: schema.TypeInt64},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "69c389cbab464d83d1324371",
					Fields: map[string]any{
						"name":                     "Test Lead",
						"email":                    "testlead@example.com",
						"role":                     "lead",
						"type":                     "contact",
						"has_hard_bounced":         false,
						"unsubscribed_from_emails": false,
					},
				},
			},
		},
		{
			SourceTable:      "companies",
			DestTable:        "main.intercom_companies",
			KeyColumn:        "id",
			ExpectedRowCount: 4,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "company_id", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
				{Name: "app_id", DataType: schema.TypeString},
				{Name: "created_at", DataType: schema.TypeInt64},
				{Name: "updated_at", DataType: schema.TypeInt64},
				{Name: "monthly_spend", DataType: schema.TypeInt64},
				{Name: "session_count", DataType: schema.TypeInt64},
				{Name: "user_count", DataType: schema.TypeInt64},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "69c3878392c22b2611f6faab",
					Fields: map[string]any{
						"name":   "[Demo]",
						"type":   "company",
						"app_id": "k8yj51wz",
					},
				},
			},
		},
		{
			SourceTable:         "conversations",
			DestTable:           "main.intercom_conversations",
			KeyColumn:           "id",
			MinExpectedRowCount: 4,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
				{Name: "state", DataType: schema.TypeString},
				{Name: "open", DataType: schema.TypeBoolean},
				{Name: "priority", DataType: schema.TypeString},
				{Name: "created_at", DataType: schema.TypeInt64},
				{Name: "updated_at", DataType: schema.TypeInt64},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "215473625008659",
					Fields: map[string]any{
						"type":     "conversation",
						"state":    "open",
						"open":     true,
						"priority": "not_priority",
					},
				},
			},
		},
		{
			SourceTable:      "articles",
			DestTable:        "main.intercom_articles",
			KeyColumn:        "id",
			ExpectedRowCount: 2,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "title", DataType: schema.TypeString},
				{Name: "state", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
				{Name: "workspace_id", DataType: schema.TypeString},
				{Name: "created_at", DataType: schema.TypeInt64},
				{Name: "updated_at", DataType: schema.TypeInt64},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "14192613",
					Fields: map[string]any{
						"title": "Getting Started Guide",
						"state": "published",
						"type":  "article",
					},
				},
			},
		},
		{
			SourceTable:      "tags",
			DestTable:        "main.intercom_tags",
			KeyColumn:        "id",
			ExpectedRowCount: 2,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "14187913",
					Fields: map[string]any{
						"name": "test-tag",
						"type": "tag",
					},
				},
			},
		},
		{
			SourceTable:      "segments",
			DestTable:        "main.intercom_segments",
			KeyColumn:        "id",
			ExpectedRowCount: 3,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
				{Name: "person_type", DataType: schema.TypeString},
				{Name: "created_at", DataType: schema.TypeInt64},
				{Name: "updated_at", DataType: schema.TypeInt64},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "69c3878266067f0046e1ca88",
					Fields: map[string]any{
						"name":        "Active",
						"type":        "segment",
						"person_type": "user",
					},
				},
			},
		},
		{
			SourceTable:      "admins",
			DestTable:        "main.intercom_admins",
			KeyColumn:        "id",
			ExpectedRowCount: 2,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "email", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
				{Name: "has_inbox_seat", DataType: schema.TypeBoolean},
				{Name: "away_mode_enabled", DataType: schema.TypeBoolean},
				{Name: "away_mode_reassign", DataType: schema.TypeBoolean},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "10217387",
					Fields: map[string]any{
						"name":               "Bruin Vendor",
						"email":              "vendor_accounts@getbruin.com",
						"type":               "admin",
						"has_inbox_seat":     false,
						"away_mode_enabled":  false,
						"away_mode_reassign": false,
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
