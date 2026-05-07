package fireflies_test

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

func TestFirefliesPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	key := os.Getenv("FIREFLIES_API_KEY")
	if key == "" {
		t.Skip("Set FIREFLIES_API_KEY to run Fireflies integration tests")
	}

	ctx := context.Background()
	sourceURI := fmt.Sprintf("fireflies://?api_key=%s", key)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("fireflies_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	expectations := []testutil.TableExpectation{
		{
			SourceTable: "users",
			DestTable:   "main.fireflies_users",
			KeyColumn:   "user_id",
			ExpectedSchema: []schema.Column{
				{Name: "user_id", DataType: schema.TypeString},
				{Name: "email", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "num_transcripts", DataType: schema.TypeInt64},
				{Name: "recent_transcript", DataType: schema.TypeString},
				{Name: "recent_meeting", DataType: schema.TypeString},
				{Name: "minutes_consumed", DataType: schema.TypeInt64},
				{Name: "is_admin", DataType: schema.TypeBoolean},
				{Name: "integrations", DataType: schema.TypeJSON},
				{Name: "user_groups", DataType: schema.TypeJSON},
			},
			ExpectedRowCount: 1,
			Rows: []testutil.ExpectedRow{
				{
					ID: "01KH32WZ067X4YH3C93THGPW6B",
					Fields: map[string]any{
						"email":             "bruintestgong@gmail.com",
						"name":              "Test Gong",
						"num_transcripts":   int64(1),
						"recent_transcript": "01KH33RTM4E623S9X5EXCKHWNK",
						"recent_meeting":    "01KH33RTM4E623S9X5EXCKHWNK",
						"minutes_consumed":  int64(3),
						"is_admin":          true,
						"integrations":      `["zoom"]`,
						"user_groups":       `[]`,
					},
				},
			},
		},
		{
			SourceTable: "transcripts",
			DestTable:   "main.fireflies_transcripts",
			KeyColumn:   "id",
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "title", DataType: schema.TypeString},
				{Name: "date", DataType: schema.TypeTimestampTZ},
				{Name: "duration", DataType: schema.TypeFloat64},
				{Name: "transcript_url", DataType: schema.TypeString},
				{Name: "meeting_link", DataType: schema.TypeString},
				{Name: "organizer_email", DataType: schema.TypeString},
				{Name: "participants", DataType: schema.TypeJSON},
				{Name: "fireflies_users", DataType: schema.TypeJSON},
				{Name: "channels", DataType: schema.TypeJSON},
				{Name: "speakers", DataType: schema.TypeJSON},
				{Name: "analytics", DataType: schema.TypeJSON},
				{Name: "sentences", DataType: schema.TypeJSON},
				{Name: "meeting_info", DataType: schema.TypeJSON},
				{Name: "meeting_attendees", DataType: schema.TypeJSON},
				{Name: "meeting_attendance", DataType: schema.TypeJSON},
				{Name: "summary", DataType: schema.TypeJSON},
				{Name: "user", DataType: schema.TypeJSON},
				{Name: "apps_preview", DataType: schema.TypeJSON},
				{Name: "_errors", DataType: schema.TypeJSON},
			},
			ExpectedRowCount: 1,
			Rows: []testutil.ExpectedRow{
				{
					ID: "01KH33RTM4E623S9X5EXCKHWNK",
					Fields: map[string]any{
						"title":           "product team",
						"date":            time.Date(2026, 2, 10, 6, 30, 0, 0, time.UTC),
						"duration":        float64(3.930000066757202),
						"transcript_url":  "https://app.fireflies.ai/view/01KH33RTM4E623S9X5EXCKHWNK",
						"meeting_link":    "https://us05web.zoom.us/j/83995849432?pwd=0n2939edjauhE7OHDDETaEg2mNFcbT.1",
						"organizer_email": "bruintestgong@gmail.com",
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
