package hanautil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name         string
		uri          string
		wantDSN      string
		wantDatabase string
		wantError    string
	}{
		{
			name:         "original source format",
			uri:          "hana://user:p%40ss@hana.example.com/landing",
			wantDSN:      "hdb://user:p%40ss@hana.example.com:30015?defaultSchema=landing",
			wantDatabase: "landing",
		},
		{
			name:         "tenant query parameter",
			uri:          "hana://user:pass@hana.example.com/landing?databaseName=TENANT",
			wantDSN:      "hdb://user:pass@hana.example.com:30015?databaseName=TENANT&defaultSchema=landing",
			wantDatabase: "landing",
		},
		{
			name:         "cloud TLS defaults",
			uri:          "saphana://user:pass@abc.hanacloud.ondemand.com:443/APP",
			wantDSN:      "hdb://user:pass@abc.hanacloud.ondemand.com:443?TLSServerName=abc.hanacloud.ondemand.com&defaultSchema=APP",
			wantDatabase: "APP",
		},
		{
			name:         "ipv6 literal host keeps brackets",
			uri:          "hana://user:pass@[2001:db8::1]:30015/APP",
			wantDSN:      "hdb://user:pass@[2001:db8::1]:30015?defaultSchema=APP",
			wantDatabase: "APP",
		},
		{
			name:      "invalid scheme",
			uri:       "postgres://user:pass@localhost/db",
			wantError: "unsupported scheme",
		},
		{
			name:      "missing host",
			uri:       "hana:///APP",
			wantError: "host is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsn, database, err := ParseURI(tt.uri)
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantDSN, dsn)
			assert.Equal(t, tt.wantDatabase, database)
		})
	}
}
