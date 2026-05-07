package zoom_test

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

func TestZoomPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	clientID := os.Getenv("ZOOM_CLIENT_ID")
	clientSecret := os.Getenv("ZOOM_CLIENT_SECRET")
	accountID := os.Getenv("ZOOM_ACCOUNT_ID")

	if clientID == "" || clientSecret == "" || accountID == "" {
		t.Skip("Set ZOOM_CLIENT_ID, ZOOM_CLIENT_SECRET, and ZOOM_ACCOUNT_ID to run Zoom integration tests")
	}

	ctx := context.Background()
	sourceURI := fmt.Sprintf("zoom://?client_id=%s&client_secret=%s&account_id=%s", clientID, clientSecret, accountID)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("zoom_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	expectations := []testutil.TableExpectation{
		{
			SourceTable: "users",
			DestTable:   "main.zoom_users",
			KeyColumn:   "id",
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "first_name", DataType: schema.TypeString},
				{Name: "last_name", DataType: schema.TypeString},
				{Name: "display_name", DataType: schema.TypeString},
				{Name: "email", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeInt64},
				{Name: "status", DataType: schema.TypeString},
				{Name: "pmi", DataType: schema.TypeInt64},
				{Name: "timezone", DataType: schema.TypeString},
				{Name: "verified", DataType: schema.TypeInt64},
				{Name: "created_at", DataType: schema.TypeTimestampTZ},
				{Name: "language", DataType: schema.TypeString},
				{Name: "role_id", DataType: schema.TypeString},
				{Name: "user_created_at", DataType: schema.TypeTimestampTZ},
			},
			ExpectedRowCount: 1,
			Rows: []testutil.ExpectedRow{
				{
					ID: "RMNs5Y3UTpWJU6OnkVSHNg",
					Fields: map[string]any{
						"first_name":   "Gong",
						"last_name":    "Test",
						"display_name": "Gong Test",
						"email":        "vendor_accounts@getbruin.com",
						"type":         int64(1),
						"status":       "active",
						"pmi":          int64(9485207754),
						"timezone":     "Europe/Istanbul",
						"verified":     int64(1),
						"created_at":   time.Date(2026, 2, 16, 5, 14, 47, 0, time.UTC),
						"language":     "en-US",
						"role_id":      "0",
					},
				},
			},
		},
		// meetings test removed: the Zoom test account has 0 scheduled meetings
		// (past meetings were auto-purged by Zoom). Re-add when new test meetings are created.
	}

	for _, exp := range expectations {
		t.Run(exp.SourceTable, func(t *testing.T) {
			testutil.RunPipeline(t, ctx, sourceURI, destURI, exp)
			testutil.Check(t, destURI, exp)
		})
	}
}
