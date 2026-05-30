//go:build integration

package revenuecat_test

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

func TestRevenueCatPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	apiKey := os.Getenv("REVENUECAT_API_KEY")
	if apiKey == "" {
		t.Skip("Set REVENUECAT_API_KEY to run RevenueCat integration tests")
	}

	projectID := os.Getenv("REVENUECAT_PROJECT_ID")
	if projectID == "" {
		t.Skip("Set REVENUECAT_PROJECT_ID to run RevenueCat integration tests")
	}

	ctx := context.Background()
	sourceURI := fmt.Sprintf("revenuecat://?api_key=%s&project_id=%s", apiKey, projectID)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("revenuecat_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	expectations := []testutil.TableExpectation{
		{
			SourceTable:      "customers",
			DestTable:        "main.revenuecat_customers",
			KeyColumn:        "id",
			ExpectedRowCount: 2,
			ExpectedSchema: []schema.Column{
				{Name: "first_seen_at", DataType: schema.TypeTimestampTZ},
				{Name: "id", DataType: schema.TypeString},
				{Name: "last_seen_at", DataType: schema.TypeTimestampTZ},
				{Name: "object", DataType: schema.TypeString},
				{Name: "project_id", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "gong_test_customer_01",
					Fields: map[string]any{
						"object":     "customer",
						"project_id": "projc09fd2a0",
					},
				},
			},
		},
		{
			SourceTable:      "projects",
			DestTable:        "main.revenuecat_projects",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "created_at", DataType: schema.TypeTimestampTZ},
				{Name: "id", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "object", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "projc09fd2a0",
					Fields: map[string]any{
						"name":   "Bruin Data",
						"object": "project",
					},
				},
			},
		},
		{
			SourceTable:      "products",
			DestTable:        "main.revenuecat_products",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "app_id", DataType: schema.TypeString},
				{Name: "created_at", DataType: schema.TypeInt64},
				{Name: "display_name", DataType: schema.TypeString},
				{Name: "id", DataType: schema.TypeString},
				{Name: "object", DataType: schema.TypeString},
				{Name: "state", DataType: schema.TypeString},
				{Name: "store_identifier", DataType: schema.TypeString},
				{Name: "type", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "prodc77599c496",
					Fields: map[string]any{
						"app_id":           "app5bcca3ab6c",
						"created_at":       int64(1774275709251),
						"display_name":     "Gong Test Monthly",
						"object":           "product",
						"state":            "active",
						"store_identifier": "gong_test_monthly",
						"type":             "subscription",
					},
				},
			},
		},
		{
			SourceTable:      "entitlements",
			DestTable:        "main.revenuecat_entitlements",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "created_at", DataType: schema.TypeInt64},
				{Name: "display_name", DataType: schema.TypeString},
				{Name: "id", DataType: schema.TypeString},
				{Name: "lookup_key", DataType: schema.TypeString},
				{Name: "object", DataType: schema.TypeString},
				{Name: "project_id", DataType: schema.TypeString},
				{Name: "state", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "entl7f26ee86a5",
					Fields: map[string]any{
						"created_at":   int64(1774275745465),
						"display_name": "Pro Access",
						"lookup_key":   "pro_access",
						"object":       "entitlement",
						"project_id":   "projc09fd2a0",
						"state":        "active",
					},
				},
			},
		},
		{
			SourceTable:      "offerings",
			DestTable:        "main.revenuecat_offerings",
			KeyColumn:        "id",
			ExpectedRowCount: 1,
			ExpectedSchema: []schema.Column{
				{Name: "created_at", DataType: schema.TypeInt64},
				{Name: "display_name", DataType: schema.TypeString},
				{Name: "id", DataType: schema.TypeString},
				{Name: "is_current", DataType: schema.TypeBoolean},
				{Name: "lookup_key", DataType: schema.TypeString},
				{Name: "object", DataType: schema.TypeString},
				{Name: "project_id", DataType: schema.TypeString},
				{Name: "state", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "ofrngad403fd047",
					Fields: map[string]any{
						"created_at":   int64(1774275765443),
						"display_name": "Default Offering",
						"is_current":   true,
						"lookup_key":   "default",
						"object":       "offering",
						"project_id":   "projc09fd2a0",
						"state":        "active",
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
