//go:build integration

package jira_test

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

func TestJiraPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	domain := os.Getenv("JIRA_DOMAIN")
	email := os.Getenv("JIRA_EMAIL")
	apiToken := os.Getenv("JIRA_API_TOKEN")
	if domain == "" || email == "" || apiToken == "" {
		t.Skip("Set JIRA_DOMAIN, JIRA_EMAIL, and JIRA_API_TOKEN to run Jira integration tests")
	}

	ctx := context.Background()
	sourceURI := fmt.Sprintf("jira://%s.atlassian.net?email=%s&api_token=%s", domain, email, apiToken)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("jira_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	expectations := []testutil.TableExpectation{
		{
			SourceTable:      "projects",
			DestTable:        "main.jira_projects",
			KeyColumn:        "id",
			ExpectedRowCount: 2,
			ExpectedSchema: []schema.Column{
				{Name: "entityid", DataType: schema.TypeString},
				{Name: "expand", DataType: schema.TypeString},
				{Name: "id", DataType: schema.TypeString},
				{Name: "isprivate", DataType: schema.TypeBoolean},
				{Name: "key", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "projecttypekey", DataType: schema.TypeString},
				{Name: "self", DataType: schema.TypeString},
				{Name: "simplified", DataType: schema.TypeBoolean},
				{Name: "style", DataType: schema.TypeString},
				{Name: "uuid", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "10000",
					Fields: map[string]any{
						"key":            "SAM1",
						"name":           "(Example) Billing System Dev",
						"projectTypeKey": "software",
						"simplified":     true,
						"isPrivate":      false,
						"style":          "next-gen",
					},
				},
			},
		},
		{
			SourceTable:         "users",
			DestTable:           "main.jira_users",
			KeyColumn:           "accountId",
			MinExpectedRowCount: 47,
			ExpectedSchema: []schema.Column{
				{Name: "accountid", DataType: schema.TypeString},
				{Name: "accounttype", DataType: schema.TypeString},
				{Name: "active", DataType: schema.TypeBoolean},
				{Name: "displayname", DataType: schema.TypeString},
				{Name: "emailaddress", DataType: schema.TypeString},
				{Name: "locale", DataType: schema.TypeString},
				{Name: "self", DataType: schema.TypeString},
				{Name: "apptype", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "712020:b354a490-28c5-4a2f-8698-3e4fa62ba495",
					Fields: map[string]any{
						"displayName": "Vendor Accounts",
						"accountType": "atlassian",
						"active":      true,
						"locale":      "en_US",
					},
				},
			},
		},
		{
			SourceTable:      "issue_types",
			DestTable:        "main.jira_issue_types",
			KeyColumn:        "id",
			ExpectedRowCount: 10,
			ExpectedSchema: []schema.Column{
				{Name: "avatarid", DataType: schema.TypeInt64},
				{Name: "description", DataType: schema.TypeString},
				{Name: "hierarchylevel", DataType: schema.TypeInt64},
				{Name: "iconurl", DataType: schema.TypeString},
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "self", DataType: schema.TypeString},
				{Name: "subtask", DataType: schema.TypeBoolean},
				{Name: "untranslatedname", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "10002",
					Fields: map[string]any{
						"name":             "Subtask",
						"untranslatedName": "Subtask",
						"subtask":          true,
						"hierarchyLevel":   int64(-1),
						"avatarId":         int64(10316),
					},
				},
			},
		},
		{
			SourceTable:      "statuses",
			DestTable:        "main.jira_statuses",
			KeyColumn:        "id",
			ExpectedRowCount: 8,
			ExpectedSchema: []schema.Column{
				{Name: "description", DataType: schema.TypeString},
				{Name: "iconurl", DataType: schema.TypeString},
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "self", DataType: schema.TypeString},
				{Name: "untranslatedname", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "10000",
					Fields: map[string]any{
						"name":             "To Do",
						"untranslatedName": "To Do",
					},
				},
			},
		},
		{
			SourceTable:      "priorities",
			DestTable:        "main.jira_priorities",
			KeyColumn:        "id",
			ExpectedRowCount: 5,
			ExpectedSchema: []schema.Column{
				{Name: "description", DataType: schema.TypeString},
				{Name: "iconurl", DataType: schema.TypeString},
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "self", DataType: schema.TypeString},
				{Name: "statuscolor", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "1",
					Fields: map[string]any{
						"name":        "Highest",
						"description": "This problem will block progress.",
						"statusColor": "#d04437",
					},
				},
			},
		},
		{
			SourceTable:      "resolutions",
			DestTable:        "main.jira_resolutions",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "description", DataType: schema.TypeString},
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "self", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "10000",
					Fields: map[string]any{
						"name": "Done",
					},
				},
			},
		},
		{
			SourceTable:      "events",
			DestTable:        "main.jira_events",
			KeyColumn:        "id",
			ExpectedRowCount: 17,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "name", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "1",
					Fields: map[string]any{
						"name": "Issue Created",
					},
				},
			},
		},
		{
			SourceTable:      "project_versions",
			DestTable:        "main.jira_project_versions",
			KeyColumn:        "id",
			ExpectedRowCount: 2,
			ExpectedSchema: []schema.Column{
				{Name: "archived", DataType: schema.TypeBoolean},
				{Name: "description", DataType: schema.TypeString},
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "projectid", DataType: schema.TypeInt64},
				{Name: "released", DataType: schema.TypeBoolean},
				{Name: "self", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "10001",
					Fields: map[string]any{
						"name":        "v2.0.0",
						"description": "Second release",
						"archived":    false,
						"released":    true,
						"projectId":   int64(10000),
					},
				},
			},
		},
		{
			SourceTable:      "project_components",
			DestTable:        "main.jira_project_components",
			KeyColumn:        "id",
			ExpectedRowCount: 2,
			ExpectedSchema: []schema.Column{
				{Name: "assigneetype", DataType: schema.TypeString},
				{Name: "description", DataType: schema.TypeString},
				{Name: "id", DataType: schema.TypeString},
				{Name: "isassigneetypevalid", DataType: schema.TypeBoolean},
				{Name: "issuecount", DataType: schema.TypeInt64},
				{Name: "name", DataType: schema.TypeString},
				{Name: "project", DataType: schema.TypeString},
				{Name: "projectid", DataType: schema.TypeInt64},
				{Name: "realassigneetype", DataType: schema.TypeString},
				{Name: "self", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "10001",
					Fields: map[string]any{
						"name":                "Frontend",
						"description":         "Frontend UI",
						"project":             "SAM1",
						"projectId":           int64(10000),
						"assigneeType":        "PROJECT_DEFAULT",
						"realAssigneeType":    "PROJECT_DEFAULT",
						"isAssigneeTypeValid": false,
						"issueCount":          int64(0),
					},
				},
			},
		},
		{
			SourceTable:         "issues",
			DestTable:           "main.jira_issues",
			KeyColumn:           "id",
			MinExpectedRowCount: 17,
			ExpectedSchema: []schema.Column{
				{Name: "expand", DataType: schema.TypeString},
				{Name: "fields_created", DataType: schema.TypeTimestampTZ},
				{Name: "fields_customfield_10019", DataType: schema.TypeTimestampTZ},
				{Name: "fields_duedate", DataType: schema.TypeDate},
				{Name: "fields_statuscategorychangedate", DataType: schema.TypeTimestampTZ},
				{Name: "fields_summary", DataType: schema.TypeString},
				{Name: "fields_updated", DataType: schema.TypeTimestampTZ},
				{Name: "fields_workratio", DataType: schema.TypeInt64},
				{Name: "id", DataType: schema.TypeString},
				{Name: "key", DataType: schema.TypeString},
				{Name: "self", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "10000",
					Fields: map[string]any{
						"key":              "SAM1-1",
						"fields_summary":   "Implement User Authentication",
						"fields_workratio": int64(-1),
					},
				},
			},
		},
		{
			SourceTable:         "issue_changelogs",
			DestTable:           "main.jira_issue_changelogs",
			KeyColumn:           "id",
			MinExpectedRowCount: 21,
			ExpectedSchema: []schema.Column{
				{Name: "created", DataType: schema.TypeTimestampTZ},
				{Name: "id", DataType: schema.TypeString},
				{Name: "issue_id", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "10005",
					Fields: map[string]any{
						"issue_id": "10000",
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
