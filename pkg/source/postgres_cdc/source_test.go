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
			name:            "invalid mode",
			uri:             "postgres+cdc://user:pass@localhost:5432/mydb?publication=my_pub&mode=invalid",
			wantPublication: "",
			wantSlot:        "",
			wantMode:        "",
			wantErr:         true,
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

			// Verify normalized URI doesn't contain CDC params
			assert.NotContains(t, normalizedURI, "publication=")
			assert.NotContains(t, normalizedURI, "slot=")
			assert.NotContains(t, normalizedURI, "mode=")
			assert.NotContains(t, normalizedURI, "dest_schema=")
			assert.NotContains(t, normalizedURI, "+cdc")
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
