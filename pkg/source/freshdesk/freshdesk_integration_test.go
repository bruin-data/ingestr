package freshdesk_test

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

func TestFreshdeskPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	subdomain := os.Getenv("FRESHDESK_SUBDOMAIN")
	apiKey := os.Getenv("FRESHDESK_API_KEY")
	if subdomain == "" || apiKey == "" {
		t.Skip("Set FRESHDESK_SUBDOMAIN and FRESHDESK_API_KEY to run Freshdesk integration tests")
	}

	ctx := context.Background()
	sourceURI := fmt.Sprintf("freshdesk://%s?api_key=%s", subdomain, apiKey)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("freshdesk_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	expectations := []testutil.TableExpectation{
		{
			SourceTable:      "tickets",
			DestTable:        "main.freshdesk_tickets",
			KeyColumn:        "id",
			ExpectedRowCount: 3,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "subject", DataType: schema.TypeString},
				{Name: "status", DataType: schema.TypeInt64},
				{Name: "priority", DataType: schema.TypeInt64},
				{Name: "source", DataType: schema.TypeInt64},
				{Name: "type", DataType: schema.TypeString},
				{Name: "requester_id", DataType: schema.TypeInt64},
				{Name: "responder_id", DataType: schema.TypeInt64},
				{Name: "company_id", DataType: schema.TypeInt64},
				{Name: "description", DataType: schema.TypeString},
				{Name: "description_text", DataType: schema.TypeString},
				{Name: "created_at", DataType: schema.TypeTimestampTZ},
				{Name: "updated_at", DataType: schema.TypeTimestampTZ},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "1",
					Fields: map[string]any{
						"subject":  "404 error when on a specific page",
						"status":   int64(2),
						"priority": int64(4),
						"type":     "Incident",
					},
				},
				{
					ID: "2",
					Fields: map[string]any{
						"subject":      "Authentication failure",
						"status":       int64(2),
						"priority":     int64(4),
						"type":         "Question",
						"requester_id": int64(68002157637),
						"responder_id": int64(68002068646),
						"company_id":   int64(68000153346),
					},
				},
				{
					ID: "3",
					Fields: map[string]any{
						"subject":      "Issues with reports",
						"status":       int64(2),
						"priority":     int64(1),
						"type":         "Problem",
						"requester_id": int64(68002157638),
						"responder_id": int64(68002068646),
						"company_id":   int64(68000153345),
					},
				},
			},
		},
		{
			SourceTable:      "agents",
			DestTable:        "main.freshdesk_agents",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "type", DataType: schema.TypeString},
				{Name: "available", DataType: schema.TypeBoolean},
				{Name: "occasional", DataType: schema.TypeBoolean},
				{Name: "ticket_scope", DataType: schema.TypeInt64},
				{Name: "contact", DataType: schema.TypeJSON},
				{Name: "created_at", DataType: schema.TypeTimestampTZ},
				{Name: "updated_at", DataType: schema.TypeTimestampTZ},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "68002068646",
					Fields: map[string]any{
						"type":         "support_agent",
						"available":    false,
						"occasional":   false,
						"ticket_scope": int64(1),
					},
				},
			},
		},
		{
			SourceTable:      "contacts",
			DestTable:        "main.freshdesk_contacts",
			KeyColumn:        "id",
			ExpectedRowCount: 5,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "name", DataType: schema.TypeString},
				{Name: "email", DataType: schema.TypeString},
				{Name: "active", DataType: schema.TypeBoolean},
				{Name: "job_title", DataType: schema.TypeString},
				{Name: "company_id", DataType: schema.TypeInt64},
				{Name: "created_at", DataType: schema.TypeTimestampTZ},
				{Name: "updated_at", DataType: schema.TypeTimestampTZ},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "68002157638",
					Fields: map[string]any{
						"name":       "Bob Tree",
						"email":      "bob.tree@freshdesk.com",
						"active":     true,
						"job_title":  "CEO",
						"company_id": int64(68000153345),
					},
				},
				{
					ID: "68002157637",
					Fields: map[string]any{
						"name":       "Emily Garcia",
						"email":      "emily.garcia@acme.com",
						"active":     true,
						"job_title":  "Associate Director",
						"company_id": int64(68000153346),
					},
				},
				{
					ID: "68002157642",
					Fields: map[string]any{
						"name":       "Johnny Appleseed",
						"email":      "johnny.appleseed@jpl.gov",
						"active":     true,
						"job_title":  "Manager Customer Support",
						"company_id": int64(68000153349),
					},
				},
			},
		},
		{
			SourceTable:      "companies",
			DestTable:        "main.freshdesk_companies",
			KeyColumn:        "id",
			ExpectedRowCount: 5,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "name", DataType: schema.TypeString},
				{Name: "description", DataType: schema.TypeString},
				{Name: "created_at", DataType: schema.TypeTimestampTZ},
				{Name: "updated_at", DataType: schema.TypeTimestampTZ},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "68000153346",
					Fields: map[string]any{
						"name":        "Acme Inc",
						"description": "Acme Inc is a versatile manufacturing company known for producing a wide range of industrial and consumer products, emphasizing quality and innovation.",
					},
				},
				{
					ID: "68000153345",
					Fields: map[string]any{
						"name":        "Freshworks",
						"description": "Freshworks provides innovative customer engagement software for businesses of all sizes, focusing on customer support, sales, and marketing solutions.",
					},
				},
				{
					ID: "68000153349",
					Fields: map[string]any{
						"name":        "Jet Propulsion Laboratory , NASA",
						"description": "Jet Propulsion Laboratory (JPL) is a renowned research and development center managed by NASA, leading missions in planetary exploration and space technology advancements.",
					},
				},
			},
		},
		{
			SourceTable:      "roles",
			DestTable:        "main.freshdesk_roles",
			KeyColumn:        "id",
			ExpectedRowCount: 7,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "name", DataType: schema.TypeString},
				{Name: "description", DataType: schema.TypeString},
				{Name: "default", DataType: schema.TypeBoolean},
				{Name: "agent_type", DataType: schema.TypeInt64},
				{Name: "created_at", DataType: schema.TypeTimestampTZ},
				{Name: "updated_at", DataType: schema.TypeTimestampTZ},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "68000254166",
					Fields: map[string]any{
						"name":       "Account Administrator",
						"agent_type": int64(1),
					},
				},
				{
					ID: "68000254169",
					Fields: map[string]any{
						"name":       "Agent",
						"agent_type": int64(1),
					},
				},
				{
					ID: "68000254170",
					Fields: map[string]any{
						"name":       "Ticket Collaborator",
						"agent_type": int64(3),
					},
				},
			},
		},
		{
			SourceTable:      "groups",
			DestTable:        "main.freshdesk_groups",
			KeyColumn:        "id",
			ExpectedRowCount: 2,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "name", DataType: schema.TypeString},
				{Name: "description", DataType: schema.TypeString},
				{Name: "group_type", DataType: schema.TypeString},
				{Name: "created_at", DataType: schema.TypeTimestampTZ},
				{Name: "updated_at", DataType: schema.TypeTimestampTZ},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "68000005672",
					Fields: map[string]any{
						"name":        "Engineering Support",
						"description": "Handles technical and engineering issues",
						"group_type":  "support_agent_group",
					},
				},
				{
					ID: "68000005673",
					Fields: map[string]any{
						"name":        "Billing Support",
						"description": "Handles billing and payment inquiries",
						"group_type":  "support_agent_group",
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

func TestFreshdeskTicketsSearch(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	subdomain := os.Getenv("FRESHDESK_SUBDOMAIN")
	apiKey := os.Getenv("FRESHDESK_API_KEY")
	if subdomain == "" || apiKey == "" {
		t.Skip("Set FRESHDESK_SUBDOMAIN and FRESHDESK_API_KEY to run Freshdesk integration tests")
	}

	ctx := context.Background()
	sourceURI := fmt.Sprintf("freshdesk://%s?api_key=%s", subdomain, apiKey)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("freshdesk_search_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	exp := testutil.TableExpectation{
		SourceTable:         "tickets:priority:>3",
		DestTable:           "main.\"freshdesk_tickets:priority:>3\"",
		KeyColumn:           "id",
		MinExpectedRowCount: 1,
		ExpectedSchema: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "subject", DataType: schema.TypeString},
			{Name: "priority", DataType: schema.TypeInt64},
		},
	}

	t.Run("tickets_search", func(t *testing.T) {
		testutil.RunPipeline(t, ctx, sourceURI, destURI, exp)
		testutil.Check(t, destURI, exp)
	})
}
