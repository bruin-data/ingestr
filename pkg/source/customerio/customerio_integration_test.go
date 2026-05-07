package customerio_test

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

func TestCustomerIOPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	apiKey := os.Getenv("CUSTOMERIO_APP_API_KEY")
	if apiKey == "" {
		t.Skip("Set CUSTOMERIO_APP_API_KEY to run Customer.io integration tests")
	}

	ctx := context.Background()
	sourceURI := fmt.Sprintf("customerio://?api_key=%s", apiKey)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("customerio_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	expectations := []testutil.TableExpectation{
		{
			SourceTable:      "segments",
			DestTable:        "main.customerio_segments",
			KeyColumn:        "id",
			ExpectedRowCount: 10,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "name", DataType: schema.TypeString},
				{Name: "description", DataType: schema.TypeString},
				{Name: "state", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
				{Name: "deduplicate_id", DataType: schema.TypeString},
				{Name: "created_at", DataType: schema.TypeInt64},
				{Name: "updated_at", DataType: schema.TypeTimestampTZ},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "5",
					Fields: map[string]any{
						"name":  "Unsubscribed",
						"state": "finished",
						"type":  "dynamic",
					},
				},
			},
		},
		{
			SourceTable:      "campaigns",
			DestTable:        "main.customerio_campaigns",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "name", DataType: schema.TypeString},
				{Name: "active", DataType: schema.TypeBoolean},
				{Name: "state", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
				{Name: "created", DataType: schema.TypeInt64},
				{Name: "deduplicate_id", DataType: schema.TypeString},
				{Name: "first_started", DataType: schema.TypeInt64},
				{Name: "scheduled_start", DataType: schema.TypeInt64},
				{Name: "scheduled_start_should_backfill", DataType: schema.TypeBoolean},
				{Name: "scheduled_stop", DataType: schema.TypeInt64},
				{Name: "scheduled_stop_should_sunset", DataType: schema.TypeBoolean},
				{Name: "updated", DataType: schema.TypeTimestampTZ},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "1",
					Fields: map[string]any{
						"name":   "Untitled Campaign 1",
						"active": true,
						"state":  "running",
						"type":   "seg_attr",
					},
				},
			},
		},
		{
			SourceTable:      "broadcasts",
			DestTable:        "main.customerio_broadcasts",
			KeyColumn:        "id",
			ExpectedRowCount: 4,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "name", DataType: schema.TypeString},
				{Name: "active", DataType: schema.TypeBoolean},
				{Name: "state", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
				{Name: "created", DataType: schema.TypeInt64},
				{Name: "deduplicate_id", DataType: schema.TypeString},
				{Name: "first_started", DataType: schema.TypeInt64},
				{Name: "scheduled_start", DataType: schema.TypeInt64},
				{Name: "scheduled_start_should_backfill", DataType: schema.TypeBoolean},
				{Name: "scheduled_stop", DataType: schema.TypeInt64},
				{Name: "scheduled_stop_should_sunset", DataType: schema.TypeBoolean},
				{Name: "updated", DataType: schema.TypeTimestampTZ},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "2",
					Fields: map[string]any{
						"name":   "Test Broadcast",
						"active": false,
						"state":  "draft",
						"type":   "triggered_broadcast",
					},
				},
			},
		},
		{
			SourceTable:         "messages",
			DestTable:           "main.customerio_messages",
			KeyColumn:           "id",
			MinExpectedRowCount: 10,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
				{Name: "subject", DataType: schema.TypeString},
				{Name: "recipient", DataType: schema.TypeString},
				{Name: "campaign_id", DataType: schema.TypeInt64},
				{Name: "action_id", DataType: schema.TypeInt64},
				{Name: "customer_id", DataType: schema.TypeString},
				{Name: "created", DataType: schema.TypeInt64},
				{Name: "deduplicate_id", DataType: schema.TypeString},
				{Name: "failure_message", DataType: schema.TypeString},
				{Name: "forgotten", DataType: schema.TypeBoolean},
				{Name: "msg_template_id", DataType: schema.TypeInt64},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "dgTIgA0LAAYFAZ0f2aLWf8cxfAZ5RH9U9g==",
					Fields: map[string]any{
						"type":        "email",
						"campaign_id": int64(1),
						"action_id":   int64(3),
						"forgotten":   false,
					},
				},
			},
		},
		{
			SourceTable:      "sender_identities",
			DestTable:        "main.customerio_sender_identities",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "name", DataType: schema.TypeString},
				{Name: "email", DataType: schema.TypeString},
				{Name: "address", DataType: schema.TypeString},
				{Name: "auto_generated", DataType: schema.TypeBoolean},
				{Name: "deduplicate_id", DataType: schema.TypeString},
				{Name: "phone", DataType: schema.TypeString},
				{Name: "template_type", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "1",
					Fields: map[string]any{
						"name":           "Bruin Test",
						"email":          "vendor_accounts@getbruin.com",
						"auto_generated": false,
						"template_type":  "email",
					},
				},
			},
		},
		{
			SourceTable:      "workspaces",
			DestTable:        "main.customerio_workspaces",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "name", DataType: schema.TypeString},
				{Name: "billable_messages_sent", DataType: schema.TypeInt64},
				{Name: "messages_sent", DataType: schema.TypeInt64},
				{Name: "object_types", DataType: schema.TypeInt64},
				{Name: "objects", DataType: schema.TypeInt64},
				{Name: "people", DataType: schema.TypeInt64},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "213064",
					Fields: map[string]any{
						"name": "Bruin Data",
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
