//go:build integration

package pinterest_test

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

func TestPinterestPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	accessToken := os.Getenv("PINTEREST_ACCESS_TOKEN")
	if accessToken == "" {
		t.Skip("Set PINTEREST_ACCESS_TOKEN to run Pinterest integration tests")
	}

	ctx := context.Background()
	sourceURI := fmt.Sprintf("pinterest://?access_token=%s", accessToken)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("pinterest_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	expectations := []testutil.TableExpectation{
		{
			SourceTable:      "boards",
			DestTable:        "main.pinterest_boards",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "board_pins_modified_at", DataType: schema.TypeTimestampTZ},
				{Name: "collaborator_count", DataType: schema.TypeInt64},
				{Name: "created_at", DataType: schema.TypeTimestampTZ},
				{Name: "description", DataType: schema.TypeString},
				{Name: "follower_count", DataType: schema.TypeInt64},
				{Name: "id", DataType: schema.TypeString},
				{Name: "is_ads_only", DataType: schema.TypeBoolean},
				{Name: "name", DataType: schema.TypeString},
				{Name: "pin_count", DataType: schema.TypeInt64},
				{Name: "privacy", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "1148206936188039028",
					Fields: map[string]any{
						"name":               "Test Board",
						"privacy":            "PUBLIC",
						"description":        "",
						"collaborator_count": int64(0),
						"follower_count":     int64(0),
						"pin_count":          int64(1),
						"is_ads_only":        false,
					},
				},
			},
		},
		{
			SourceTable:      "pins",
			DestTable:        "main.pinterest_pins",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "board_id", DataType: schema.TypeString},
				{Name: "created_at", DataType: schema.TypeTimestampTZ},
				{Name: "creative_type", DataType: schema.TypeString},
				{Name: "description", DataType: schema.TypeString},
				{Name: "dominant_color", DataType: schema.TypeString},
				{Name: "has_been_promoted", DataType: schema.TypeBoolean},
				{Name: "id", DataType: schema.TypeString},
				{Name: "is_owner", DataType: schema.TypeBoolean},
				{Name: "is_removable", DataType: schema.TypeBoolean},
				{Name: "is_standard", DataType: schema.TypeBoolean},
				{Name: "link", DataType: schema.TypeString},
				{Name: "parent_pin_id", DataType: schema.TypeString},
				{Name: "title", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "1148206867516375890",
					Fields: map[string]any{
						"title":             "i met you in this life",
						"creative_type":     "IDEA",
						"board_id":          "1148206936188039028",
						"is_owner":          false,
						"is_standard":       false,
						"has_been_promoted": false,
						"dominant_color":    "#84aeaf",
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
