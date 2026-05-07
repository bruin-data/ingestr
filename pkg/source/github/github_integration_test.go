package github_test

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

func TestGitHubPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	token := os.Getenv("GITHUB_ACCESS_TOKEN")
	if token == "" {
		t.Skip("Set GITHUB_ACCESS_TOKEN to run GitHub integration tests")
	}

	ctx := context.Background()
	sourceURI := fmt.Sprintf("github://?access_token=%s&owner=vendorAccGong&repo=gong-integration-test", token)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("github_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	expectations := []testutil.TableExpectation{
		{
			SourceTable: "issues",
			DestTable:   "main.github_issues",
			KeyColumn:   "number",
			ExpectedSchema: []schema.Column{
				{Name: "number", DataType: schema.TypeInt64},
				{Name: "url", DataType: schema.TypeString},
				{Name: "title", DataType: schema.TypeString},
				{Name: "body", DataType: schema.TypeString},
				{Name: "author", DataType: schema.TypeJSON},
				{Name: "authorAssociation", DataType: schema.TypeString},
				{Name: "closed", DataType: schema.TypeBoolean},
				{Name: "closedAt", DataType: schema.TypeTimestampTZ},
				{Name: "createdAt", DataType: schema.TypeTimestampTZ},
				{Name: "state", DataType: schema.TypeString},
				{Name: "updatedAt", DataType: schema.TypeTimestampTZ},
				{Name: "reactionsTotalCount", DataType: schema.TypeInt64},
				{Name: "reactions", DataType: schema.TypeJSON},
				{Name: "commentsTotalCount", DataType: schema.TypeInt64},
				{Name: "comments", DataType: schema.TypeJSON},
			},
			ExpectedRowCount: 3,
			Rows: []testutil.ExpectedRow{
				{
					ID: "1",
					Fields: map[string]any{
						"url":                 "https://github.com/vendorAccGong/gong-integration-test/issues/1",
						"title":               "Feature: Add Division function",
						"body":                "We need a Division function in utils.go that handles division by zero gracefully.",
						"authorAssociation":   "OWNER",
						"closed":              false,
						"closedAt":            nil,
						"createdAt":           time.Date(2026, 2, 14, 10, 38, 11, 0, time.UTC),
						"state":               "OPEN",
						"reactionsTotalCount": int64(0),
						"reactions":           "[]",
						"commentsTotalCount":  int64(1),
					},
				},
				{
					ID: "2",
					Fields: map[string]any{
						"url":                 "https://github.com/vendorAccGong/gong-integration-test/issues/2",
						"title":               "Add unit tests for utils package",
						"body":                "We need unit tests for Add and Multiply functions.",
						"authorAssociation":   "OWNER",
						"closed":              true,
						"closedAt":            time.Date(2026, 2, 14, 10, 38, 25, 0, time.UTC),
						"createdAt":           time.Date(2026, 2, 14, 10, 38, 11, 0, time.UTC),
						"state":               "CLOSED",
						"reactionsTotalCount": int64(0),
						"reactions":           "[]",
						"commentsTotalCount":  int64(0),
						"comments":            "[]",
					},
				},
				{
					ID: "3",
					Fields: map[string]any{
						"url":                 "https://github.com/vendorAccGong/gong-integration-test/issues/3",
						"title":               "Bug: Add function returns wrong result for negative numbers",
						"body":                "When passing negative numbers to the Add function, the result is incorrect.",
						"authorAssociation":   "OWNER",
						"closed":              false,
						"closedAt":            nil,
						"createdAt":           time.Date(2026, 2, 14, 10, 38, 19, 0, time.UTC),
						"state":               "OPEN",
						"reactionsTotalCount": int64(0),
						"reactions":           "[]",
						"commentsTotalCount":  int64(0),
						"comments":            "[]",
					},
				},
			},
		},
		{
			SourceTable: "pull_requests",
			DestTable:   "main.github_pull_requests",
			KeyColumn:   "number",
			ExpectedSchema: []schema.Column{
				{Name: "number", DataType: schema.TypeInt64},
				{Name: "url", DataType: schema.TypeString},
				{Name: "title", DataType: schema.TypeString},
				{Name: "body", DataType: schema.TypeString},
				{Name: "author", DataType: schema.TypeJSON},
				{Name: "authorAssociation", DataType: schema.TypeString},
				{Name: "closed", DataType: schema.TypeBoolean},
				{Name: "closedAt", DataType: schema.TypeTimestampTZ},
				{Name: "createdAt", DataType: schema.TypeTimestampTZ},
				{Name: "state", DataType: schema.TypeString},
				{Name: "updatedAt", DataType: schema.TypeTimestampTZ},
				{Name: "merged", DataType: schema.TypeBoolean},
				{Name: "mergedAt", DataType: schema.TypeTimestampTZ},
				{Name: "reactionsTotalCount", DataType: schema.TypeInt64},
				{Name: "reactions", DataType: schema.TypeJSON},
				{Name: "commentsTotalCount", DataType: schema.TypeInt64},
				{Name: "comments", DataType: schema.TypeJSON},
			},
			ExpectedRowCount: 2,
			Rows: []testutil.ExpectedRow{
				{
					ID: "4",
					Fields: map[string]any{
						"url":                 "https://github.com/vendorAccGong/gong-integration-test/pull/4",
						"title":               "Add unit tests for utils package",
						"body":                `This PR adds unit tests for the Add and Multiply functions.\n\nCloses #2`,
						"authorAssociation":   "OWNER",
						"closed":              false,
						"closedAt":            nil,
						"createdAt":           time.Date(2026, 2, 14, 10, 38, 56, 0, time.UTC),
						"state":               "OPEN",
						"merged":              false,
						"mergedAt":            nil,
						"reactionsTotalCount": int64(0),
						"reactions":           "[]",
						"commentsTotalCount":  int64(1),
					},
				},
				{
					ID: "5",
					Fields: map[string]any{
						"url":                 "https://github.com/vendorAccGong/gong-integration-test/pull/5",
						"title":               "Fix: add Subtract function",
						"body":                `Adds a Subtract function to utils.go.\n\nRelated to #1`,
						"authorAssociation":   "OWNER",
						"closed":              false,
						"closedAt":            nil,
						"createdAt":           time.Date(2026, 2, 14, 10, 38, 57, 0, time.UTC),
						"state":               "OPEN",
						"merged":              false,
						"mergedAt":            nil,
						"reactionsTotalCount": int64(0),
						"reactions":           "[]",
						"commentsTotalCount":  int64(0),
						"comments":            "[]",
					},
				},
			},
		},
		{
			SourceTable: "stargazers",
			DestTable:   "main.github_stargazers",
			ExpectedSchema: []schema.Column{
				{Name: "starredAt", DataType: schema.TypeTimestampTZ},
				{Name: "node", DataType: schema.TypeJSON},
			},
			ExpectedRowCount: 1,
		},
		{
			SourceTable: "repo_events",
			DestTable:   "main.github_repo_events",
			KeyColumn:   "id",
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
				{Name: "created_at", DataType: schema.TypeTimestampTZ},
				{Name: "actor", DataType: schema.TypeJSON},
				{Name: "repo", DataType: schema.TypeJSON},
				{Name: "payload", DataType: schema.TypeJSON},
				{Name: "public", DataType: schema.TypeBoolean},
				{Name: "org", DataType: schema.TypeJSON},
			},
			ExpectedRowCount: 16,
			Rows: []testutil.ExpectedRow{
				{
					ID: "6580443452",
					Fields: map[string]any{
						"type":       "WatchEvent",
						"created_at": time.Date(2026, 2, 14, 10, 39, 34, 0, time.UTC),
						"public":     true,
						"org":        nil,
					},
				},
				{
					ID: "6580443306",
					Fields: map[string]any{
						"type":       "IssueCommentEvent",
						"created_at": time.Date(2026, 2, 14, 10, 39, 34, 0, time.UTC),
						"public":     true,
						"org":        nil,
					},
				},
				{
					ID: "6580443241",
					Fields: map[string]any{
						"type":       "IssueCommentEvent",
						"created_at": time.Date(2026, 2, 14, 10, 39, 33, 0, time.UTC),
						"public":     true,
						"org":        nil,
					},
				},
				{
					ID: "6580438383",
					Fields: map[string]any{
						"type":       "PullRequestEvent",
						"created_at": time.Date(2026, 2, 14, 10, 38, 57, 0, time.UTC),
						"public":     true,
						"org":        nil,
					},
				},
				{
					ID: "6580438259",
					Fields: map[string]any{
						"type":       "PullRequestEvent",
						"created_at": time.Date(2026, 2, 14, 10, 38, 56, 0, time.UTC),
						"public":     true,
						"org":        nil,
					},
				},
				{
					ID: "6580434380",
					Fields: map[string]any{
						"type":       "IssuesEvent",
						"created_at": time.Date(2026, 2, 14, 10, 38, 26, 0, time.UTC),
						"public":     true,
						"org":        nil,
					},
				},
				{
					ID: "6580433455",
					Fields: map[string]any{
						"type":       "IssuesEvent",
						"created_at": time.Date(2026, 2, 14, 10, 38, 20, 0, time.UTC),
						"public":     true,
						"org":        nil,
					},
				},
				{
					ID: "6580432405",
					Fields: map[string]any{
						"type":       "IssuesEvent",
						"created_at": time.Date(2026, 2, 14, 10, 38, 12, 0, time.UTC),
						"public":     true,
						"org":        nil,
					},
				},
				{
					ID: "6580432403",
					Fields: map[string]any{
						"type":       "IssuesEvent",
						"created_at": time.Date(2026, 2, 14, 10, 38, 12, 0, time.UTC),
						"public":     true,
						"org":        nil,
					},
				},
				{
					ID: "8445478680",
					Fields: map[string]any{
						"type":       "PushEvent",
						"created_at": time.Date(2026, 2, 14, 10, 36, 48, 0, time.UTC),
						"public":     true,
						"org":        nil,
					},
				},
				{
					ID: "8445474016",
					Fields: map[string]any{
						"type":       "PushEvent",
						"created_at": time.Date(2026, 2, 14, 10, 36, 29, 0, time.UTC),
						"public":     true,
						"org":        nil,
					},
				},
				{
					ID: "8445376425",
					Fields: map[string]any{
						"type":       "CreateEvent",
						"created_at": time.Date(2026, 2, 14, 10, 29, 19, 0, time.UTC),
						"public":     true,
						"org":        nil,
					},
				},
				{
					ID: "8445376193",
					Fields: map[string]any{
						"type":       "CreateEvent",
						"created_at": time.Date(2026, 2, 14, 10, 29, 18, 0, time.UTC),
						"public":     true,
						"org":        nil,
					},
				},
				{
					ID: "8424726476",
					Fields: map[string]any{
						"type":       "PushEvent",
						"created_at": time.Date(2026, 2, 13, 15, 20, 47, 0, time.UTC),
						"public":     true,
						"org":        nil,
					},
				},
				{
					ID: "8423964080",
					Fields: map[string]any{
						"type":       "PushEvent",
						"created_at": time.Date(2026, 2, 13, 15, 20, 39, 0, time.UTC),
						"public":     true,
						"org":        nil,
					},
				},
				{
					ID: "8423958141",
					Fields: map[string]any{
						"type":       "CreateEvent",
						"created_at": time.Date(2026, 2, 13, 15, 20, 27, 0, time.UTC),
						"public":     true,
						"org":        nil,
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
