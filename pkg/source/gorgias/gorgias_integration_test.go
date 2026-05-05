package gorgias_test

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

func TestGorgiasPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	domain := os.Getenv("GORGIAS_DOMAIN")
	email := os.Getenv("GORGIAS_EMAIL")
	apiKey := os.Getenv("GORGIAS_API_KEY")
	if domain == "" || email == "" || apiKey == "" {
		t.Skip("Set GORGIAS_DOMAIN, GORGIAS_EMAIL, and GORGIAS_API_KEY to run Gorgias integration tests")
	}

	ctx := context.Background()
	sourceURI := fmt.Sprintf("gorgias://%s?api_key=%s&email=%s", domain, apiKey, email)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("gorgias_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	expectations := []testutil.TableExpectation{
		{
			SourceTable: "customers",
			DestTable:   "main.gorgias_customers",
			KeyColumn:   "id",
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "external_id", DataType: schema.TypeString},
				{Name: "active", DataType: schema.TypeBoolean},
				{Name: "email", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "firstname", DataType: schema.TypeString},
				{Name: "lastname", DataType: schema.TypeString},
				{Name: "language", DataType: schema.TypeString},
				{Name: "timezone", DataType: schema.TypeString},
				{Name: "created_datetime", DataType: schema.TypeTimestampTZ},
				{Name: "updated_datetime", DataType: schema.TypeTimestampTZ},
				{Name: "meta", DataType: schema.TypeJSON},
				{Name: "data", DataType: schema.TypeJSON},
				{Name: "note", DataType: schema.TypeString},
			},
			MinExpectedRowCount: 2,
			Rows: []testutil.ExpectedRow{
				{
					ID: "358036840",
					Fields: map[string]any{
						"email":     "testcustomer@example.com",
						"name":      "Test Customer Gong",
						"firstname": "Test",
						"lastname":  "Gong",
						"language":  "en",
						"timezone":  "UTC",
						"active":    true,
					},
				},
			},
		},
		{
			SourceTable: "tickets",
			DestTable:   "main.gorgias_tickets",
			KeyColumn:   "id",
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "uri", DataType: schema.TypeString},
				{Name: "external_id", DataType: schema.TypeString},
				{Name: "language", DataType: schema.TypeString},
				{Name: "status", DataType: schema.TypeString},
				{Name: "priority", DataType: schema.TypeString},
				{Name: "channel", DataType: schema.TypeString},
				{Name: "via", DataType: schema.TypeString},
				{Name: "from_agent", DataType: schema.TypeBoolean},
				{Name: "customer", DataType: schema.TypeJSON},
				{Name: "assignee_user", DataType: schema.TypeJSON},
				{Name: "assignee_team", DataType: schema.TypeJSON},
				{Name: "subject", DataType: schema.TypeString},
				{Name: "excerpt", DataType: schema.TypeString},
				{Name: "integrations", DataType: schema.TypeJSON},
				{Name: "meta", DataType: schema.TypeJSON},
				{Name: "tags", DataType: schema.TypeJSON},
				{Name: "messages_count", DataType: schema.TypeInt64},
				{Name: "is_unread", DataType: schema.TypeBoolean},
				{Name: "spam", DataType: schema.TypeBoolean},
				{Name: "created_datetime", DataType: schema.TypeTimestampTZ},
				{Name: "opened_datetime", DataType: schema.TypeTimestampTZ},
				{Name: "last_received_message_datetime", DataType: schema.TypeTimestampTZ},
				{Name: "last_message_datetime", DataType: schema.TypeTimestampTZ},
				{Name: "updated_datetime", DataType: schema.TypeTimestampTZ},
				{Name: "closed_datetime", DataType: schema.TypeTimestampTZ},
				{Name: "snooze_datetime", DataType: schema.TypeTimestampTZ},
				{Name: "trashed_datetime", DataType: schema.TypeTimestampTZ},
			},
			MinExpectedRowCount: 1,
			Rows: []testutil.ExpectedRow{
				{
					ID: "47423971",
					Fields: map[string]any{
						"uri":            "/api/tickets/47423971/",
						"status":         "open",
						"priority":       "normal",
						"channel":        "email",
						"via":            "email",
						"subject":        "Let's send your first message!",
						"messages_count": int64(1),
						"is_unread":      true,
						"spam":           false,
						"from_agent":     false,
					},
				},
			},
		},
		{
			SourceTable: "ticket_messages",
			DestTable:   "main.gorgias_ticket_messages",
			KeyColumn:   "id",
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "uri", DataType: schema.TypeString},
				{Name: "message_id", DataType: schema.TypeString},
				{Name: "ticket_id", DataType: schema.TypeInt64},
				{Name: "external_id", DataType: schema.TypeString},
				{Name: "public", DataType: schema.TypeBoolean},
				{Name: "channel", DataType: schema.TypeString},
				{Name: "via", DataType: schema.TypeString},
				{Name: "sender", DataType: schema.TypeJSON},
				{Name: "integration_id", DataType: schema.TypeInt64},
				{Name: "intents", DataType: schema.TypeJSON},
				{Name: "rule_id", DataType: schema.TypeInt64},
				{Name: "from_agent", DataType: schema.TypeBoolean},
				{Name: "receiver", DataType: schema.TypeJSON},
				{Name: "subject", DataType: schema.TypeString},
				{Name: "body_text", DataType: schema.TypeString},
				{Name: "body_html", DataType: schema.TypeString},
				{Name: "stripped_text", DataType: schema.TypeString},
				{Name: "stripped_html", DataType: schema.TypeString},
				{Name: "stripped_signature", DataType: schema.TypeString},
				{Name: "headers", DataType: schema.TypeJSON},
				{Name: "attachments", DataType: schema.TypeJSON},
				{Name: "actions", DataType: schema.TypeJSON},
				{Name: "macros", DataType: schema.TypeJSON},
				{Name: "meta", DataType: schema.TypeJSON},
				{Name: "created_datetime", DataType: schema.TypeTimestampTZ},
				{Name: "sent_datetime", DataType: schema.TypeTimestampTZ},
				{Name: "failed_datetime", DataType: schema.TypeTimestampTZ},
				{Name: "deleted_datetime", DataType: schema.TypeTimestampTZ},
				{Name: "opened_datetime", DataType: schema.TypeTimestampTZ},
				{Name: "last_sending_error", DataType: schema.TypeString},
				{Name: "is_retriable", DataType: schema.TypeBoolean},
			},
			MinExpectedRowCount: 1,
			Rows: []testutil.ExpectedRow{
				{
					ID: "123643926",
					Fields: map[string]any{
						"uri":        "/api/tickets/47423971/messages/123643926/",
						"ticket_id":  int64(47423971),
						"public":     true,
						"channel":    "email",
						"via":        "email",
						"from_agent": false,
						"subject":    "Can't login. Please help",
					},
				},
			},
		},
		{
			SourceTable: "satisfaction_surveys",
			DestTable:   "main.gorgias_satisfaction_surveys",
			KeyColumn:   "id",
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "body_text", DataType: schema.TypeString},
				{Name: "created_datetime", DataType: schema.TypeTimestampTZ},
				{Name: "customer_id", DataType: schema.TypeInt64},
				{Name: "meta", DataType: schema.TypeJSON},
				{Name: "score", DataType: schema.TypeFloat64},
				{Name: "scored_datetime", DataType: schema.TypeTimestampTZ},
				{Name: "sent_datetime", DataType: schema.TypeTimestampTZ},
				{Name: "should_send_datetime", DataType: schema.TypeTimestampTZ},
				{Name: "ticket_id", DataType: schema.TypeInt64},
				{Name: "uri", DataType: schema.TypeString},
			},
			MinExpectedRowCount: 1,
			Rows: []testutil.ExpectedRow{
				{
					ID: "3416057",
					Fields: map[string]any{
						"body_text":   "Great support, very helpful!",
						"customer_id": int64(358036840),
						"score":       float64(5),
						"ticket_id":   int64(47423971),
						"uri":         "/api/satisfaction-surveys/3416057/",
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
