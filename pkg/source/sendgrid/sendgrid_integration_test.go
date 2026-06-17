//go:build integration

package sendgrid_test

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

// Test data was seeded in the vendor_accounts@getbruin.com SendGrid trial account:
//   - lists: two marketing lists ("Ingestr Test List", "Newsletter Subscribers")
//   - single_sends: one draft single send ("Ingestr Test Single Send")
//   - global_stats: always returns one row per day in the requested range (zero metrics if no sends)
//   - bounces: two hard bounces from sends to non-existent gmail.com mailboxes
//   - blocks: two blocks from sends to a domain with no resolvable MX
//   - unsubscribes: two global unsubscribes seeded via the ASM API
//   - messages: Email Activity for the account's test sends. This data is time-limited (SendGrid
//     retains Email Activity for a bounded window) and reflects every send, so it uses
//     MinExpectedRowCount and a schema-only assertion rather than an exact count.
func TestSendGridPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	key := os.Getenv("SENDGRID_API_KEY")
	if key == "" {
		t.Skip("Set SENDGRID_API_KEY to run SendGrid integration tests")
	}

	ctx := context.Background()
	sourceURI := fmt.Sprintf("sendgrid://?api_key=%s", key)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("sendgrid_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	// Intervals are half-open [start, end): the end day is excluded, so Jun 1..Jun 9 yields Jun 1..Jun 8.
	statsStart := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	statsEnd := time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC)

	expectations := []testutil.TableExpectation{
		{
			SourceTable: "lists",
			DestTable:   "main.sendgrid_lists",
			KeyColumn:   "id",
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "contact_count", DataType: schema.TypeInt64},
			},
			ExpectedRowCount: 2,
			Rows: []testutil.ExpectedRow{
				{
					ID: "d4dc71e5-f3c9-42a1-b7f6-0424a4c45b54",
					Fields: map[string]any{
						"name":          "Ingestr Test List",
						"contact_count": int64(0),
					},
				},
			},
		},
		{
			SourceTable: "single_sends",
			DestTable:   "main.sendgrid_single_sends",
			KeyColumn:   "id",
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "status", DataType: schema.TypeString},
				{Name: "is_abtest", DataType: schema.TypeBoolean},
				{Name: "created_at", DataType: schema.TypeTimestampTZ},
				{Name: "updated_at", DataType: schema.TypeTimestampTZ},
			},
			ExpectedRowCount: 1,
			Rows: []testutil.ExpectedRow{
				{
					ID: "ea2ac1f4-6405-11f1-9c04-a27bde8ca616",
					Fields: map[string]any{
						"name":       "Ingestr Test Single Send",
						"status":     "draft",
						"is_abtest":  false,
						"created_at": time.Date(2026, 6, 9, 13, 19, 59, 0, time.UTC),
						"updated_at": time.Date(2026, 6, 9, 13, 19, 59, 0, time.UTC),
					},
				},
			},
		},
		{
			SourceTable:   "global_stats",
			DestTable:     "main.sendgrid_global_stats",
			KeyColumn:     "date",
			IntervalStart: &statsStart,
			IntervalEnd:   &statsEnd,
			ExpectedSchema: []schema.Column{
				{Name: "date", DataType: schema.TypeDate},
			},
			ExpectedRowCount: 8,
		},
		{
			SourceTable: "bounces",
			DestTable:   "main.sendgrid_bounces",
			KeyColumn:   "email",
			ExpectedSchema: []schema.Column{
				{Name: "email", DataType: schema.TypeString},
				{Name: "status", DataType: schema.TypeString},
				{Name: "reason", DataType: schema.TypeString},
				{Name: "created", DataType: schema.TypeInt64},
			},
			ExpectedRowCount: 2,
			Rows: []testutil.ExpectedRow{
				{
					ID: "no-such-account-55texist012@gmail.com",
					Fields: map[string]any{
						"status":  "5.1.1",
						"created": int64(1781013676),
					},
				},
			},
		},
		{
			SourceTable: "blocks",
			DestTable:   "main.sendgrid_blocks",
			KeyColumn:   "email",
			ExpectedSchema: []schema.Column{
				{Name: "email", DataType: schema.TypeString},
				{Name: "reason", DataType: schema.TypeString},
				{Name: "created", DataType: schema.TypeInt64},
			},
			ExpectedRowCount: 2,
			Rows: []testutil.ExpectedRow{
				{
					ID: "nobody@nonexistent-bruin-test-xyz123.com",
					Fields: map[string]any{
						"reason":  "unable to get mx info: failed to get IPs from PTR record: lookup <nil>: unrecognized address",
						"created": int64(1781011819),
					},
				},
			},
		},
		{
			SourceTable: "unsubscribes",
			DestTable:   "main.sendgrid_unsubscribes",
			KeyColumn:   "email",
			ExpectedSchema: []schema.Column{
				{Name: "email", DataType: schema.TypeString},
				{Name: "created", DataType: schema.TypeInt64},
			},
			ExpectedRowCount: 2,
			Rows: []testutil.ExpectedRow{
				{
					ID: "unsub-test-1@example.com",
					Fields: map[string]any{
						"created": int64(1781695248),
					},
				},
			},
		},
		// invalid_emails and spam_reports are also supported and use the same code path, but they
		// are system/recipient-generated (no API to seed them), so they're empty in the test
		// account and not asserted here.
		{
			SourceTable: "messages",
			DestTable:   "main.sendgrid_messages",
			KeyColumn:   "msg_id",
			ExpectedSchema: []schema.Column{
				{Name: "msg_id", DataType: schema.TypeString},
				{Name: "from_email", DataType: schema.TypeString},
				{Name: "to_email", DataType: schema.TypeString},
				{Name: "subject", DataType: schema.TypeString},
				{Name: "status", DataType: schema.TypeString},
				{Name: "opens_count", DataType: schema.TypeInt64},
				{Name: "clicks_count", DataType: schema.TypeInt64},
				{Name: "last_event_time", DataType: schema.TypeTimestampTZ},
			},
			// Email Activity data is time-limited and reflects all account sends, so assert a floor.
			MinExpectedRowCount: 1,
		},
	}

	for _, exp := range expectations {
		t.Run(exp.SourceTable, func(t *testing.T) {
			testutil.RunPipeline(t, ctx, sourceURI, destURI, exp)
			testutil.Check(t, destURI, exp)
		})
	}
}
