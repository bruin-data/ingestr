package clickup_test

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

func TestClickUpPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	apiKey := os.Getenv("CLICKUP_API_KEY")
	if apiKey == "" {
		t.Skip("Set CLICKUP_API_KEY to run ClickUp integration tests")
	}

	ctx := context.Background()
	sourceURI := fmt.Sprintf("clickup://?api_key=%s", apiKey)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("clickup_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	expectations := []testutil.TableExpectation{
		{
			SourceTable:      "user",
			DestTable:        "main.clickup_user",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "username", DataType: schema.TypeString},
				{Name: "email", DataType: schema.TypeString},
				{Name: "color", DataType: schema.TypeString},
				{Name: "initials", DataType: schema.TypeString},
				{Name: "timezone", DataType: schema.TypeString},
				{Name: "global_font_support", DataType: schema.TypeBoolean},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "284461060",
					Fields: map[string]any{
						"username":            "Gong Test",
						"email":               "vendor_accounts@getbruin.com",
						"color":               "#595d66",
						"initials":            "GT",
						"timezone":            "Europe/Istanbul",
						"global_font_support": true,
					},
				},
			},
		},
		{
			SourceTable:      "teams",
			DestTable:        "main.clickup_teams",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "color", DataType: schema.TypeString},
				{Name: "members", DataType: schema.TypeJSON},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "90182472089",
					Fields: map[string]any{
						"name":  "Getbruin",
						"color": "#40BC86",
					},
				},
			},
		},
		{
			SourceTable:      "spaces",
			DestTable:        "main.clickup_spaces",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "color", DataType: schema.TypeString},
				{Name: "private", DataType: schema.TypeBoolean},
				{Name: "admin_can_manage", DataType: schema.TypeBoolean},
				{Name: "multiple_assignees", DataType: schema.TypeBoolean},
				{Name: "archived", DataType: schema.TypeBoolean},
				{Name: "statuses", DataType: schema.TypeJSON},
				{Name: "features", DataType: schema.TypeJSON},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "90189931029",
					Fields: map[string]any{
						"name":               "Team Space",
						"color":              "#03A2FD",
						"private":            false,
						"admin_can_manage":   true,
						"multiple_assignees": true,
						"archived":           false,
					},
				},
			},
		},
		{
			SourceTable:         "lists",
			DestTable:           "main.clickup_lists",
			KeyColumn:           "id",
			MinExpectedRowCount: 4,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "content", DataType: schema.TypeString},
				{Name: "orderindex", DataType: schema.TypeInt64},
				{Name: "archived", DataType: schema.TypeBoolean},
				{Name: "override_statuses", DataType: schema.TypeBoolean},
				{Name: "permission_level", DataType: schema.TypeString},
				{Name: "folder", DataType: schema.TypeJSON},
				{Name: "space", DataType: schema.TypeJSON},
				{Name: "task_count", DataType: schema.TypeInt64},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "901816172769",
					Fields: map[string]any{
						"name":       "Project 2",
						"content":    nil,
						"archived":   false,
						"task_count": int64(3),
					},
				},
				{
					ID: "901816172770",
					Fields: map[string]any{
						"name":       "Project 1",
						"content":    nil,
						"archived":   false,
						"task_count": int64(3),
					},
				},
				{
					ID: "901816400579",
					Fields: map[string]any{
						"name":       "Folder List 1",
						"content":    "",
						"archived":   false,
						"task_count": int64(2),
					},
				},
			},
		},
		{
			SourceTable:         "tasks",
			DestTable:           "main.clickup_tasks",
			KeyColumn:           "id",
			MinExpectedRowCount: 13,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "text_content", DataType: schema.TypeString},
				{Name: "description", DataType: schema.TypeString},
				{Name: "status", DataType: schema.TypeJSON},
				{Name: "date_created", DataType: schema.TypeString},
				{Name: "date_updated", DataType: schema.TypeString},
				{Name: "archived", DataType: schema.TypeBoolean},
				{Name: "creator", DataType: schema.TypeJSON},
				{Name: "assignees", DataType: schema.TypeJSON},
				{Name: "tags", DataType: schema.TypeJSON},
				{Name: "url", DataType: schema.TypeString},
				{Name: "team_id", DataType: schema.TypeString},
				{Name: "list", DataType: schema.TypeJSON},
				{Name: "folder", DataType: schema.TypeJSON},
				{Name: "space", DataType: schema.TypeJSON},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "86ewny5pr",
					Fields: map[string]any{
						"name":         "Task 1",
						"text_content": "",
						"description":  "",
						"archived":     false,
						"team_id":      "90182472089",
						"url":          "https://app.clickup.com/t/86ewny5pr",
					},
				},
				{
					ID: "86ewny5pt",
					Fields: map[string]any{
						"name":     "Task 2",
						"archived": false,
						"team_id":  "90182472089",
					},
				},
				{
					ID: "86ewt2yvz",
					Fields: map[string]any{
						"name":        "Folder Task 1",
						"description": "Task inside a folder for integration testing",
						"archived":    false,
						"team_id":     "90182472089",
						"url":         "https://app.clickup.com/t/86ewt2yvz",
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
