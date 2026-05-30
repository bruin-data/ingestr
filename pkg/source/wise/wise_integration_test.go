//go:build integration

package wise_test

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

func TestWisePipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	apiKey := os.Getenv("WISE_API_KEY")
	if apiKey == "" {
		t.Skip("Set WISE_API_KEY to run Wise integration tests")
	}

	ctx := context.Background()
	sourceURI := fmt.Sprintf("wise://?api_key=%s", apiKey)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("wise_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	expectations := []testutil.TableExpectation{
		{
			SourceTable:      "profiles",
			DestTable:        "main.wise_profiles",
			KeyColumn:        "id",
			ExpectedRowCount: 2,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "type", DataType: schema.TypeString},
				{Name: "firstname", DataType: schema.TypeString},
				{Name: "lastname", DataType: schema.TypeString},
				{Name: "fullname", DataType: schema.TypeString},
				{Name: "email", DataType: schema.TypeString},
				{Name: "currentstate", DataType: schema.TypeString},
				{Name: "partner", DataType: schema.TypeBoolean},
				{Name: "obfuscated", DataType: schema.TypeBoolean},
				{Name: "contractingwithwise", DataType: schema.TypeBoolean},
				{Name: "dataobfuscated", DataType: schema.TypeBoolean},
				{Name: "jointprofile", DataType: schema.TypeBoolean},
				{Name: "partnercustomer", DataType: schema.TypeBoolean},
				{Name: "dateofbirth", DataType: schema.TypeDate},
				{Name: "createdat", DataType: schema.TypeTimestampTZ},
				{Name: "updatedat", DataType: schema.TypeTimestampTZ},
				{Name: "phonenumber", DataType: schema.TypeString},
				{Name: "publicid", DataType: schema.TypeString},
				{Name: "creatorclientid", DataType: schema.TypeString},
				{Name: "profilerole", DataType: schema.TypeString},
				{Name: "version", DataType: schema.TypeInt64},
				{Name: "userid", DataType: schema.TypeInt64},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "86096403",
					Fields: map[string]any{
						"firstName":    "Gong",
						"lastName":     "Test",
						"fullName":     "Gong Test",
						"email":        "vendor_accounts@getbruin.com",
						"type":         "PERSONAL",
						"currentState": "VISIBLE",
						"partner":      false,
						"obfuscated":   false,
					},
				},
			},
		},
		{
			SourceTable:      "transfers",
			DestTable:        "main.wise_transfers",
			KeyColumn:        "id",
			ExpectedRowCount: 2,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeInt64},
				{Name: "status", DataType: schema.TypeString},
				{Name: "reference", DataType: schema.TypeString},
				{Name: "sourcecurrency", DataType: schema.TypeString},
				{Name: "sourcevalue", DataType: schema.TypeFloat64},
				{Name: "targetcurrency", DataType: schema.TypeString},
				{Name: "targetvalue", DataType: schema.TypeFloat64},
				{Name: "rate", DataType: schema.TypeFloat64},
				{Name: "business", DataType: schema.TypeInt64},
				{Name: "user", DataType: schema.TypeInt64},
				{Name: "targetaccount", DataType: schema.TypeInt64},
				{Name: "hasactiveissues", DataType: schema.TypeBoolean},
				{Name: "created", DataType: schema.TypeTimestampTZ},
				{Name: "customertransactionid", DataType: schema.TypeString},
				{Name: "quoteuuid", DataType: schema.TypeString},
				{Name: "payinsessionid", DataType: schema.TypeString},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "2036889089",
					Fields: map[string]any{
						"status":         "incoming_payment_waiting",
						"reference":      "Test transfer 1",
						"sourceCurrency": "EUR",
						"targetCurrency": "GBP",
						"business":       int64(86098631),
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

	// Merge idempotency: run again and verify no duplicates
	for _, exp := range expectations {
		t.Run(exp.SourceTable+"_merge_idempotent", func(t *testing.T) {
			testutil.RunPipeline(t, ctx, sourceURI, destURI, exp)
			testutil.Check(t, destURI, exp)
		})
	}
}
