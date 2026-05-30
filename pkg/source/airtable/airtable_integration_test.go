//go:build integration

package airtable_test

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

func TestAirtablePipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	accessToken := os.Getenv("AIRTABLE_ACCESS_TOKEN")
	baseID := os.Getenv("AIRTABLE_BASE_ID")
	if accessToken == "" || baseID == "" {
		t.Skip("Set AIRTABLE_ACCESS_TOKEN and AIRTABLE_BASE_ID to run Airtable integration tests")
	}

	ctx := context.Background()
	sourceURI := fmt.Sprintf("airtable://?access_token=%s", accessToken)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("airtable_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	expectations := []testutil.TableExpectation{
		{
			SourceTable:      baseID + "/Table 1",
			DestTable:        "main.airtable_table_1",
			KeyColumn:        "id",
			ExpectedRowCount: 3,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "createdtime", DataType: schema.TypeTimestampTZ},
				{Name: "fields__name", DataType: schema.TypeString},
				{Name: "fields__notes", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "recAC8CMuJNfN4fpk",
					Fields: map[string]any{
						"fields__name":  "Alice Johnson",
						"fields__notes": "Engineering lead",
					},
				},
				{
					ID: "recM8e1jCFJJqOXu2",
					Fields: map[string]any{
						"fields__name":  "Bob Smith",
						"fields__notes": "Product manager",
					},
				},
				{
					ID: "recdfDnfEVAITJoPk",
					Fields: map[string]any{
						"fields__name":  "Charlie Brown",
						"fields__notes": "Designer",
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
