//go:build integration

package trello_test

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

func TestTrelloPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	apiKey := os.Getenv("TRELLO_API_KEY")
	token := os.Getenv("TRELLO_TOKEN")
	if apiKey == "" || token == "" {
		t.Skip("Set TRELLO_API_KEY and TRELLO_TOKEN to run Trello integration tests")
	}

	ctx := context.Background()
	sourceURI := fmt.Sprintf("trello://?api_key=%s&token=%s", apiKey, token)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("trello_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	const (
		boardID = "6a4b94d0ebf0cf58f0708182"
		orgID   = "6a4b94d0ebf0cf58f070816a"
		memID   = "6a4b947b73a1197b27a39ff0"
		todoID  = "6a4b94d0ebf0cf58f070819b"
		cardID  = "6a4b9580346a52f436f26e21"
	)

	expectations := []testutil.TableExpectation{
		{
			SourceTable:      "boards",
			DestTable:        "main.trello_boards",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "closed", DataType: schema.TypeBoolean},
				{Name: "id_organization", DataType: schema.TypeString},
				{Name: "id_member_creator", DataType: schema.TypeString},
				{Name: "short_link", DataType: schema.TypeString},
				{Name: "short_url", DataType: schema.TypeString},
				{Name: "url", DataType: schema.TypeString},
				{Name: "pinned", DataType: schema.TypeBoolean},
				{Name: "starred", DataType: schema.TypeBoolean},
				{Name: "subscribed", DataType: schema.TypeBoolean},
				{Name: "enterprise_owned", DataType: schema.TypeBoolean},
				{Name: "creation_method", DataType: schema.TypeString},
				{Name: "node_id", DataType: schema.TypeString},
				{Name: "date_last_activity", DataType: schema.TypeTimestampTZ},
				{Name: "date_last_view", DataType: schema.TypeTimestampTZ},
				{Name: "ix_update", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: boardID,
					Fields: map[string]any{
						"name":              "Ingestr Test Board",
						"closed":            false,
						"id_organization":   orgID,
						"id_member_creator": memID,
						"short_link":        "PpWFqkuP",
						"short_url":         "https://trello.com/b/PpWFqkuP",
						"url":               "https://trello.com/b/PpWFqkuP/ingestr-test-board",
						"pinned":            false,
						"starred":           false,
						"subscribed":        false,
						"enterprise_owned":  false,
						"creation_method":   "automatic",
						"node_id":           "ari:cloud:trello::board/workspace/6a4b94d0ebf0cf58f070816a/6a4b94d0ebf0cf58f0708182",
					},
				},
			},
		},
		{
			SourceTable:      "organizations",
			DestTable:        "main.trello_organizations",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "display_name", DataType: schema.TypeString},
				{Name: "id_member_creator", DataType: schema.TypeString},
				{Name: "url", DataType: schema.TypeString},
				{Name: "offering", DataType: schema.TypeString},
				{Name: "node_id", DataType: schema.TypeString},
				{Name: "domain_name", DataType: schema.TypeString},
				{Name: "ix_update", DataType: schema.TypeString},
				{Name: "ai_eligible", DataType: schema.TypeBoolean},
				{Name: "billing_locked", DataType: schema.TypeBoolean},
				{Name: "invited", DataType: schema.TypeBoolean},
				{Name: "billable_collaborator_count", DataType: schema.TypeInt64},
				{Name: "billable_member_count", DataType: schema.TypeInt64},
				{Name: "members_count", DataType: schema.TypeInt64},
				{Name: "date_last_activity", DataType: schema.TypeTimestampTZ},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: orgID,
					Fields: map[string]any{
						"name":              "workspace20073132",
						"display_name":      "Vendor Accounts's workspace",
						"id_member_creator": memID,
						"url":               "https://trello.com/w/workspace20073132",
						"offering":          "trello.free",
						"node_id":           "ari:cloud:trello::workspace/6a4b94d0ebf0cf58f070816a",
					},
				},
			},
		},
		{
			SourceTable:      "lists",
			DestTable:        "main.trello_lists",
			KeyColumn:        "id",
			ExpectedRowCount: 3,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "id_board", DataType: schema.TypeString},
				{Name: "closed", DataType: schema.TypeBoolean},
				{Name: "subscribed", DataType: schema.TypeBoolean},
				{Name: "pos", DataType: schema.TypeInt64},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: todoID,
					Fields: map[string]any{
						"name":       "To Do",
						"id_board":   boardID,
						"closed":     false,
						"subscribed": false,
						"pos":        int64(140737488355328),
					},
				},
			},
		},
		{
			SourceTable:      "members",
			DestTable:        "main.trello_members",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "full_name", DataType: schema.TypeString},
				{Name: "username", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: memID,
					Fields: map[string]any{
						"full_name": "Vendor Accounts",
						"username":  "vendoraccounts",
					},
				},
			},
		},
		{
			SourceTable:      "labels",
			DestTable:        "main.trello_labels",
			KeyColumn:        "id",
			ExpectedRowCount: 6,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "id_board", DataType: schema.TypeString},
				{Name: "color", DataType: schema.TypeString},
				{Name: "uses", DataType: schema.TypeInt64},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "6a4b94d0ebf0cf58f07081a7",
					Fields: map[string]any{
						"id_board": boardID,
						"color":    "blue",
						"uses":     int64(0),
					},
				},
			},
		},
		{
			SourceTable:      "checklists",
			DestTable:        "main.trello_checklists",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "id_board", DataType: schema.TypeString},
				{Name: "id_card", DataType: schema.TypeString},
				{Name: "pos", DataType: schema.TypeInt64},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "6a4b958265ecadd2c010e858",
					Fields: map[string]any{
						"name":     "Acceptance criteria",
						"id_board": boardID,
						"id_card":  cardID,
						"pos":      int64(140737488355328),
					},
				},
			},
		},
		{
			SourceTable:      "cards",
			DestTable:        "main.trello_cards",
			KeyColumn:        "id",
			ExpectedRowCount: 4,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "desc", DataType: schema.TypeString},
				{Name: "id_board", DataType: schema.TypeString},
				{Name: "id_list", DataType: schema.TypeString},
				{Name: "closed", DataType: schema.TypeBoolean},
				{Name: "due_complete", DataType: schema.TypeBoolean},
				{Name: "subscribed", DataType: schema.TypeBoolean},
				{Name: "pinned", DataType: schema.TypeBoolean},
				{Name: "is_template", DataType: schema.TypeBoolean},
				{Name: "manual_cover_attachment", DataType: schema.TypeBoolean},
				{Name: "id_short", DataType: schema.TypeInt64},
				{Name: "pos", DataType: schema.TypeInt64},
				{Name: "short_link", DataType: schema.TypeString},
				{Name: "short_url", DataType: schema.TypeString},
				{Name: "url", DataType: schema.TypeString},
				{Name: "node_id", DataType: schema.TypeString},
				{Name: "date_last_activity", DataType: schema.TypeTimestampTZ},
			},
			// "desc" is a reserved word in DuckDB, so it is verified via the schema
			// only and omitted from the row field checks.
			Rows: []testutil.ExpectedRow{
				{
					ID: cardID,
					Fields: map[string]any{
						"name":                    "Design schema",
						"id_board":                boardID,
						"id_list":                 todoID,
						"closed":                  false,
						"due_complete":            false,
						"subscribed":              false,
						"pinned":                  false,
						"is_template":             false,
						"manual_cover_attachment": false,
						"id_short":                int64(1),
						"pos":                     int64(140737488355328),
						"short_link":              "WA3Jflwe",
						"short_url":               "https://trello.com/c/WA3Jflwe",
						"url":                     "https://trello.com/c/WA3Jflwe/1-design-schema",
						"node_id":                 "ari:cloud:trello::card/workspace/6a4b94d0ebf0cf58f070816a/6a4b9580346a52f436f26e21",
					},
				},
			},
		},
		{
			SourceTable:      "actions",
			DestTable:        "main.trello_actions",
			KeyColumn:        "id",
			ExpectedRowCount: 8,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
				{Name: "id_member_creator", DataType: schema.TypeString},
				{Name: "date", DataType: schema.TypeTimestampTZ},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "6a4b9583336fab7126d7e2f9",
					Fields: map[string]any{
						"type":              "commentCard",
						"id_member_creator": memID,
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
