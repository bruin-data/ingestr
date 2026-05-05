package notion_test

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

func TestNotionPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	key := os.Getenv("NOTION_API_KEY")
	dbID := os.Getenv("NOTION_DATABASE_ID")
	if key == "" || dbID == "" {
		t.Skip("Set NOTION_API_KEY and NOTION_DATABASE_ID to run Notion integration tests")
	}

	ctx := context.Background()
	sourceURI := fmt.Sprintf("notion://?api_key=%s", key)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("notion_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	expectations := []testutil.TableExpectation{
		{
			SourceTable: dbID,
			DestTable:   "main.notion_tasks",
			KeyColumn:   "id",
			ExpectedSchema: []schema.Column{
				{Name: "archived", DataType: schema.TypeBoolean},
				{Name: "created_time", DataType: schema.TypeTimestampTZ},
				{Name: "id", DataType: schema.TypeString},
				{Name: "in_trash", DataType: schema.TypeBoolean},
				{Name: "is_archived", DataType: schema.TypeBoolean},
				{Name: "is_locked", DataType: schema.TypeBoolean},
				{Name: "last_edited_time", DataType: schema.TypeTimestampTZ},
				{Name: "object", DataType: schema.TypeString},
				{Name: "url", DataType: schema.TypeString},
			},
			MinExpectedRowCount: 3,
			Rows: []testutil.ExpectedRow{
				{
					ID: "32c79ff9-0403-81cc-b423-c14505afffb1",
					Fields: map[string]any{
						"object":      "page",
						"archived":    false,
						"in_trash":    false,
						"is_archived": false,
						"is_locked":   false,
						"url":         "https://www.notion.so/Fix-login-bug-32c79ff9040381ccb423c14505afffb1",
					},
				},
			},
		},
	}

	for _, exp := range expectations {
		t.Run("database", func(t *testing.T) {
			testutil.RunPipeline(t, ctx, sourceURI, destURI, exp)
			testutil.Check(t, destURI, exp)
		})
	}
}
