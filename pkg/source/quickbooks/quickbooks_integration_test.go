//go:build integration

package quickbooks_test

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

func TestQuickBooksPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	companyID := os.Getenv("QUICKBOOKS_COMPANY_ID")
	clientID := os.Getenv("QUICKBOOKS_CLIENT_ID")
	clientSecret := os.Getenv("QUICKBOOKS_CLIENT_SECRET")
	refreshToken := os.Getenv("QUICKBOOKS_REFRESH_TOKEN")
	env := os.Getenv("QUICKBOOKS_ENVIRONMENT")

	if companyID == "" || clientID == "" || clientSecret == "" || refreshToken == "" {
		t.Skip("Set QUICKBOOKS_COMPANY_ID, QUICKBOOKS_CLIENT_ID, QUICKBOOKS_CLIENT_SECRET, QUICKBOOKS_REFRESH_TOKEN to run QuickBooks integration tests")
	}

	if env == "" {
		env = "sandbox"
	}

	ctx := context.Background()
	sourceURI := fmt.Sprintf("quickbooks://?company_id=%s&client_id=%s&client_secret=%s&refresh_token=%s&environment=%s",
		companyID, clientID, clientSecret, refreshToken, env)

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("quickbooks_%d.duckdb", time.Now().UnixNano()))
	destURI := fmt.Sprintf("duckdb:///%s", dbPath)

	expectations := []testutil.TableExpectation{
		{
			SourceTable:      "customers",
			DestTable:        "main.quickbooks_customers",
			KeyColumn:        "id",
			ExpectedRowCount: 29,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "lastupdatedtime", DataType: schema.TypeTimestampTZ},
				{Name: "active", DataType: schema.TypeBoolean},
				{Name: "balance", DataType: schema.TypeFloat64},
				{Name: "balancewithjobs", DataType: schema.TypeFloat64},
				{Name: "billwithparent", DataType: schema.TypeBoolean},
				{Name: "cliententityid", DataType: schema.TypeString},
				{Name: "companyname", DataType: schema.TypeString},
				{Name: "displayname", DataType: schema.TypeString},
				{Name: "familyname", DataType: schema.TypeString},
				{Name: "fullyqualifiedname", DataType: schema.TypeString},
				{Name: "givenname", DataType: schema.TypeString},
				{Name: "isproject", DataType: schema.TypeBoolean},
				{Name: "job", DataType: schema.TypeBoolean},
				{Name: "level", DataType: schema.TypeInt64},
				{Name: "middlename", DataType: schema.TypeString},
				{Name: "preferreddeliverymethod", DataType: schema.TypeString},
				{Name: "printoncheckname", DataType: schema.TypeString},
				{Name: "synctoken", DataType: schema.TypeString},
				{Name: "taxable", DataType: schema.TypeBoolean},
				{Name: "v4idpseudonym", DataType: schema.TypeString},
				{Name: "domain", DataType: schema.TypeString},
				{Name: "sparse", DataType: schema.TypeBoolean},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "1",
					Fields: map[string]any{
						"DisplayName":             "Amy's Bird Sanctuary",
						"Active":                  true,
						"Balance":                 239.0,
						"BalanceWithJobs":         239.0,
						"BillWithParent":          false,
						"ClientEntityId":          "0",
						"CompanyName":             "Amy's Bird Sanctuary",
						"FamilyName":              "Lauterbach",
						"FullyQualifiedName":      "Amy's Bird Sanctuary",
						"GivenName":               "Amy",
						"IsProject":               false,
						"Job":                     false,
						"PreferredDeliveryMethod": "Print",
						"PrintOnCheckName":        "Amy's Bird Sanctuary",
						"SyncToken":               "0",
						"Taxable":                 true,
						"V4IDPseudonym":           "002098f5bb3f61761f4f45959156010af099a7",
					},
				},
			},
		},
		{
			SourceTable:      "invoices",
			DestTable:        "main.quickbooks_invoices",
			KeyColumn:        "id",
			ExpectedRowCount: 31,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "lastupdatedtime", DataType: schema.TypeTimestampTZ},
				{Name: "allowipnpayment", DataType: schema.TypeBoolean},
				{Name: "allowonlineachpayment", DataType: schema.TypeBoolean},
				{Name: "allowonlinecreditcardpayment", DataType: schema.TypeBoolean},
				{Name: "allowonlinepayment", DataType: schema.TypeBoolean},
				{Name: "applytaxafterdiscount", DataType: schema.TypeBoolean},
				{Name: "balance", DataType: schema.TypeFloat64},
				{Name: "docnumber", DataType: schema.TypeString},
				{Name: "duedate", DataType: schema.TypeDate},
				{Name: "emailstatus", DataType: schema.TypeString},
				{Name: "freeformaddress", DataType: schema.TypeBoolean},
				{Name: "printstatus", DataType: schema.TypeString},
				{Name: "privatenote", DataType: schema.TypeString},
				{Name: "synctoken", DataType: schema.TypeString},
				{Name: "totalamt", DataType: schema.TypeFloat64},
				{Name: "txndate", DataType: schema.TypeDate},
				{Name: "domain", DataType: schema.TypeString},
				{Name: "sparse", DataType: schema.TypeBoolean},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "130",
					Fields: map[string]any{
						"DocNumber":                    "1037",
						"TotalAmt":                     362.07,
						"Balance":                      362.07,
						"AllowIPNPayment":              false,
						"AllowOnlineACHPayment":        false,
						"AllowOnlineCreditCardPayment": false,
						"AllowOnlinePayment":           false,
						"ApplyTaxAfterDiscount":        false,
						"EmailStatus":                  "NotSet",
						"FreeFormAddress":              true,
						"PrintStatus":                  "NeedToPrint",
						"SyncToken":                    "0",
					},
				},
			},
		},
		{
			SourceTable:      "accounts",
			DestTable:        "main.quickbooks_accounts",
			KeyColumn:        "id",
			ExpectedRowCount: 89,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "lastupdatedtime", DataType: schema.TypeTimestampTZ},
				{Name: "accountsubtype", DataType: schema.TypeString},
				{Name: "accounttype", DataType: schema.TypeString},
				{Name: "active", DataType: schema.TypeBoolean},
				{Name: "classification", DataType: schema.TypeString},
				{Name: "currentbalance", DataType: schema.TypeFloat64},
				{Name: "currentbalancewithsubaccounts", DataType: schema.TypeFloat64},
				{Name: "fullyqualifiedname", DataType: schema.TypeString},
				{Name: "name", DataType: schema.TypeString},
				{Name: "subaccount", DataType: schema.TypeBoolean},
				{Name: "synctoken", DataType: schema.TypeString},
				{Name: "domain", DataType: schema.TypeString},
				{Name: "sparse", DataType: schema.TypeBoolean},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "1",
					Fields: map[string]any{
						"Name":                          "Services",
						"AccountType":                   "Income",
						"AccountSubType":                "ServiceFeeIncome",
						"Active":                        true,
						"Classification":                "Revenue",
						"CurrentBalance":                0.0,
						"CurrentBalanceWithSubAccounts": 0.0,
						"FullyQualifiedName":            "Services",
						"SubAccount":                    false,
						"SyncToken":                     "0",
					},
				},
			},
		},
		{
			SourceTable:      "vendors",
			DestTable:        "main.quickbooks_vendors",
			KeyColumn:        "id",
			ExpectedRowCount: 26,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "lastupdatedtime", DataType: schema.TypeTimestampTZ},
				{Name: "acctnum", DataType: schema.TypeString},
				{Name: "active", DataType: schema.TypeBoolean},
				{Name: "balance", DataType: schema.TypeFloat64},
				{Name: "billrate", DataType: schema.TypeInt64},
				{Name: "companyname", DataType: schema.TypeString},
				{Name: "displayname", DataType: schema.TypeString},
				{Name: "familyname", DataType: schema.TypeString},
				{Name: "givenname", DataType: schema.TypeString},
				{Name: "middlename", DataType: schema.TypeString},
				{Name: "printoncheckname", DataType: schema.TypeString},
				{Name: "suffix", DataType: schema.TypeString},
				{Name: "synctoken", DataType: schema.TypeString},
				{Name: "title", DataType: schema.TypeString},
				{Name: "v4idpseudonym", DataType: schema.TypeString},
				{Name: "vendor1099", DataType: schema.TypeBoolean},
				{Name: "domain", DataType: schema.TypeString},
				{Name: "sparse", DataType: schema.TypeBoolean},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "33",
					Fields: map[string]any{
						"DisplayName":      "Chin's Gas and Oil",
						"Active":           true,
						"Balance":          0.0,
						"CompanyName":      "Chin's Gas and Oil",
						"PrintOnCheckName": "Chin's Gas and Oil",
						"SyncToken":        "0",
						"V4IDPseudonym":    "002098ea003c7e13e84fda90d9769b9294cb45",
						"Vendor1099":       false,
					},
				},
			},
		},
		{
			SourceTable:      "payments",
			DestTable:        "main.quickbooks_payments",
			KeyColumn:        "id",
			ExpectedRowCount: 16,
			ExpectedSchema: []schema.Column{
				{Name: "id", DataType: schema.TypeString},
				{Name: "lastupdatedtime", DataType: schema.TypeTimestampTZ},
				{Name: "paymentrefnum", DataType: schema.TypeString},
				{Name: "privatenote", DataType: schema.TypeString},
				{Name: "processpayment", DataType: schema.TypeBoolean},
				{Name: "synctoken", DataType: schema.TypeString},
				{Name: "totalamt", DataType: schema.TypeFloat64},
				{Name: "txndate", DataType: schema.TypeDate},
				{Name: "unappliedamt", DataType: schema.TypeInt64},
				{Name: "domain", DataType: schema.TypeString},
				{Name: "sparse", DataType: schema.TypeBoolean},
			},
			Rows: []testutil.ExpectedRow{
				{
					ID: "74",
					Fields: map[string]any{
						"TotalAmt":       0.0,
						"ProcessPayment": false,
						"SyncToken":      "1",
						"UnappliedAmt":   int64(0),
						"PrivateNote":    "Created by QB Online to link credits to charges.",
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
