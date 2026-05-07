package zendesk

import (
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseZendeskURI(t *testing.T) {
	tests := []struct {
		name      string
		uri       string
		want      zendeskCredentials
		wantErr   bool
		errSubstr string
	}{
		{
			name: "OAuth token with @-style URI",
			uri:  "zendesk://:my_oauth_token@mycompany",
			want: zendeskCredentials{
				subdomain:  "mycompany",
				oauthToken: "my_oauth_token",
				authType:   authOAuth,
			},
		},
		{
			name: "API token with @-style URI",
			uri:  "zendesk://user@example.com:my_api_token@mycompany",
			want: zendeskCredentials{
				subdomain: "mycompany",
				email:     "user@example.com",
				apiToken:  "my_api_token",
				authType:  authAPIToken,
			},
		},
		{
			name:      "missing scheme",
			uri:       "http://mycompany",
			wantErr:   true,
			errSubstr: "must start with zendesk://",
		},
		{
			name:      "empty credentials",
			uri:       "zendesk://:@mycompany",
			wantErr:   true,
			errSubstr: "invalid zendesk credentials",
		},
		{
			name:      "missing subdomain",
			uri:       "zendesk://user:token@",
			wantErr:   true,
			errSubstr: "subdomain is required",
		},
		{
			name:      "no @ separator",
			uri:       "zendesk://mycompany",
			wantErr:   true,
			errSubstr: "expected email:token@subdomain",
		},
		{
			name:      "no colon separator",
			uri:       "zendesk://token@mycompany",
			wantErr:   true,
			errSubstr: "expected email:token@subdomain",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseZendeskURI(tt.uri)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errSubstr != "" {
					assert.Contains(t, err.Error(), tt.errSubstr)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want.subdomain, got.subdomain)
			assert.Equal(t, tt.want.email, got.email)
			assert.Equal(t, tt.want.apiToken, got.apiToken)
			assert.Equal(t, tt.want.oauthToken, got.oauthToken)
			assert.Equal(t, tt.want.authType, got.authType)
		})
	}
}

func TestGetTableConfig(t *testing.T) {
	tests := []struct {
		table          string
		primaryKeys    []string
		incrementalKey string
		strategy       config.IncrementalStrategy
	}{
		{"tickets", []string{"id"}, "updated_at", config.StrategyMerge},
		{"ticket_metric_events", []string{"id"}, "time", config.StrategyAppend},
		{"calls", nil, "", config.StrategyReplace},
		{"calls_incremental", []string{"id"}, "updated_at", config.StrategyMerge},
		{"legs_incremental", []string{"id"}, "updated_at", config.StrategyMerge},
		{"chats", []string{"id"}, "update_timestamp", config.StrategyMerge},
		{"users", nil, "", config.StrategyReplace},
		{"groups", nil, "", config.StrategyReplace},
		{"brands", nil, "", config.StrategyReplace},
	}

	for _, tt := range tests {
		t.Run(tt.table, func(t *testing.T) {
			cfg := getTableConfig(tt.table)
			assert.Equal(t, tt.primaryKeys, cfg.primaryKeys)
			assert.Equal(t, tt.incrementalKey, cfg.incrementalKey)
			assert.Equal(t, tt.strategy, cfg.strategy)
		})
	}
}

func TestIsValidTable(t *testing.T) {
	for _, table := range supportedTables {
		assert.True(t, isValidTable(table), "expected %s to be valid", table)
	}

	assert.False(t, isValidTable("nonexistent"))
	assert.False(t, isValidTable(""))
}

func TestSupportedTablesHaveConfigs(t *testing.T) {
	for _, table := range supportedTables {
		cfg := getTableConfig(table)
		assert.NotNil(t, cfg, "table %s should have a config", table)
		assert.NotEmpty(t, cfg.strategy, "table %s should have a strategy", table)
	}
}

func TestGetTableConfigDefaultsToReplace(t *testing.T) {
	cfg := getTableConfig("unknown_table_xyz")
	assert.Equal(t, config.StrategyReplace, cfg.strategy)
	assert.Nil(t, cfg.primaryKeys)
	assert.Empty(t, cfg.incrementalKey)
}

func TestSchemes(t *testing.T) {
	s := &ZendeskSource{}
	schemes := s.Schemes()
	assert.Contains(t, schemes, "zendesk")
	assert.Len(t, schemes, 1)
}

func TestPivotCustomFields(t *testing.T) {
	fields := map[string]ticketField{
		"100": {title: "Priority Level", options: map[string]string{"high": "High Priority", "low": "Low Priority"}},
		"200": {title: "Region", options: map[string]string{}},
		"300": {title: "Notes"},
	}

	t.Run("pivots fields with option mapping", func(t *testing.T) {
		ticket := map[string]any{
			"id":      1,
			"subject": "test",
			"custom_fields": []any{
				map[string]any{"id": "100", "value": "high"},
			},
		}
		pivotCustomFields(ticket, fields)
		assert.Equal(t, "High Priority", ticket["priority_level"])
		assert.Nil(t, ticket["custom_fields"], "custom_fields should be deleted")
	})

	t.Run("pivots fields without options uses raw value", func(t *testing.T) {
		ticket := map[string]any{
			"custom_fields": []any{
				map[string]any{"id": "200", "value": "EMEA"},
			},
		}
		pivotCustomFields(ticket, fields)
		assert.Equal(t, "EMEA", ticket["region"])
	})

	t.Run("preserves nil values", func(t *testing.T) {
		ticket := map[string]any{
			"custom_fields": []any{
				map[string]any{"id": "300", "value": nil},
			},
		}
		pivotCustomFields(ticket, fields)
		assert.Nil(t, ticket["notes"])
		assert.Contains(t, ticket, "notes")
	})

	t.Run("unknown field id is skipped", func(t *testing.T) {
		ticket := map[string]any{
			"custom_fields": []any{
				map[string]any{"id": "999", "value": "something"},
			},
		}
		pivotCustomFields(ticket, fields)
		assert.NotContains(t, ticket, "something")
	})

	t.Run("unmapped option value falls through to raw value", func(t *testing.T) {
		ticket := map[string]any{
			"custom_fields": []any{
				map[string]any{"id": "100", "value": "unknown_option"},
			},
		}
		pivotCustomFields(ticket, fields)
		assert.Equal(t, "unknown_option", ticket["priority_level"])
	})

	t.Run("no custom_fields is a no-op", func(t *testing.T) {
		ticket := map[string]any{"id": 1, "subject": "test"}
		pivotCustomFields(ticket, fields)
		assert.Equal(t, map[string]any{"id": 1, "subject": "test"}, ticket)
	})
}
