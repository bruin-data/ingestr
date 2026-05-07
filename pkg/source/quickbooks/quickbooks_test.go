package quickbooks

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/bruin-data/gong/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name      string
		uri       string
		want      quickbooksCredentials
		wantErr   bool
		errSubstr string
	}{
		{
			name: "valid URI with refresh token",
			uri:  "quickbooks://?company_id=123&client_id=cid&client_secret=csec&refresh_token=rt123",
			want: quickbooksCredentials{
				companyID:    "123",
				clientID:     "cid",
				clientSecret: "csec",
				refreshToken: "rt123",
				environment:  "production",
			},
		},
		{
			name: "valid URI with access token",
			uri:  "quickbooks://?company_id=123&client_id=cid&client_secret=csec&access_token=at123",
			want: quickbooksCredentials{
				companyID:    "123",
				clientID:     "cid",
				clientSecret: "csec",
				accessToken:  "at123",
				environment:  "production",
			},
		},
		{
			name: "sandbox environment",
			uri:  "quickbooks://?company_id=123&client_id=cid&client_secret=csec&refresh_token=rt&environment=sandbox",
			want: quickbooksCredentials{
				companyID:    "123",
				clientID:     "cid",
				clientSecret: "csec",
				refreshToken: "rt",
				environment:  "sandbox",
			},
		},
		{
			name: "with minor version",
			uri:  "quickbooks://?company_id=123&client_id=cid&client_secret=csec&refresh_token=rt&minor_version=65",
			want: quickbooksCredentials{
				companyID:    "123",
				clientID:     "cid",
				clientSecret: "csec",
				refreshToken: "rt",
				environment:  "production",
				minorVersion: "65",
			},
		},
		{
			name:      "missing company_id",
			uri:       "quickbooks://?client_id=cid&client_secret=csec&refresh_token=rt",
			wantErr:   true,
			errSubstr: "company_id is required",
		},
		{
			name:      "missing client_id",
			uri:       "quickbooks://?company_id=123&client_secret=csec&refresh_token=rt",
			wantErr:   true,
			errSubstr: "client_id is required",
		},
		{
			name:      "missing client_secret",
			uri:       "quickbooks://?company_id=123&client_id=cid&refresh_token=rt",
			wantErr:   true,
			errSubstr: "client_secret is required",
		},
		{
			name:      "missing both tokens",
			uri:       "quickbooks://?company_id=123&client_id=cid&client_secret=csec",
			wantErr:   true,
			errSubstr: "either refresh_token or access_token is required",
		},
		{
			name:      "wrong scheme",
			uri:       "http://?company_id=123&client_id=cid&client_secret=csec&refresh_token=rt",
			wantErr:   true,
			errSubstr: "must start with quickbooks://",
		},
		{
			name:      "invalid environment",
			uri:       "quickbooks://?company_id=123&client_id=cid&client_secret=csec&refresh_token=rt&environment=staging",
			wantErr:   true,
			errSubstr: "environment must be 'production' or 'sandbox'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseURI(tt.uri)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errSubstr != "" {
					assert.Contains(t, err.Error(), tt.errSubstr)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want.companyID, got.companyID)
			assert.Equal(t, tt.want.clientID, got.clientID)
			assert.Equal(t, tt.want.clientSecret, got.clientSecret)
			assert.Equal(t, tt.want.refreshToken, got.refreshToken)
			assert.Equal(t, tt.want.accessToken, got.accessToken)
			assert.Equal(t, tt.want.environment, got.environment)
			assert.Equal(t, tt.want.minorVersion, got.minorVersion)
		})
	}
}

func TestIsValidTable(t *testing.T) {
	for _, table := range supportedTables {
		assert.True(t, isValidTable(table), "expected %s to be valid", table)
	}

	assert.False(t, isValidTable("nonexistent"))
	assert.False(t, isValidTable(""))
	assert.False(t, isValidTable("Customers"))
	assert.False(t, isValidTable("INVOICES"))
}

func TestBuildQuery(t *testing.T) {
	t.Run("no filters", func(t *testing.T) {
		q := buildQuery("Customer", 1, source.ReadOptions{})
		assert.Equal(t, "SELECT * FROM Customer ORDERBY MetaData.LastUpdatedTime ASC STARTPOSITION 1 MAXRESULTS 1000", q)
	})

	t.Run("with interval start", func(t *testing.T) {
		start := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
		q := buildQuery("Invoice", 1, source.ReadOptions{IntervalStart: &start})
		assert.Contains(t, q, "WHERE MetaData.LastUpdatedTime >= '2025-06-01T00:00:00+00:00'")
		assert.Contains(t, q, "ORDERBY MetaData.LastUpdatedTime ASC")
	})

	t.Run("with both intervals", func(t *testing.T) {
		start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC)
		q := buildQuery("Account", 1, source.ReadOptions{IntervalStart: &start, IntervalEnd: &end})
		assert.Contains(t, q, "MetaData.LastUpdatedTime >= '2025-01-01T00:00:00+00:00'")
		assert.Contains(t, q, "MetaData.LastUpdatedTime < '2025-12-31T23:59:59+00:00'")
		assert.Contains(t, q, " AND ")
	})

	t.Run("custom start position", func(t *testing.T) {
		q := buildQuery("Vendor", 1001, source.ReadOptions{})
		assert.Contains(t, q, "STARTPOSITION 1001 MAXRESULTS 1000")
	})
}

func TestNormalizeItem(t *testing.T) {
	t.Run("extracts lastupdatedtime and renames Id", func(t *testing.T) {
		item := map[string]any{
			"Id":   "42",
			"Name": "Test Customer",
			"MetaData": map[string]any{
				"CreateTime":      "2025-01-01T00:00:00-08:00",
				"LastUpdatedTime": "2025-06-15T10:30:00-08:00",
			},
		}

		result := normalizeItem(item)
		assert.Equal(t, "42", result["id"])
		assert.Equal(t, "2025-06-15T10:30:00-08:00", result["lastupdatedtime"])
		assert.Nil(t, result["Id"])
		assert.NotNil(t, result["MetaData"])
	})

	t.Run("handles missing MetaData", func(t *testing.T) {
		item := map[string]any{
			"Id":   "1",
			"Name": "No MetaData",
		}

		result := normalizeItem(item)
		assert.Equal(t, "1", result["id"])
		assert.Nil(t, result["lastupdatedtime"])
	})

	t.Run("handles missing Id", func(t *testing.T) {
		item := map[string]any{
			"Name": "No Id",
			"MetaData": map[string]any{
				"LastUpdatedTime": "2025-01-01T00:00:00-08:00",
			},
		}

		result := normalizeItem(item)
		assert.Nil(t, result["id"])
		assert.Equal(t, "2025-01-01T00:00:00-08:00", result["lastupdatedtime"])
	})
}

func TestJsonUseNumber(t *testing.T) {
	t.Run("preserves large integers", func(t *testing.T) {
		data := []byte(`{"id": 2033513821949367296, "name": "test"}`)
		var result map[string]any
		err := jsonUseNumber(data, &result)
		require.NoError(t, err)

		id, ok := result["id"].(json.Number)
		require.True(t, ok, "id should be json.Number, got %T", result["id"])
		assert.Equal(t, "2033513821949367296", id.String())

		i, err := id.Int64()
		require.NoError(t, err)
		assert.Equal(t, int64(2033513821949367296), i)
	})

	t.Run("preserves floats", func(t *testing.T) {
		data := []byte(`{"amount": 1234.56}`)
		var result map[string]any
		err := jsonUseNumber(data, &result)
		require.NoError(t, err)

		amount, ok := result["amount"].(json.Number)
		require.True(t, ok)
		f, err := amount.Float64()
		require.NoError(t, err)
		assert.InDelta(t, 1234.56, f, 0.001)
	})

	t.Run("invalid json returns error", func(t *testing.T) {
		data := []byte(`{invalid}`)
		var result map[string]any
		err := jsonUseNumber(data, &result)
		require.Error(t, err)
	})
}
