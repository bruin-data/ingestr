//go:build integration

package klaviyo_test

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

func TestKlaviyoPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	apiKey := os.Getenv("KLAVIYO_API_KEY")
	if apiKey == "" {
		t.Skip("Set KLAVIYO_API_KEY to run Klaviyo integration tests")
	}

	ctx := context.Background()
	sourceURI := fmt.Sprintf("klaviyo://?api_key=%s", apiKey)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("klaviyo_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	expectations := []testutil.TableExpectation{
		{
			SourceTable:      "lists",
			DestTable:        "main.klaviyo_lists",
			KeyColumn:        "id",
			ExpectedRowCount: 3,
			ExpectedSchema: []schema.Column{
				{Name: "created", DataType: schema.TypeTimestampTZ},
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "opt_in_process", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
				{Name: "updated", DataType: schema.TypeTimestampTZ},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "ThGFRh",
					Fields: map[string]any{
						"name": "Email List",
						"type": "list",
					},
				},
			},
		},
		{
			SourceTable:      "metrics",
			DestTable:        "main.klaviyo_metrics",
			KeyColumn:        "id",
			ExpectedRowCount: 19,
			ExpectedSchema: []schema.Column{
				{Name: "created", DataType: schema.TypeTimestampTZ},
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
				{Name: "updated", DataType: schema.TypeTimestampTZ},
			},
		},
		{
			SourceTable:      "segments",
			DestTable:        "main.klaviyo_segments",
			KeyColumn:        "id",
			ExpectedRowCount: 4,
			ExpectedSchema: []schema.Column{
				{Name: "created", DataType: schema.TypeTimestampTZ},
				{Name: "id", DataType: schema.TypeString},
				{Name: "is_active", DataType: schema.TypeBoolean},
				{Name: "is_processing", DataType: schema.TypeBoolean},
				{Name: "is_starred", DataType: schema.TypeBoolean},
				{Name: "name", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
				{Name: "updated", DataType: schema.TypeTimestampTZ},
			},
		},
		{
			SourceTable:      "tags",
			DestTable:        "main.klaviyo_tags",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
			},
		},
		{
			SourceTable:      "coupons",
			DestTable:        "main.klaviyo_coupons",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
			},
		},
		{
			SourceTable:      "images",
			DestTable:        "main.klaviyo_images",
			KeyColumn:        "id",
			ExpectedRowCount: 5,
			ExpectedSchema: []schema.Column{
				{Name: "format", DataType: schema.TypeString},
				{Name: "hidden", DataType: schema.TypeBoolean},
				{Name: "id", DataType: schema.TypeString},
				{Name: "image_url", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "size", DataType: schema.TypeInt64},
				{Name: "type", DataType: schema.TypeString},
				{Name: "updated_at", DataType: schema.TypeTimestampTZ},
			},
		},
		{
			SourceTable:      "profiles",
			DestTable:        "main.klaviyo_profiles",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "created", DataType: schema.TypeTimestampTZ},
				{Name: "email", DataType: schema.TypeString},
				{Name: "id", DataType: schema.TypeString},
				{Name: "last_event_date", DataType: schema.TypeTimestampTZ},
				{Name: "type", DataType: schema.TypeString},
				{Name: "updated", DataType: schema.TypeTimestampTZ},
			},
		},
		{
			SourceTable:      "events",
			DestTable:        "main.klaviyo_events",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "datetime", DataType: schema.TypeTimestampTZ},
				{Name: "id", DataType: schema.TypeString},
				{Name: "timestamp", DataType: schema.TypeInt64},
				{Name: "type", DataType: schema.TypeString},
				{Name: "uuid", DataType: schema.TypeString},
			},
		},
		{
			SourceTable:      "templates",
			DestTable:        "main.klaviyo_templates",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "created", DataType: schema.TypeTimestampTZ},
				{Name: "editor_type", DataType: schema.TypeString},
				{Name: "html", DataType: schema.TypeString},
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
				{Name: "updated", DataType: schema.TypeTimestampTZ},
			},
		},
		{
			SourceTable:      "catalog-items",
			DestTable:        "main.klaviyo_catalog_items",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "created", DataType: schema.TypeTimestampTZ},
				{Name: "description", DataType: schema.TypeString},
				{Name: "external_id", DataType: schema.TypeString},
				{Name: "id", DataType: schema.TypeString},
				{Name: "image_full_url", DataType: schema.TypeString},
				{Name: "image_thumbnail_url", DataType: schema.TypeString},
				{Name: "price", DataType: schema.TypeFloat64},
				{Name: "published", DataType: schema.TypeBoolean},
				{Name: "title", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
				{Name: "updated", DataType: schema.TypeTimestampTZ},
				{Name: "url", DataType: schema.TypeString},
			},
		},
		{
			SourceTable:      "catalog-variants",
			DestTable:        "main.klaviyo_catalog_variants",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "created", DataType: schema.TypeTimestampTZ},
				{Name: "description", DataType: schema.TypeString},
				{Name: "external_id", DataType: schema.TypeString},
				{Name: "id", DataType: schema.TypeString},
				{Name: "image_full_url", DataType: schema.TypeString},
				{Name: "image_thumbnail_url", DataType: schema.TypeString},
				{Name: "inventory_policy", DataType: schema.TypeInt64},
				{Name: "inventory_quantity", DataType: schema.TypeFloat64},
				{Name: "price", DataType: schema.TypeFloat64},
				{Name: "published", DataType: schema.TypeBoolean},
				{Name: "sku", DataType: schema.TypeString},
				{Name: "title", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
				{Name: "updated", DataType: schema.TypeTimestampTZ},
				{Name: "url", DataType: schema.TypeString},
			},
		},
		{
			SourceTable:      "catalog-categories",
			DestTable:        "main.klaviyo_catalog_categories",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "external_id", DataType: schema.TypeString},
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
				{Name: "updated", DataType: schema.TypeTimestampTZ},
			},
		},
		{
			SourceTable:      "flows",
			DestTable:        "main.klaviyo_flows",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "archived", DataType: schema.TypeBoolean},
				{Name: "created", DataType: schema.TypeTimestampTZ},
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "status", DataType: schema.TypeString},
				{Name: "trigger_type", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
				{Name: "updated", DataType: schema.TypeTimestampTZ},
			},
		},
		{
			SourceTable:      "forms",
			DestTable:        "main.klaviyo_forms",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "ab_test", DataType: schema.TypeBoolean},
				{Name: "created_at", DataType: schema.TypeTimestampTZ},
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "status", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
				{Name: "updated_at", DataType: schema.TypeTimestampTZ},
			},
		},
		{
			SourceTable:      "campaigns",
			DestTable:        "main.klaviyo_campaigns",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "archived", DataType: schema.TypeBoolean},
				{Name: "campaign_type", DataType: schema.TypeString},
				{Name: "created_at", DataType: schema.TypeTimestampTZ},
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "status", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
				{Name: "updated_at", DataType: schema.TypeTimestampTZ},
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
