package adlsutil

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseClientCredentials(t *testing.T) {
	values := url.Values{
		"tenant_id":     {"tenant"},
		"client_id":     {"client"},
		"client_secret": {"secret"},
	}

	got := ParseClientCredentials(values)
	assert.Equal(t, ClientCredentials{
		TenantID:     "tenant",
		ClientID:     "client",
		ClientSecret: "secret",
	}, got)
	assert.True(t, got.IsSet())
}

func TestClientCredentialsNewTokenCredentialRequiresCompleteConfig(t *testing.T) {
	_, err := ClientCredentials{TenantID: "tenant", ClientID: "client"}.NewTokenCredential()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "client_secret")
}

func TestClientCredentialsNewTokenCredential(t *testing.T) {
	cred, err := ClientCredentials{
		TenantID:     "tenant",
		ClientID:     "client",
		ClientSecret: "secret",
	}.NewTokenCredential()
	require.NoError(t, err)
	assert.NotNil(t, cred)
}

func TestAppendSASToken(t *testing.T) {
	assert.Equal(t, "https://account.dfs.core.windows.net/fs?sig=abc", AppendSASToken("https://account.dfs.core.windows.net/fs", "sig=abc"))
	assert.Equal(t, "https://account.dfs.core.windows.net/fs?existing=1&sig=abc", AppendSASToken("https://account.dfs.core.windows.net/fs?existing=1", "sig=abc"))
	assert.Equal(t, "https://account.dfs.core.windows.net/fs", AppendSASToken("https://account.dfs.core.windows.net/fs", ""))
}

func TestParseAccountName(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"query value", "adls://?account_name=queryacct", "queryacct"},
		{"dfs host", "abfss://filesystem@hostacct.dfs.core.windows.net", "hostacct"},
		{"plain host", "adls://plainacct", "plainacct"},
		{"unrecognized host", "adls://blobacct.blob.core.windows.net", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(tt.raw)
			require.NoError(t, err)
			assert.Equal(t, tt.want, ParseAccountName(u))
		})
	}
}

func TestFilesystemURL(t *testing.T) {
	assert.Equal(t, "https://myaccount.dfs.core.windows.net/filesystem", FilesystemURL("myaccount", "/filesystem/"))
}

func TestPathURL(t *testing.T) {
	got, err := PathURL("myaccount", "filesystem", "records/users/file 1.parquet")
	require.NoError(t, err)
	assert.Equal(t, "https://myaccount.dfs.core.windows.net/filesystem/records/users/file%201.parquet", got)
}

func TestDirectoryPrefixes(t *testing.T) {
	tests := []struct {
		name               string
		path               string
		skipPrefixSegments int
		want               []string
	}{
		{
			name:               "all prefixes",
			path:               "lakehouse.Lakehouse/Tables/staff/_delta_log",
			skipPrefixSegments: 0,
			want: []string{
				"lakehouse.Lakehouse",
				"lakehouse.Lakehouse/Tables",
				"lakehouse.Lakehouse/Tables/staff",
				"lakehouse.Lakehouse/Tables/staff/_delta_log",
			},
		},
		{
			name:               "skip onelake managed item and area",
			path:               "lakehouse.Lakehouse/Tables/staff/_delta_log",
			skipPrefixSegments: 2,
			want: []string{
				"lakehouse.Lakehouse/Tables/staff",
				"lakehouse.Lakehouse/Tables/staff/_delta_log",
			},
		},
		{
			name:               "skip more than path length",
			path:               "lakehouse.Lakehouse/Tables",
			skipPrefixSegments: 3,
			want:               []string{},
		},
		{
			name:               "empty path",
			path:               "",
			skipPrefixSegments: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, directoryPrefixes(tt.path, tt.skipPrefixSegments))
		})
	}
}
