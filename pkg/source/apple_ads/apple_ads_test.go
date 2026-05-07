package appleads

import (
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseReportTableName(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantBase    string
		wantGran    string
		wantGroupBy []string
		wantErr     string
	}{
		{
			name:     "base table only no granularity",
			input:    "campaign_reports",
			wantBase: "campaign_reports",
			wantGran: "",
		},
		{
			name:     "explicit daily",
			input:    "campaign_reports:daily",
			wantBase: "campaign_reports",
			wantGran: "DAILY",
		},
		{
			name:     "hourly",
			input:    "ad_group_reports:hourly",
			wantBase: "ad_group_reports",
			wantGran: "HOURLY",
		},
		{
			name:     "weekly",
			input:    "keyword_reports:weekly",
			wantBase: "keyword_reports",
			wantGran: "WEEKLY",
		},
		{
			name:     "monthly",
			input:    "search_term_reports:monthly",
			wantBase: "search_term_reports",
			wantGran: "MONTHLY",
		},
		{
			name:     "case insensitive granularity",
			input:    "campaign_reports:HOURLY",
			wantBase: "campaign_reports",
			wantGran: "HOURLY",
		},
		{
			name:    "invalid granularity",
			input:   "campaign_reports:yearly",
			wantErr: `invalid granularity "yearly"`,
		},
		{
			name:        "daily with single groupBy",
			input:       "campaign_reports:daily:countryOrRegion",
			wantBase:    "campaign_reports",
			wantGran:    "DAILY",
			wantGroupBy: []string{"countryOrRegion"},
		},
		{
			name:        "hourly with multiple groupBy",
			input:       "campaign_reports:hourly:ageRange,gender",
			wantBase:    "campaign_reports",
			wantGran:    "HOURLY",
			wantGroupBy: []string{"ageRange", "gender"},
		},
		{
			name:        "all valid groupBy fields",
			input:       "campaign_reports:daily:countryOrRegion,ageRange,gender,deviceClass,adminArea,locality,countryCode",
			wantBase:    "campaign_reports",
			wantGran:    "DAILY",
			wantGroupBy: []string{"countryOrRegion", "ageRange", "gender", "deviceClass", "adminArea", "locality", "countryCode"},
		},
		{
			name:    "invalid groupBy field",
			input:   "campaign_reports:daily:invalidField",
			wantErr: `invalid groupBy field "invalidField"`,
		},
		{
			name:    "one valid one invalid groupBy",
			input:   "campaign_reports:daily:gender,bogus",
			wantErr: `invalid groupBy field "bogus"`,
		},
		{
			name:        "no granularity with groupBy (double colon)",
			input:       "campaign_reports::countryOrRegion",
			wantBase:    "campaign_reports",
			wantGran:    "",
			wantGroupBy: []string{"countryOrRegion"},
		},
		{
			name:        "no granularity with multiple groupBy",
			input:       "campaign_reports::ageRange,gender",
			wantBase:    "campaign_reports",
			wantGran:    "",
			wantGroupBy: []string{"ageRange", "gender"},
		},
		{
			name:    "groupBy without double colon fails as invalid granularity",
			input:   "campaign_reports:countryOrRegion",
			wantErr: `invalid granularity "countryOrRegion"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseReportTableName(tt.input)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantBase, got.baseTable)
			assert.Equal(t, tt.wantGran, got.granularity)
			assert.Equal(t, tt.wantGroupBy, got.groupBy)
		})
	}
}

func dateStr(t time.Time) string {
	return t.Format("2006-01-02")
}

func TestReportDateRange(t *testing.T) {
	t.Run("defaults per granularity", func(t *testing.T) {
		for _, tc := range []struct {
			gran     string
			lookback int
		}{
			{"HOURLY", 7},
			{"DAILY", 90},
			{"WEEKLY", 365},
			{"MONTHLY", 730},
		} {
			t.Run(tc.gran, func(t *testing.T) {
				start, end, err := reportDateRange(source.ReadOptions{}, tc.gran)
				require.NoError(t, err)
				now := time.Now().UTC()
				assert.Equal(t, dateStr(now), dateStr(end))
				expected := now.AddDate(0, 0, -tc.lookback)
				assert.Equal(t, dateStr(expected), dateStr(start))
			})
		}
	})

	t.Run("respects IntervalStart and IntervalEnd", func(t *testing.T) {
		now := time.Now().UTC()
		s := now.AddDate(0, 0, -30)
		e := now.AddDate(0, 0, -10)
		start, end, err := reportDateRange(source.ReadOptions{
			IntervalStart: &s,
			IntervalEnd:   &e,
		}, "DAILY")
		require.NoError(t, err)
		assert.Equal(t, dateStr(s), dateStr(start))
		assert.Equal(t, dateStr(e), dateStr(end))
	})

	t.Run("errors when start too old for HOURLY", func(t *testing.T) {
		now := time.Now().UTC()
		tooOld := now.AddDate(0, 0, -60)
		_, _, err := reportDateRange(source.ReadOptions{
			IntervalStart: &tooOld,
		}, "HOURLY")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "too far in the past")
		assert.Contains(t, err.Error(), "HOURLY")
	})

	t.Run("errors when start too old for DAILY", func(t *testing.T) {
		now := time.Now().UTC()
		tooOld := now.AddDate(0, 0, -200)
		_, _, err := reportDateRange(source.ReadOptions{
			IntervalStart: &tooOld,
		}, "DAILY")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "too far in the past")
		assert.Contains(t, err.Error(), "DAILY")
	})
}

func TestParseURI(t *testing.T) {
	t.Run("valid URI with key_base64", func(t *testing.T) {
		uri := "appleads://?client_id=cid&team_id=tid&key_id=kid&org_id=111,222&key_base64=dGVzdC1rZXk="
		cfg, err := parseURI(uri)
		require.NoError(t, err)
		assert.Equal(t, "cid", cfg.clientID)
		assert.Equal(t, "tid", cfg.teamID)
		assert.Equal(t, "kid", cfg.keyID)
		assert.Equal(t, []string{"111", "222"}, cfg.orgIDs)
		assert.Equal(t, "test-key", cfg.privateKey)
	})

	t.Run("missing client_id", func(t *testing.T) {
		_, err := parseURI("appleads://?team_id=tid&key_id=kid&org_id=1&key_base64=dGVzdA==")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "client_id")
	})

	t.Run("missing team_id", func(t *testing.T) {
		_, err := parseURI("appleads://?client_id=cid&key_id=kid&org_id=1&key_base64=dGVzdA==")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "team_id")
	})

	t.Run("missing key_id", func(t *testing.T) {
		_, err := parseURI("appleads://?client_id=cid&team_id=tid&org_id=1&key_base64=dGVzdA==")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "key_id")
	})

	t.Run("missing org_id", func(t *testing.T) {
		_, err := parseURI("appleads://?client_id=cid&team_id=tid&key_id=kid&key_base64=dGVzdA==")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "org_id")
	})

	t.Run("missing both key_path and key_base64", func(t *testing.T) {
		_, err := parseURI("appleads://?client_id=cid&team_id=tid&key_id=kid&org_id=1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "key_path or key_base64")
	})

	t.Run("invalid scheme", func(t *testing.T) {
		_, err := parseURI("postgres://host/db")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid appleads URI")
	})

	t.Run("single org_id", func(t *testing.T) {
		cfg, err := parseURI("appleads://?client_id=c&team_id=t&key_id=k&org_id=999&key_base64=dGVzdA==")
		require.NoError(t, err)
		assert.Equal(t, []string{"999"}, cfg.orgIDs)
	})
}

func TestIsValidTable(t *testing.T) {
	for _, table := range supportedTables {
		assert.True(t, isValidTable(table), "expected %q to be valid", table)
	}
	assert.False(t, isValidTable("keywords"))
	assert.False(t, isValidTable("search_terms"))
	assert.False(t, isValidTable("nonexistent"))
	assert.False(t, isValidTable("campaigns:daily"))
	assert.False(t, isValidTable("campaign_reports:daily"))
}
