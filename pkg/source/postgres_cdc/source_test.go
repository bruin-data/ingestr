package postgres_cdc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchemes(t *testing.T) {
	source := NewPostgresCDCSource()
	schemes := source.Schemes()

	assert.Contains(t, schemes, "postgres+cdc")
	assert.Contains(t, schemes, "postgresql+cdc")
	assert.Len(t, schemes, 2)
}

func TestParseURIConfig(t *testing.T) {
	tests := []struct {
		name            string
		uri             string
		wantPublication string
		wantSlot        string
		wantMode        CDCMode
		wantDestSchema  string
		wantStateID     string
		wantBinary      bool
		wantErr         bool
	}{
		{
			name:            "full config",
			uri:             "postgres+cdc://user:pass@localhost:5432/mydb?publication=my_pub&slot=my_slot&mode=stream",
			wantPublication: "my_pub",
			wantSlot:        "my_slot",
			wantMode:        ModeStream,
			wantErr:         false,
		},
		{
			name:            "minimal config",
			uri:             "postgres+cdc://user:pass@localhost:5432/mydb?publication=my_pub",
			wantPublication: "my_pub",
			wantSlot:        "",
			wantMode:        ModeBatch,
			wantErr:         false,
		},
		{
			name:            "postgresql+cdc scheme",
			uri:             "postgresql+cdc://user:pass@localhost:5432/mydb?publication=my_pub",
			wantPublication: "my_pub",
			wantSlot:        "",
			wantMode:        ModeBatch,
			wantErr:         false,
		},
		{
			name:            "batch mode explicit",
			uri:             "postgres+cdc://user:pass@localhost:5432/mydb?publication=my_pub&mode=batch",
			wantPublication: "my_pub",
			wantSlot:        "",
			wantMode:        ModeBatch,
			wantErr:         false,
		},
		{
			name:            "with dest_schema",
			uri:             "postgres+cdc://user:pass@localhost:5432/mydb?publication=my_pub&dest_schema=my_dataset",
			wantPublication: "my_pub",
			wantSlot:        "",
			wantMode:        ModeBatch,
			wantDestSchema:  "my_dataset",
			wantErr:         false,
		},
		{
			name:            "with explicit state identity",
			uri:             "postgres+cdc://user:pass@localhost:5432/mydb?publication=my_pub&state_id=orders-east",
			wantPublication: "my_pub",
			wantMode:        ModeBatch,
			wantStateID:     "orders-east",
		},
		{
			name:            "invalid mode",
			uri:             "postgres+cdc://user:pass@localhost:5432/mydb?publication=my_pub&mode=invalid",
			wantPublication: "",
			wantSlot:        "",
			wantMode:        "",
			wantErr:         true,
		},
		{
			name:            "binary opt-in",
			uri:             "postgres+cdc://user:pass@localhost:5432/mydb?publication=my_pub&binary=true",
			wantPublication: "my_pub",
			wantMode:        ModeBatch,
			wantBinary:      true,
		},
		{
			name:            "binary explicit off",
			uri:             "postgres+cdc://user:pass@localhost:5432/mydb?publication=my_pub&binary=false",
			wantPublication: "my_pub",
			wantMode:        ModeBatch,
			wantBinary:      false,
		},
		{
			name:    "binary invalid value",
			uri:     "postgres+cdc://user:pass@localhost:5432/mydb?publication=my_pub&binary=maybe",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, normalizedURI, err := parseURIConfig(tt.uri)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantPublication, cfg.Publication)
			assert.Equal(t, tt.wantSlot, cfg.SlotName)
			assert.Equal(t, tt.wantMode, cfg.Mode)
			assert.Equal(t, tt.wantDestSchema, cfg.DestSchema)
			assert.Equal(t, tt.wantStateID, cfg.StateID)
			assert.Equal(t, tt.wantBinary, cfg.Binary)

			// Verify normalized URI doesn't contain CDC params
			assert.NotContains(t, normalizedURI, "publication=")
			assert.NotContains(t, normalizedURI, "slot=")
			assert.NotContains(t, normalizedURI, "mode=")
			assert.NotContains(t, normalizedURI, "dest_schema=")
			assert.NotContains(t, normalizedURI, "binary=")
			assert.NotContains(t, normalizedURI, "state_id=")
			assert.NotContains(t, normalizedURI, "+cdc")
		})
	}
}

func TestQuotePublicationTables(t *testing.T) {
	tests := []struct {
		name   string
		tables []pgTableRef
		want   string
	}{
		{
			name:   "single public table",
			tables: []pgTableRef{{schema: "public", name: "users"}},
			want:   `"public"."users"`,
		},
		{
			name:   "multiple schemas",
			tables: []pgTableRef{{schema: "public", name: "users"}, {schema: "app", name: "orders"}},
			want:   `"public"."users", "app"."orders"`,
		},
		{
			name:   "identifiers needing quoting",
			tables: []pgTableRef{{schema: "public", name: "Mixed Case"}, {schema: "public", name: `weird"name`}},
			want:   `"public"."Mixed Case", "public"."weird""name"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, quotePublicationTables(tt.tables))
		})
	}
}

func TestBuildReplicationConnString(t *testing.T) {
	tests := []struct {
		name string
		uri  string
		want string
	}{
		{
			name: "simple URI",
			uri:  "postgres://user:pass@localhost:5432/mydb",
			want: "postgres://user:pass@localhost:5432/mydb?replication=database",
		},
		{
			name: "URI with existing params",
			uri:  "postgres://user:pass@localhost:5432/mydb?sslmode=disable",
			want: "postgres://user:pass@localhost:5432/mydb?replication=database&sslmode=disable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildReplicationConnString(tt.uri)
			assert.Contains(t, got, "replication=database")
		})
	}
}
