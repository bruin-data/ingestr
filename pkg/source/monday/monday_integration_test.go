//go:build integration

package monday_test

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

func TestMondayPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	key := os.Getenv("MONDAY_API_KEY")
	if key == "" {
		t.Skip("Set MONDAY_API_KEY to run Monday integration tests")
	}

	ctx := context.Background()
	sourceURI := fmt.Sprintf("monday://?api_key=%s", key)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("monday_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	expectations := []testutil.TableExpectation{
		{
			SourceTable: "account",
			DestTable:   "main.monday_account",
			KeyColumn:   "id",
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "slug", DataType: schema.TypeString},
				{Name: "country_code", DataType: schema.TypeString},
				{Name: "first_day_of_the_week", DataType: schema.TypeString},
				{Name: "show_timeline_weekends", DataType: schema.TypeBoolean},
				{Name: "active_members_count", DataType: schema.TypeInt64},
			},
			ExpectedRowCount: 1,
			Rows: []testutil.ExpectedRow{
				{
					ID: "33820899",
					Fields: map[string]any{
						"name":                   "GongTest",
						"slug":                   "gongtest",
						"country_code":           "TR",
						"first_day_of_the_week":  "monday",
						"show_timeline_weekends": true,
						"active_members_count":   int64(1),
					},
				},
			},
		},
		{
			SourceTable: "account_roles",
			DestTable:   "main.monday_account_roles",
			KeyColumn:   "id",
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "role_type", DataType: schema.TypeString},
			},
			ExpectedRowCount: 4,
			Rows: []testutil.ExpectedRow{
				{
					ID: "1",
					Fields: map[string]any{
						"name":      "admin",
						"role_type": "basic_role",
					},
				},
				{
					ID: "2",
					Fields: map[string]any{
						"name":      "member",
						"role_type": "basic_role",
					},
				},
				{
					ID: "3",
					Fields: map[string]any{
						"name":      "view_only",
						"role_type": "basic_role",
					},
				},
				{
					ID: "4",
					Fields: map[string]any{
						"name":      "guest",
						"role_type": "basic_role",
					},
				},
			},
		},
		{
			SourceTable: "board_columns",
			DestTable:   "main.monday_board_columns",
			KeyColumn:   "board_id || '~' || id",
			ExpectedSchema: []schema.Column{
				{Name: "board_id", DataType: schema.TypeString},
				{Name: "id", DataType: schema.TypeString},
				{Name: "title", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
				{Name: "archived", DataType: schema.TypeBoolean},
				{Name: "description", DataType: schema.TypeString},
				{Name: "settings_str", DataType: schema.TypeString},
				{Name: "width", DataType: schema.TypeInt64},
			},
			ExpectedRowCount: 14,
			Rows: []testutil.ExpectedRow{
				{
					ID: "5091841857~color_mm0mq4a4",
					Fields: map[string]any{
						"board_id":    "5091841857",
						"title":       "Priority",
						"type":        "status",
						"archived":    false,
						"description": nil,
						"width":       nil,
					},
				},
				{
					ID: "5091841857~date_mm0mxg6w",
					Fields: map[string]any{
						"board_id":    "5091841857",
						"title":       "Due Date",
						"type":        "date",
						"archived":    false,
						"description": nil,
						"width":       nil,
					},
				},
				{
					ID: "5091841857~multiple_person_mm0mbzxj",
					Fields: map[string]any{
						"board_id":    "5091841857",
						"title":       "Assignee",
						"type":        "people",
						"archived":    false,
						"description": nil,
						"width":       nil,
					},
				},
				{
					ID: "5091841912~color_mm0m7kkp",
					Fields: map[string]any{
						"board_id":    "5091841912",
						"title":       "Severity",
						"type":        "status",
						"archived":    false,
						"description": nil,
						"width":       nil,
					},
				},
				{
					ID: "5091841912~date_mm0mk2f0",
					Fields: map[string]any{
						"board_id":    "5091841912",
						"title":       "Reported Date",
						"type":        "date",
						"archived":    false,
						"description": nil,
						"width":       nil,
					},
				},
				{
					ID: "5091841883~color_mm0m4wkj",
					Fields: map[string]any{
						"board_id":    "5091841883",
						"title":       "Campaign Status",
						"type":        "status",
						"archived":    false,
						"description": nil,
						"width":       nil,
					},
				},
				{
					ID: "5091839751~project_owner",
					Fields: map[string]any{
						"board_id":    "5091839751",
						"title":       "Owner",
						"type":        "people",
						"archived":    false,
						"description": "",
						"width":       nil,
					},
				},
				{
					ID: "5091839751~project_status",
					Fields: map[string]any{
						"board_id":    "5091839751",
						"title":       "Status",
						"type":        "status",
						"archived":    false,
						"description": nil,
						"width":       int64(134),
					},
				},
				{
					ID: "5091839751~project_timeline",
					Fields: map[string]any{
						"board_id":    "5091839751",
						"title":       "Timeline",
						"type":        "timeline",
						"archived":    false,
						"description": "",
						"width":       nil,
					},
				},
			},
		},
		{
			SourceTable: "boards",
			DestTable:   "main.monday_boards",
			KeyColumn:   "id",
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "description", DataType: schema.TypeString},
				{Name: "state", DataType: schema.TypeString},
				{Name: "board_kind", DataType: schema.TypeString},
				{Name: "workspace_id", DataType: schema.TypeString},
				{Name: "permissions", DataType: schema.TypeString},
				{Name: "item_terminology", DataType: schema.TypeString},
				{Name: "items_count", DataType: schema.TypeInt64},
				{Name: "updated_at", DataType: schema.TypeTimestampTZ},
				{Name: "url", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
				{Name: "creator_id", DataType: schema.TypeString},
				{Name: "owners", DataType: schema.TypeJSON},
				{Name: "subscribers", DataType: schema.TypeJSON},
				{Name: "team_owners", DataType: schema.TypeString},
				{Name: "team_subscribers", DataType: schema.TypeString},
				{Name: "tags", DataType: schema.TypeString},
			},
			ExpectedRowCount: 4,
			Rows: []testutil.ExpectedRow{
				{
					ID: "5091841912",
					Fields: map[string]any{
						"name":             "Bug Tracker",
						"description":      nil,
						"state":            "active",
						"board_kind":       "public",
						"workspace_id":     "5816004",
						"permissions":      "everyone",
						"item_terminology": "item",
						"items_count":      int64(2),
						"url":              "https://gongtest.monday.com/boards/5091841912",
						"type":             "board",
						"creator_id":       "99910119",
					},
				},
				{
					ID: "5091841883",
					Fields: map[string]any{
						"name":             "Marketing Campaigns",
						"description":      nil,
						"state":            "active",
						"board_kind":       "public",
						"workspace_id":     "5816004",
						"permissions":      "everyone",
						"item_terminology": "item",
						"items_count":      int64(1),
						"url":              "https://gongtest.monday.com/boards/5091841883",
						"type":             "board",
						"creator_id":       "99910119",
					},
				},
				{
					ID: "5091841857",
					Fields: map[string]any{
						"name":             "Engineering Tasks",
						"description":      nil,
						"state":            "active",
						"board_kind":       "public",
						"workspace_id":     "5816004",
						"permissions":      "everyone",
						"item_terminology": "item",
						"items_count":      int64(3),
						"url":              "https://gongtest.monday.com/boards/5091841857",
						"type":             "board",
						"creator_id":       "99910119",
					},
				},
				{
					ID: "5091839751",
					Fields: map[string]any{
						"name":             "initial project",
						"description":      "Manage any type of project. Assign owners, set timelines and keep track of where your project stands.",
						"state":            "active",
						"board_kind":       "public",
						"workspace_id":     "5816004",
						"permissions":      "everyone",
						"item_terminology": "task",
						"items_count":      int64(3),
						"url":              "https://gongtest.monday.com/boards/5091839751",
						"type":             "board",
						"creator_id":       "99910119",
					},
				},
			},
		},
		{
			SourceTable: "tags",
			DestTable:   "main.monday_tags",
			KeyColumn:   "id",
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "color", DataType: schema.TypeString},
			},
			ExpectedRowCount: 3,
			Rows: []testutil.ExpectedRow{
				{
					ID: "5920741",
					Fields: map[string]any{
						"name":  "urgent",
						"color": "#e2445c",
					},
				},
				{
					ID: "5920742",
					Fields: map[string]any{
						"name":  "backend",
						"color": "#a25ddc",
					},
				},
				{
					ID: "5920743",
					Fields: map[string]any{
						"name":  "frontend",
						"color": "#579bfc",
					},
				},
			},
		},
		{
			SourceTable: "updates",
			DestTable:   "main.monday_updates",
			KeyColumn:   "id",
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "body", DataType: schema.TypeString},
				{Name: "text_body", DataType: schema.TypeString},
				{Name: "created_at", DataType: schema.TypeTimestampTZ},
				{Name: "updated_at", DataType: schema.TypeTimestampTZ},
				{Name: "edited_at", DataType: schema.TypeTimestampTZ},
				{Name: "creator_id", DataType: schema.TypeString},
				{Name: "creator_name", DataType: schema.TypeString},
				{Name: "item_id", DataType: schema.TypeString},
				{Name: "item_name", DataType: schema.TypeString},
				{Name: "assets", DataType: schema.TypeString},
				{Name: "replies", DataType: schema.TypeString},
				{Name: "likes", DataType: schema.TypeString},
				{Name: "pinned_to_top", DataType: schema.TypeString},
				{Name: "viewers", DataType: schema.TypeString},
			},
			ExpectedRowCount: 6,
			Rows: []testutil.ExpectedRow{
				{
					ID: "523528666",
					Fields: map[string]any{
						"body":       "Email campaign draft ready for review.",
						"text_body":  "Email campaign draft ready for review.",
						"creator_id": "99910119",
						"item_id":    "2723608951",
					},
				},
				{
					ID: "523528562",
					Fields: map[string]any{
						"body":       "Memory leak traced to unclosed DB connections.",
						"text_body":  "Memory leak traced to unclosed DB connections.",
						"creator_id": "99910119",
						"item_id":    "2723664927",
					},
				},
				{
					ID: "523528504",
					Fields: map[string]any{
						"body":       "Investigating the bug in production logs.",
						"text_body":  "Investigating the bug in production logs.",
						"creator_id": "99910119",
						"item_id":    "2723614117",
					},
				},
				{
					ID: "523528386",
					Fields: map[string]any{
						"body":       "Added unit tests for the auth endpoints.",
						"text_body":  "Added unit tests for the auth endpoints.",
						"creator_id": "99910119",
						"item_id":    "2723664908",
					},
				},
				{
					ID: "523528314",
					Fields: map[string]any{
						"body":       "CI/CD pipeline configured with GitHub Actions.",
						"text_body":  "CI/CD pipeline configured with GitHub Actions.",
						"creator_id": "99910119",
						"item_id":    "2723664604",
					},
				},
				{
					ID: "523528203",
					Fields: map[string]any{
						"body":       "Started working on initial task setup.",
						"text_body":  "Started working on initial task setup.",
						"creator_id": "99910119",
						"item_id":    "2723610670",
					},
				},
			},
		},
		{
			SourceTable: "users",
			DestTable:   "main.monday_users",
			KeyColumn:   "id",
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "email", DataType: schema.TypeString},
				{Name: "enabled", DataType: schema.TypeBoolean},
				{Name: "is_admin", DataType: schema.TypeBoolean},
				{Name: "is_guest", DataType: schema.TypeBoolean},
				{Name: "is_pending", DataType: schema.TypeBoolean},
				{Name: "is_view_only", DataType: schema.TypeBoolean},
				{Name: "created_at", DataType: schema.TypeTimestampTZ},
				{Name: "country_code", DataType: schema.TypeString},
				{Name: "phone", DataType: schema.TypeString},
				{Name: "photo_original", DataType: schema.TypeString},
				{Name: "photo_thumb", DataType: schema.TypeString},
				{Name: "photo_tiny", DataType: schema.TypeString},
				{Name: "time_zone_identifier", DataType: schema.TypeString},
				{Name: "url", DataType: schema.TypeString},
				{Name: "utc_hours_diff", DataType: schema.TypeInt64},
				{Name: "current_language", DataType: schema.TypeString},
				{Name: "account_id", DataType: schema.TypeString},
			},
			ExpectedRowCount: 1,
			Rows: []testutil.ExpectedRow{
				{
					ID: "99910119",
					Fields: map[string]any{
						"name":                 "Gong Test",
						"email":                "vendor_accounts@getbruin.com",
						"enabled":              true,
						"is_admin":             true,
						"is_guest":             false,
						"is_pending":           false,
						"is_view_only":         false,
						"country_code":         "TR",
						"phone":                "",
						"time_zone_identifier": "Europe/Istanbul",
						"url":                  "https://gongtest.monday.com/users/99910119",
						"utc_hours_diff":       int64(3),
						"current_language":     "en",
						"account_id":           "33820899",
					},
				},
			},
		},
		{
			SourceTable: "workspaces",
			DestTable:   "main.monday_workspaces",
			KeyColumn:   "id",
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "kind", DataType: schema.TypeString},
				{Name: "created_at", DataType: schema.TypeTimestampTZ},
				{Name: "is_default_workspace", DataType: schema.TypeBoolean},
				{Name: "state", DataType: schema.TypeString},
				{Name: "account_product_id", DataType: schema.TypeString},
				{Name: "owners_subscribers", DataType: schema.TypeJSON},
				{Name: "team_owners_subscribers", DataType: schema.TypeString},
				{Name: "teams_subscribers", DataType: schema.TypeString},
				{Name: "users_subscribers", DataType: schema.TypeJSON},
			},
			ExpectedRowCount: 1,
			Rows: []testutil.ExpectedRow{
				{
					ID: "5816004",
					Fields: map[string]any{
						"name":                 "Main workspace",
						"kind":                 "open",
						"is_default_workspace": true,
						"state":                "active",
						"account_product_id":   "6121987",
					},
				},
			},
		},
		// items: the new resource lands board rows with column_values as JSON.
		// 2 + 1 + 3 + 3 items across the four boards.
		{
			SourceTable: "items",
			DestTable:   "main.monday_items",
			KeyColumn:   "id",
			ExpectedSchema: []schema.Column{
				{Name: "board_id", DataType: schema.TypeString},
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "state", DataType: schema.TypeString},
				{Name: "created_at", DataType: schema.TypeTimestampTZ},
				{Name: "updated_at", DataType: schema.TypeTimestampTZ},
				{Name: "creator_id", DataType: schema.TypeString},
				{Name: "group_id", DataType: schema.TypeString},
				{Name: "group_title", DataType: schema.TypeString},
				{Name: "column_values", DataType: schema.TypeJSON},
			},
			ExpectedRowCount: 9,
		},
		// items scoped to a single board -> only that board's items (items_count = 3).
		{
			SourceTable:      "items:5091839751",
			DestTable:        "main.monday_items_initial",
			KeyColumn:        "id",
			ExpectedRowCount: 3,
			ExpectedSchema: []schema.Column{
				{Name: "board_id", DataType: schema.TypeString},
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "column_values", DataType: schema.TypeJSON},
			},
		},
		// :linked returns the master board's own items plus any linked sub-boards
		// (board name == master item title). No sub-board names match on this
		// account, so it returns at least the master's own 3 items.
		{
			SourceTable:         "items:5091839751:linked",
			DestTable:           "main.monday_items_linked",
			KeyColumn:           "id",
			MinExpectedRowCount: 3,
			ExpectedSchema: []schema.Column{
				{Name: "board_id", DataType: schema.TypeString},
				{Name: "id", DataType: schema.TypeString},
				{Name: "column_values", DataType: schema.TypeJSON},
			},
		},
		// board_columns scoped to a single board.
		{
			SourceTable:         "board_columns:5091839751",
			DestTable:           "main.monday_board_columns_scoped",
			KeyColumn:           "board_id || '~' || id",
			MinExpectedRowCount: 3,
			ExpectedSchema: []schema.Column{
				{Name: "board_id", DataType: schema.TypeString},
				{Name: "id", DataType: schema.TypeString},
				{Name: "title", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{ID: "5091839751~project_owner", Fields: map[string]any{"board_id": "5091839751", "title": "Owner", "type": "people"}},
				{ID: "5091839751~project_status", Fields: map[string]any{"board_id": "5091839751", "title": "Status", "type": "status"}},
				{ID: "5091839751~project_timeline", Fields: map[string]any{"board_id": "5091839751", "title": "Timeline", "type": "timeline"}},
			},
		},
		// board_views scoped to a single board.
		{
			SourceTable:         "board_views:5091839751",
			DestTable:           "main.monday_board_views_scoped",
			KeyColumn:           "board_id || '~' || id",
			MinExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "board_id", DataType: schema.TypeString},
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
			},
		},
		// boards scoped to a single board -> exactly that board.
		{
			SourceTable:      "boards:5091839751",
			DestTable:        "main.monday_boards_scoped",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "items_count", DataType: schema.TypeInt64},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "5091839751",
					Fields: map[string]any{
						"name":        "initial project",
						"items_count": int64(3),
					},
				},
			},
		},
		// updates scoped to a single board -> only updates on that board's items,
		// enriched with item_name + creator_name. One update on the initial project board.
		{
			SourceTable:      "updates:5091839751",
			DestTable:        "main.monday_updates_scoped",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "body", DataType: schema.TypeString},
				{Name: "creator_id", DataType: schema.TypeString},
				{Name: "creator_name", DataType: schema.TypeString},
				{Name: "item_id", DataType: schema.TypeString},
				{Name: "item_name", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "523528203",
					Fields: map[string]any{
						"body":         "Started working on initial task setup.",
						"creator_id":   "99910119",
						"creator_name": "Gong Test",
						"item_id":      "2723610670",
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
