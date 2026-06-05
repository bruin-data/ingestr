//go:build integration

package mailchimp_test

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

func TestMailchimpPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	apiKey := os.Getenv("MAILCHIMP_API_KEY")
	server := os.Getenv("MAILCHIMP_SERVER")
	if apiKey == "" || server == "" {
		t.Skip("Set MAILCHIMP_API_KEY and MAILCHIMP_SERVER to run Mailchimp integration tests")
	}

	ctx := context.Background()
	sourceURI := fmt.Sprintf("mailchimp://?api_key=%s&server=%s", apiKey, server)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("mailchimp_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	expectations := []testutil.TableExpectation{
		{
			SourceTable:      "account",
			DestTable:        "main.mailchimp_account",
			KeyColumn:        "account_id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "account_id", DataType: schema.TypeString},
				{Name: "account_industry", DataType: schema.TypeString},
				{Name: "account_name", DataType: schema.TypeString},
				{Name: "account_timezone", DataType: schema.TypeString},
				{Name: "avatar_url", DataType: schema.TypeString},
				{Name: "email", DataType: schema.TypeString},
				{Name: "first_name", DataType: schema.TypeString},
				{Name: "first_payment", DataType: schema.TypeTimestampTZ},
				{Name: "last_login", DataType: schema.TypeTimestampTZ},
				{Name: "last_name", DataType: schema.TypeString},
				{Name: "login_id", DataType: schema.TypeString},
				{Name: "member_since", DataType: schema.TypeTimestampTZ},
				{Name: "pricing_plan_type", DataType: schema.TypeString},
				{Name: "pro_enabled", DataType: schema.TypeBoolean},
				{Name: "role", DataType: schema.TypeString},
				{Name: "total_subscribers", DataType: schema.TypeInt64},
				{Name: "username", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "037e052834d48fe438d196491",
					Fields: map[string]any{
						"account_name":      "Irem Cagin",
						"account_timezone":  "America/New_York",
						"email":             "cagin.yurtturk@getbruin.com",
						"pricing_plan_type": "monthly",
						"pro_enabled":       true,
						"role":              "owner",
					},
				},
			},
		},
		{
			SourceTable:      "audiences",
			DestTable:        "main.mailchimp_audiences",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "beamer_address", DataType: schema.TypeString},
				{Name: "date_created", DataType: schema.TypeTimestampTZ},
				{Name: "double_optin", DataType: schema.TypeBoolean},
				{Name: "email_type_option", DataType: schema.TypeBoolean},
				{Name: "has_welcome", DataType: schema.TypeBoolean},
				{Name: "id", DataType: schema.TypeString},
				{Name: "list_rating", DataType: schema.TypeInt64},
				{Name: "marketing_permissions", DataType: schema.TypeBoolean},
				{Name: "name", DataType: schema.TypeString},
				{Name: "notify_on_subscribe", DataType: schema.TypeString},
				{Name: "notify_on_unsubscribe", DataType: schema.TypeString},
				{Name: "permission_reminder", DataType: schema.TypeString},
				{Name: "subscribe_url_long", DataType: schema.TypeString},
				{Name: "subscribe_url_short", DataType: schema.TypeString},
				{Name: "use_archive_bar", DataType: schema.TypeBoolean},
				{Name: "visibility", DataType: schema.TypeString},
				{Name: "web_id", DataType: schema.TypeInt64},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "4f74fbb762",
					Fields: map[string]any{
						"name":                  "Irem Cagin",
						"double_optin":          false,
						"email_type_option":     false,
						"has_welcome":           false,
						"visibility":            "prv",
						"use_archive_bar":       true,
						"list_rating":           int64(0),
						"marketing_permissions": false,
					},
				},
			},
		},
		{
			SourceTable:      "campaigns",
			DestTable:        "main.mailchimp_campaigns",
			KeyColumn:        "id",
			ExpectedRowCount: 4,
			ExpectedSchema: []schema.Column{
				{Name: "archive_url", DataType: schema.TypeString},
				{Name: "content_type", DataType: schema.TypeString},
				{Name: "create_time", DataType: schema.TypeTimestampTZ},
				{Name: "emails_sent", DataType: schema.TypeInt64},
				{Name: "id", DataType: schema.TypeString},
				{Name: "long_archive_url", DataType: schema.TypeString},
				{Name: "needs_block_refresh", DataType: schema.TypeBoolean},
				{Name: "resendable", DataType: schema.TypeBoolean},
				{Name: "send_time", DataType: schema.TypeString},
				{Name: "status", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
				{Name: "web_id", DataType: schema.TypeInt64},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "0126a5cbb6",
					Fields: map[string]any{
						"content_type":        "multichannel",
						"emails_sent":         int64(0),
						"needs_block_refresh": false,
						"resendable":          false,
						"status":              "save",
						"type":                "regular",
					},
				},
			},
		},
		{
			SourceTable:      "campaign_folders",
			DestTable:        "main.mailchimp_campaign_folders",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "count", DataType: schema.TypeInt64},
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "293bb21b18",
					Fields: map[string]any{
						"name":  "Test Folder",
						"count": int64(0),
					},
				},
			},
		},
		{
			SourceTable:      "lists_merge_fields",
			DestTable:        "main.mailchimp_lists_merge_fields",
			KeyColumn:        "merge_id",
			ExpectedRowCount: 6,
			ExpectedSchema: []schema.Column{
				{Name: "audiences_id", DataType: schema.TypeString},
				{Name: "default_value", DataType: schema.TypeString},
				{Name: "display_order", DataType: schema.TypeInt64},
				{Name: "help_text", DataType: schema.TypeString},
				{Name: "list_id", DataType: schema.TypeString},
				{Name: "merge_id", DataType: schema.TypeInt64},
				{Name: "name", DataType: schema.TypeString},
				{Name: "public", DataType: schema.TypeBoolean},
				{Name: "required", DataType: schema.TypeBoolean},
				{Name: "tag", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "3",
					Fields: map[string]any{
						"audiences_id":  "4f74fbb762",
						"list_id":       "4f74fbb762",
						"name":          "Address",
						"tag":           "ADDRESS",
						"type":          "address",
						"public":        false,
						"required":      false,
						"display_order": int64(4),
						"default_value": "",
						"help_text":     "",
					},
				},
			},
		},
		{
			SourceTable:      "lists_segments",
			DestTable:        "main.mailchimp_lists_segments",
			KeyColumn:        "id",
			ExpectedRowCount: 4,
			ExpectedSchema: []schema.Column{
				{Name: "audiences_id", DataType: schema.TypeString},
				{Name: "created_at", DataType: schema.TypeTimestampTZ},
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "list_id", DataType: schema.TypeString},
				{Name: "member_count", DataType: schema.TypeInt64},
				{Name: "name", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
				{Name: "updated_at", DataType: schema.TypeTimestampTZ},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "1867423",
					Fields: map[string]any{
						"audiences_id": "4f74fbb762",
						"list_id":      "4f74fbb762",
						"name":         "Customer",
						"type":         "static",
						"member_count": int64(3),
					},
				},
			},
		},
		{
			SourceTable:      "lists_interest_categories",
			DestTable:        "main.mailchimp_lists_interest_categories",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "audiences_id", DataType: schema.TypeString},
				{Name: "display_order", DataType: schema.TypeInt64},
				{Name: "id", DataType: schema.TypeString},
				{Name: "list_id", DataType: schema.TypeString},
				{Name: "title", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "54847a287e",
					Fields: map[string]any{
						"audiences_id":  "4f74fbb762",
						"list_id":       "4f74fbb762",
						"title":         "Product Interests",
						"type":          "checkboxes",
						"display_order": int64(0),
					},
				},
			},
		},
		{
			SourceTable:      "lists_activity",
			DestTable:        "main.mailchimp_lists_activity",
			KeyColumn:        "day",
			ExpectedRowCount: 2,
			ExpectedSchema: []schema.Column{
				{Name: "audiences_id", DataType: schema.TypeString},
				{Name: "day", DataType: schema.TypeDate},
				{Name: "emails_sent", DataType: schema.TypeInt64},
				{Name: "hard_bounce", DataType: schema.TypeInt64},
				{Name: "other_adds", DataType: schema.TypeInt64},
				{Name: "other_removes", DataType: schema.TypeInt64},
				{Name: "recipient_clicks", DataType: schema.TypeInt64},
				{Name: "soft_bounce", DataType: schema.TypeInt64},
				{Name: "subs", DataType: schema.TypeInt64},
				{Name: "unique_opens", DataType: schema.TypeInt64},
				{Name: "unsubs", DataType: schema.TypeInt64},
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
