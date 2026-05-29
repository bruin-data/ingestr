//go:build integration

package asana_test

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

func TestAsanaPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	token := os.Getenv("ASANA_ACCESS_TOKEN")
	workspaceID := os.Getenv("ASANA_WORKSPACE_ID")
	if token == "" || workspaceID == "" {
		t.Skip("Set ASANA_ACCESS_TOKEN and ASANA_WORKSPACE_ID to run Asana integration tests")
	}

	ctx := context.Background()
	sourceURI := fmt.Sprintf("asana://%s?access_token=%s", workspaceID, token)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("asana_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	expectations := []testutil.TableExpectation{
		{
			SourceTable:      "workspaces",
			DestTable:        "main.asana_workspaces",
			KeyColumn:        "gid",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "gid", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "is_organization", DataType: schema.TypeBoolean},
				{Name: "resource_type", DataType: schema.TypeString},
				{Name: "email_domains", DataType: schema.TypeJSON},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "1213722707327570",
					Fields: map[string]any{
						"name":            "getbruin.com",
						"is_organization": true,
						"resource_type":   "workspace",
					},
				},
			},
		},
		{
			SourceTable:      "projects",
			DestTable:        "main.asana_projects",
			KeyColumn:        "gid",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "gid", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "archived", DataType: schema.TypeBoolean},
				{Name: "completed", DataType: schema.TypeBoolean},
				{Name: "resource_type", DataType: schema.TypeString},
				{Name: "created_at", DataType: schema.TypeTimestampTZ},
				{Name: "modified_at", DataType: schema.TypeTimestampTZ},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "1213714106697741",
					Fields: map[string]any{
						"name":          "Test Project",
						"resource_type": "project",
						"archived":      false,
						"completed":     false,
						"color":         "aqua",
						"default_view":  "list",
						"public":        false,
					},
				},
			},
		},
		{
			SourceTable:      "sections",
			DestTable:        "main.asana_sections",
			KeyColumn:        "gid",
			ExpectedRowCount: 3,
			ExpectedSchema: []schema.Column{
				{Name: "gid", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "resource_type", DataType: schema.TypeString},
				{Name: "created_at", DataType: schema.TypeTimestampTZ},
				{Name: "project", DataType: schema.TypeJSON},
			},
			Rows: []testutil.ExpectedRow{
				{ID: "1213714106697742", Fields: map[string]any{"name": "To do", "resource_type": "section"}},
				{ID: "1213714106697744", Fields: map[string]any{"name": "Doing", "resource_type": "section"}},
				{ID: "1213714106697745", Fields: map[string]any{"name": "Done", "resource_type": "section"}},
			},
		},
		{
			SourceTable:      "tags",
			DestTable:        "main.asana_tags",
			KeyColumn:        "gid",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "gid", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "resource_type", DataType: schema.TypeString},
				{Name: "color", DataType: schema.TypeString},
				{Name: "created_at", DataType: schema.TypeTimestampTZ},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "1213714124936791",
					Fields: map[string]any{
						"name":          "test-tag",
						"resource_type": "tag",
						"color":         "none",
					},
				},
			},
		},
		{
			SourceTable:      "tasks",
			DestTable:        "main.asana_tasks",
			KeyColumn:        "gid",
			ExpectedRowCount: 3,
			ExpectedSchema: []schema.Column{
				{Name: "gid", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "completed", DataType: schema.TypeBoolean},
				{Name: "resource_type", DataType: schema.TypeString},
				{Name: "resource_subtype", DataType: schema.TypeString},
				{Name: "created_at", DataType: schema.TypeTimestampTZ},
				{Name: "modified_at", DataType: schema.TypeTimestampTZ},
				{Name: "num_subtasks", DataType: schema.TypeInt64},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "1213714106697756",
					Fields: map[string]any{
						"name":             "Draft project brief",
						"completed":        false,
						"resource_type":    "task",
						"resource_subtype": "default_task",
					},
				},
				{
					ID: "1213735077817485",
					Fields: map[string]any{
						"name":             "Schedule kickoff meeting",
						"completed":        false,
						"resource_type":    "task",
						"resource_subtype": "default_task",
					},
				},
				{
					ID: "1213735077817487",
					Fields: map[string]any{
						"name":             "Share timeline with teammates",
						"completed":        false,
						"resource_type":    "task",
						"resource_subtype": "default_task",
					},
				},
			},
		},
		{
			SourceTable:         "stories",
			DestTable:           "main.asana_stories",
			KeyColumn:           "gid",
			MinExpectedRowCount: 8,
			ExpectedSchema: []schema.Column{
				{Name: "gid", DataType: schema.TypeString},
				{Name: "resource_type", DataType: schema.TypeString},
				{Name: "resource_subtype", DataType: schema.TypeString},
				{Name: "text", DataType: schema.TypeString},
				{Name: "created_at", DataType: schema.TypeTimestampTZ},
				{Name: "created_by", DataType: schema.TypeJSON},
			},
		},
		{
			SourceTable:      "teams",
			DestTable:        "main.asana_teams",
			KeyColumn:        "gid",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "gid", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "resource_type", DataType: schema.TypeString},
				{Name: "visibility", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "1213722707327572",
					Fields: map[string]any{
						"name":          "Vendor's first team",
						"resource_type": "team",
						"visibility":    "request_to_join",
					},
				},
			},
		},
		{
			SourceTable:      "users",
			DestTable:        "main.asana_users",
			KeyColumn:        "gid",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "gid", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "email", DataType: schema.TypeString},
				{Name: "resource_type", DataType: schema.TypeString},
				{Name: "workspaces", DataType: schema.TypeJSON},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "1213710835358215",
					Fields: map[string]any{
						"name":          "Vendor Accounts",
						"email":         "vendor_accounts@getbruin.com",
						"resource_type": "user",
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

// TestAsanaTasksMerge verifies that running the tasks pipeline twice does not produce duplicate rows.
func TestAsanaTasksMerge(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	token := os.Getenv("ASANA_ACCESS_TOKEN")
	workspaceID := os.Getenv("ASANA_WORKSPACE_ID")
	if token == "" || workspaceID == "" {
		t.Skip("Set ASANA_ACCESS_TOKEN and ASANA_WORKSPACE_ID to run Asana integration tests")
	}

	ctx := context.Background()
	sourceURI := fmt.Sprintf("asana://%s?access_token=%s", workspaceID, token)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("asana_merge_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	exp := testutil.TableExpectation{
		SourceTable:      "tasks",
		DestTable:        "main.asana_tasks",
		KeyColumn:        "gid",
		ExpectedRowCount: 3,
	}

	testutil.RunPipeline(t, ctx, sourceURI, destURI, exp)
	testutil.Check(t, destURI, exp)

	// Second run: should merge without duplicates
	testutil.RunPipeline(t, ctx, sourceURI, destURI, exp)
	testutil.Check(t, destURI, exp)
}
