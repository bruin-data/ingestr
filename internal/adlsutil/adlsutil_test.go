package adlsutil

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
