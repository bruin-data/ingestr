//go:build integration

package salesforce_test

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

func TestSalesforcePipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	username := os.Getenv("SALESFORCE_USERNAME")
	password := os.Getenv("SALESFORCE_PASSWORD")
	token := os.Getenv("SALESFORCE_TOKEN")
	domain := os.Getenv("SALESFORCE_DOMAIN")
	if username == "" || password == "" || token == "" || domain == "" {
		t.Skip("Set SALESFORCE_USERNAME, SALESFORCE_PASSWORD, SALESFORCE_TOKEN, and SALESFORCE_DOMAIN to run Salesforce integration tests")
	}

	ctx := context.Background()
	sourceURI := fmt.Sprintf("salesforce://localhost?username=%s&password=%s&token=%s&domain=%s", username, password, token, domain)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("salesforce_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	expectations := []testutil.TableExpectation{
		{
			SourceTable:    "account",
			DestTable:      "main.salesforce_account",
			KeyColumn:      "id",
			ExcludeColumns: []string{"lastmodifieddate", "lastreferenceddate", "lastvieweddate", "systemmodstamp"},
			ExpectedSchema: []schema.Column{
				{Name: "createdbyid", DataType: schema.TypeString},
				{Name: "createddate", DataType: schema.TypeTimestampTZ},
				{Name: "description", DataType: schema.TypeString},
				{Name: "id", DataType: schema.TypeString},
				{Name: "isbuyer", DataType: schema.TypeBoolean},
				{Name: "iscustomerportal", DataType: schema.TypeBoolean},
				{Name: "isdeleted", DataType: schema.TypeBoolean},
				{Name: "lastmodifiedbyid", DataType: schema.TypeString},
				{Name: "lastmodifieddate", DataType: schema.TypeTimestampTZ},
				{Name: "lastreferenceddate", DataType: schema.TypeTimestampTZ},
				{Name: "lastvieweddate", DataType: schema.TypeTimestampTZ},
				{Name: "name", DataType: schema.TypeString},
				{Name: "ownerid", DataType: schema.TypeString},
				{Name: "systemmodstamp", DataType: schema.TypeTimestampTZ},
				{Name: "type", DataType: schema.TypeString},
				{Name: "website", DataType: schema.TypeString},
			},
			ExpectedRowCount: 3,
			Rows: []testutil.ExpectedRow{
				{
					ID: "001d300000E9fv9AAB",
					Fields: map[string]any{
						"createdbyid":        "005d3000002JNarAAG",
						"createddate":        time.Date(2026, 2, 18, 5, 20, 24, 0, time.UTC),
						"description":        "Cloud infrastructure provider",
						"isbuyer":            false,
						"iscustomerportal":   false,
						"isdeleted":          false,
						"lastmodifiedbyid":   "005d3000002JNarAAG",
						"lastmodifieddate":   time.Date(2026, 2, 18, 5, 20, 24, 0, time.UTC),
						"lastreferenceddate": time.Date(2026, 2, 18, 5, 20, 46, 0, time.UTC),
						"lastvieweddate":     time.Date(2026, 2, 18, 5, 20, 46, 0, time.UTC),
						"name":               "Atlas Cloud Solutions",
						"ownerid":            "005d3000002JNarAAG",
						"systemmodstamp":     time.Date(2026, 2, 18, 5, 20, 24, 0, time.UTC),
						"type":               "Customer",
						"website":            "atlascloud.io",
					},
				},
				{
					ID: "001d300000E9kBdAAJ",
					Fields: map[string]any{
						"createdbyid":        "005d3000002JNarAAG",
						"createddate":        time.Date(2026, 2, 18, 5, 16, 31, 0, time.UTC),
						"description":        "Technology company for data integration",
						"isbuyer":            false,
						"iscustomerportal":   false,
						"isdeleted":          false,
						"lastmodifiedbyid":   "005d3000002JNarAAG",
						"lastmodifieddate":   time.Date(2026, 2, 18, 5, 16, 31, 0, time.UTC),
						"lastreferenceddate": time.Date(2026, 2, 18, 5, 58, 44, 0, time.UTC),
						"lastvieweddate":     time.Date(2026, 2, 18, 5, 58, 44, 0, time.UTC),
						"name":               "Bruin Data Inc",
						"ownerid":            "005d3000002JNarAAG",
						"systemmodstamp":     time.Date(2026, 2, 18, 5, 16, 31, 0, time.UTC),
						"type":               "Customer",
						"website":            "bruin.com",
					},
				},
				{
					ID: "001d300000E9ckWAAR",
					Fields: map[string]any{
						"createdbyid":        "005d3000002JNarAAG",
						"createddate":        time.Date(2026, 2, 18, 5, 18, 57, 0, time.UTC),
						"description":        "Finance company for testing",
						"isbuyer":            false,
						"iscustomerportal":   false,
						"isdeleted":          false,
						"lastmodifiedbyid":   "005d3000002JNarAAG",
						"lastmodifieddate":   time.Date(2026, 2, 18, 5, 18, 57, 0, time.UTC),
						"lastreferenceddate": time.Date(2026, 2, 18, 5, 19, 25, 0, time.UTC),
						"lastvieweddate":     time.Date(2026, 2, 18, 5, 19, 25, 0, time.UTC),
						"name":               "Gong Test Corp",
						"ownerid":            "005d3000002JNarAAG",
						"systemmodstamp":     time.Date(2026, 2, 18, 5, 18, 57, 0, time.UTC),
						"type":               "Prospect",
						"website":            "gongtest.com",
					},
				},
			},
		},
		{
			SourceTable:    "contact",
			DestTable:      "main.salesforce_contact",
			KeyColumn:      "id",
			ExcludeColumns: []string{"lastmodifieddate", "lastreferenceddate", "lastvieweddate", "systemmodstamp"},
			ExpectedSchema: []schema.Column{
				{Name: "accountid", DataType: schema.TypeString},
				{Name: "createdbyid", DataType: schema.TypeString},
				{Name: "createddate", DataType: schema.TypeTimestampTZ},
				{Name: "email", DataType: schema.TypeString},
				{Name: "firstname", DataType: schema.TypeString},
				{Name: "hasoptedoutofemail", DataType: schema.TypeBoolean},
				{Name: "id", DataType: schema.TypeString},
				{Name: "isdeleted", DataType: schema.TypeBoolean},
				{Name: "isemailbounced", DataType: schema.TypeBoolean},
				{Name: "lastmodifiedbyid", DataType: schema.TypeString},
				{Name: "lastmodifieddate", DataType: schema.TypeTimestampTZ},
				{Name: "lastname", DataType: schema.TypeString},
				{Name: "lastreferenceddate", DataType: schema.TypeTimestampTZ},
				{Name: "lastvieweddate", DataType: schema.TypeTimestampTZ},
				{Name: "name", DataType: schema.TypeString},
				{Name: "ownerid", DataType: schema.TypeString},
				{Name: "phone", DataType: schema.TypeString},
				{Name: "salutation", DataType: schema.TypeString},
				{Name: "systemmodstamp", DataType: schema.TypeTimestampTZ},
				{Name: "title", DataType: schema.TypeString},
			},
			ExpectedRowCount: 4,
			Rows: []testutil.ExpectedRow{
				{
					ID: "003d3000005iPntAAE",
					Fields: map[string]any{
						"accountid":          "001d300000E9fv9AAB",
						"createdbyid":        "005d3000002JNarAAG",
						"createddate":        time.Date(2026, 2, 18, 5, 20, 46, 0, time.UTC),
						"email":              "ali@techstart.com",
						"firstname":          "Sarah",
						"hasoptedoutofemail": false,
						"isdeleted":          false,
						"isemailbounced":     false,
						"lastmodifiedbyid":   "005d3000002JNarAAG",
						"lastmodifieddate":   time.Date(2026, 2, 18, 5, 20, 46, 0, time.UTC),
						"lastname":           "Johnson",
						"lastreferenceddate": time.Date(2026, 2, 18, 5, 20, 47, 0, time.UTC),
						"lastvieweddate":     time.Date(2026, 2, 18, 5, 20, 47, 0, time.UTC),
						"name":               "Sarah Johnson",
						"ownerid":            "005d3000002JNarAAG",
						"phone":              "555-0103",
						"salutation":         "Mrs.",
						"systemmodstamp":     time.Date(2026, 2, 18, 5, 20, 46, 0, time.UTC),
						"title":              "Engineer",
					},
				},
				{
					ID: "003d3000005iUPKAA2",
					Fields: map[string]any{
						"accountid":          "001d300000E9ckWAAR",
						"createdbyid":        "005d3000002JNarAAG",
						"createddate":        time.Date(2026, 2, 18, 5, 19, 25, 0, time.UTC),
						"email":              "jane@gongtest.com",
						"firstname":          "Jane",
						"hasoptedoutofemail": false,
						"isdeleted":          false,
						"isemailbounced":     false,
						"lastmodifiedbyid":   "005d3000002JNarAAG",
						"lastmodifieddate":   time.Date(2026, 2, 18, 5, 19, 25, 0, time.UTC),
						"lastname":           "Smith",
						"lastreferenceddate": time.Date(2026, 2, 18, 5, 19, 26, 0, time.UTC),
						"lastvieweddate":     time.Date(2026, 2, 18, 5, 19, 26, 0, time.UTC),
						"name":               "Jane Smith",
						"ownerid":            "005d3000002JNarAAG",
						"phone":              "555-0102",
						"salutation":         "Mr.",
						"systemmodstamp":     time.Date(2026, 2, 18, 5, 19, 25, 0, time.UTC),
						"title":              "VP Sales",
					},
				},
				{
					ID: "003d3000005iWu9AAE",
					Fields: map[string]any{
						"accountid":          "001d300000E9kBdAAJ",
						"createdbyid":        "005d3000002JNarAAG",
						"createddate":        time.Date(2026, 2, 18, 5, 17, 52, 0, time.UTC),
						"email":              "john@bruin.com",
						"firstname":          "John",
						"hasoptedoutofemail": false,
						"isdeleted":          false,
						"isemailbounced":     false,
						"lastmodifiedbyid":   "005d3000002JNarAAG",
						"lastmodifieddate":   time.Date(2026, 2, 18, 5, 17, 52, 0, time.UTC),
						"lastname":           "Doe",
						"lastreferenceddate": time.Date(2026, 2, 18, 5, 17, 53, 0, time.UTC),
						"lastvieweddate":     time.Date(2026, 2, 18, 5, 17, 53, 0, time.UTC),
						"name":               "John Doe",
						"ownerid":            "005d3000002JNarAAG",
						"phone":              nil,
						"salutation":         "Mr.",
						"systemmodstamp":     time.Date(2026, 2, 18, 5, 17, 52, 0, time.UTC),
						"title":              "CTO",
					},
				},
				{
					ID: "003d3000005iX3pAAE",
					Fields: map[string]any{
						"accountid":          "001d300000E9kBdAAJ",
						"createdbyid":        "005d3000002JNarAAG",
						"createddate":        time.Date(2026, 2, 18, 5, 21, 31, 0, time.UTC),
						"email":              "mike@example.com",
						"firstname":          "Mike",
						"hasoptedoutofemail": false,
						"isdeleted":          false,
						"isemailbounced":     false,
						"lastmodifiedbyid":   "005d3000002JNarAAG",
						"lastmodifieddate":   time.Date(2026, 2, 18, 5, 21, 31, 0, time.UTC),
						"lastname":           "Chen",
						"lastreferenceddate": time.Date(2026, 2, 18, 5, 21, 32, 0, time.UTC),
						"lastvieweddate":     time.Date(2026, 2, 18, 5, 21, 32, 0, time.UTC),
						"name":               "Mike Chen",
						"ownerid":            "005d3000002JNarAAG",
						"phone":              "555-0105",
						"salutation":         "Mr.",
						"systemmodstamp":     time.Date(2026, 2, 18, 5, 21, 31, 0, time.UTC),
						"title":              "Data Analyst",
					},
				},
			},
		},
		{
			SourceTable:    "lead",
			DestTable:      "main.salesforce_lead",
			KeyColumn:      "id",
			ExcludeColumns: []string{"lastmodifieddate", "lastreferenceddate", "lastvieweddate", "systemmodstamp"},
			ExpectedSchema: []schema.Column{
				{Name: "company", DataType: schema.TypeString},
				{Name: "createdbyid", DataType: schema.TypeString},
				{Name: "createddate", DataType: schema.TypeTimestampTZ},
				{Name: "email", DataType: schema.TypeString},
				{Name: "firstname", DataType: schema.TypeString},
				{Name: "hasoptedoutofemail", DataType: schema.TypeBoolean},
				{Name: "id", DataType: schema.TypeString},
				{Name: "isconverted", DataType: schema.TypeBoolean},
				{Name: "isdeleted", DataType: schema.TypeBoolean},
				{Name: "isunreadbyowner", DataType: schema.TypeBoolean},
				{Name: "lastmodifiedbyid", DataType: schema.TypeString},
				{Name: "lastmodifieddate", DataType: schema.TypeTimestampTZ},
				{Name: "lastname", DataType: schema.TypeString},
				{Name: "lastreferenceddate", DataType: schema.TypeTimestampTZ},
				{Name: "lastvieweddate", DataType: schema.TypeTimestampTZ},
				{Name: "name", DataType: schema.TypeString},
				{Name: "ownerid", DataType: schema.TypeString},
				{Name: "salutation", DataType: schema.TypeString},
				{Name: "status", DataType: schema.TypeString},
				{Name: "systemmodstamp", DataType: schema.TypeTimestampTZ},
				{Name: "title", DataType: schema.TypeString},
			},
			ExpectedRowCount: 3,
			Rows: []testutil.ExpectedRow{
				{
					ID: "00Qd3000007TOqvEAG",
					Fields: map[string]any{
						"company":            "DataFlow Labs",
						"createdbyid":        "005d3000002JNarAAG",
						"createddate":        time.Date(2026, 2, 18, 5, 23, 25, 0, time.UTC),
						"email":              "emma@dataflow.io",
						"firstname":          "Emma",
						"hasoptedoutofemail": false,
						"isconverted":        false,
						"isdeleted":          false,
						"isunreadbyowner":    false,
						"lastmodifiedbyid":   "005d3000002JNarAAG",
						"lastmodifieddate":   time.Date(2026, 2, 18, 5, 23, 26, 0, time.UTC),
						"lastname":           "Wilson",
						"lastreferenceddate": time.Date(2026, 2, 18, 5, 24, 57, 0, time.UTC),
						"lastvieweddate":     time.Date(2026, 2, 18, 5, 24, 57, 0, time.UTC),
						"name":               "Emma Wilson",
						"ownerid":            "005d3000002JNarAAG",
						"salutation":         "Mrs.",
						"status":             "Contacted",
						"systemmodstamp":     time.Date(2026, 2, 18, 5, 23, 26, 0, time.UTC),
						"title":              "VP Engineering",
					},
				},
				{
					ID: "00Qd3000007TOvlEAG",
					Fields: map[string]any{
						"company":            "Skyline Ventures",
						"createdbyid":        "005d3000002JNarAAG",
						"createddate":        time.Date(2026, 2, 18, 5, 24, 9, 0, time.UTC),
						"email":              "carlos@skylinevc.com",
						"firstname":          "Carlos",
						"hasoptedoutofemail": false,
						"isconverted":        false,
						"isdeleted":          false,
						"isunreadbyowner":    false,
						"lastmodifiedbyid":   "005d3000002JNarAAG",
						"lastmodifieddate":   time.Date(2026, 2, 18, 7, 15, 41, 0, time.UTC),
						"lastname":           "Rivera",
						"lastreferenceddate": time.Date(2026, 2, 18, 7, 15, 41, 0, time.UTC),
						"lastvieweddate":     time.Date(2026, 2, 18, 7, 15, 41, 0, time.UTC),
						"name":               "Carlos Rivera",
						"ownerid":            "005d3000002JNarAAG",
						"salutation":         "Mr.",
						"status":             "Qualified",
						"systemmodstamp":     time.Date(2026, 2, 18, 7, 15, 41, 0, time.UTC),
						"title":              "Managing Director",
					},
				},
				{
					ID: "00Qd3000007TOxNEAW",
					Fields: map[string]any{
						"company":            "NexGen Software",
						"createdbyid":        "005d3000002JNarAAG",
						"createddate":        time.Date(2026, 2, 18, 5, 24, 52, 0, time.UTC),
						"email":              "priya@nexgen.dev",
						"firstname":          "Priya",
						"hasoptedoutofemail": false,
						"isconverted":        false,
						"isdeleted":          false,
						"isunreadbyowner":    false,
						"lastmodifiedbyid":   "005d3000002JNarAAG",
						"lastmodifieddate":   time.Date(2026, 2, 18, 5, 24, 52, 0, time.UTC),
						"lastname":           "Patel",
						"lastreferenceddate": time.Date(2026, 2, 18, 5, 24, 57, 0, time.UTC),
						"lastvieweddate":     time.Date(2026, 2, 18, 5, 24, 57, 0, time.UTC),
						"name":               "Priya Patel",
						"ownerid":            "005d3000002JNarAAG",
						"salutation":         "Mrs.",
						"status":             "Qualified",
						"systemmodstamp":     time.Date(2026, 2, 18, 5, 24, 52, 0, time.UTC),
						"title":              "Head of Product",
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
