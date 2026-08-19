package adlsutil

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	azruntime "github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azdatalake/datalakeerror"
	datalakefile "github.com/Azure/azure-sdk-for-go/sdk/storage/azdatalake/file"
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
			name:               "skip one prefix",
			path:               "lakehouse.Lakehouse/Tables/staff/_delta_log",
			skipPrefixSegments: 1,
			want: []string{
				"lakehouse.Lakehouse/Tables",
				"lakehouse.Lakehouse/Tables/staff",
				"lakehouse.Lakehouse/Tables/staff/_delta_log",
			},
		},
		{
			name:               "skip onelake managed item and area",
			path:               "lakehouse.Lakehouse/Tables/staff/_delta_log",
			skipPrefixSegments: OneLakeManagedPrefixSegments,
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

func TestPathURLWithSuffixEscapesWorkspace(t *testing.T) {
	got, err := PathURLWithSuffix(OneLakeAccountName, OneLakeDNSSuffix, "Fabric Dev", "lh.Lakehouse/Tables/users/_delta_log/00000000000000000000.json")
	require.NoError(t, err)
	assert.Equal(
		t,
		"https://onelake.dfs.fabric.microsoft.com/Fabric%20Dev/lh.Lakehouse/Tables/users/_delta_log/00000000000000000000.json",
		got,
	)
}

func TestFilesystemURLWithSuffixEscapesWorkspace(t *testing.T) {
	assert.Equal(
		t,
		"https://onelake.dfs.fabric.microsoft.com/Fabric%20Dev",
		FilesystemURLWithSuffix(OneLakeAccountName, OneLakeDNSSuffix, "/Fabric Dev/"),
	)
}

func TestRenameSourceHeader(t *testing.T) {
	assert.Equal(
		t,
		"/Fabric%20Dev/lh.Lakehouse/Tables/t/_bruin_delta_tmp/x.tmp",
		renameSourceHeader("Fabric Dev", "lh.Lakehouse/Tables/t/_bruin_delta_tmp/x.tmp"),
	)
	assert.Equal(t, "/ws/a/b", renameSourceHeader("/ws/", "/a/b/"))
}

type recordingTransport struct {
	req    *http.Request
	status int
}

func (t *recordingTransport) Do(req *http.Request) (*http.Response, error) {
	t.req = req
	return &http.Response{
		StatusCode: t.status,
		Header:     http.Header{"X-Ms-Error-Code": []string{string(datalakeerror.PathAlreadyExists)}},
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    req,
	}, nil
}

func TestRenameIfNotExistsSendsEscapedSourceAndIfNoneMatch(t *testing.T) {
	transport := &recordingTransport{status: http.StatusCreated}
	client := &DataLakeClient{
		accountName: OneLakeAccountName,
		dnsSuffix:   OneLakeDNSSuffix,
		pipeline: azruntime.NewPipeline("test", "v1", azruntime.PipelineOptions{}, &policy.ClientOptions{
			Transport: transport,
		}),
	}

	err := client.RenameIfNotExists(
		t.Context(),
		"Fabric Dev",
		"lh.Lakehouse/Tables/t/_bruin_delta_tmp/x.tmp",
		"lh.Lakehouse/Tables/t/_delta_log/00000000000000000001.json",
	)
	require.NoError(t, err)

	require.NotNil(t, transport.req)
	assert.Equal(t, http.MethodPut, transport.req.Method)
	assert.Equal(
		t,
		"/Fabric%20Dev/lh.Lakehouse/Tables/t/_bruin_delta_tmp/x.tmp",
		transport.req.Header.Get("x-ms-rename-source"),
	)
	assert.Equal(t, "*", transport.req.Header.Get("If-None-Match"))
	assert.Equal(t, dfsServiceVersion, transport.req.Header.Get("x-ms-version"))
	assert.Equal(t, "/Fabric%20Dev/lh.Lakehouse/Tables/t/_delta_log/00000000000000000001.json", transport.req.URL.EscapedPath())
	assert.Equal(t, "mode=legacy", transport.req.URL.RawQuery)
}

func TestRenameIfNotExistsReportsConflictAsResponseError(t *testing.T) {
	transport := &recordingTransport{status: http.StatusConflict}
	client := &DataLakeClient{
		accountName: OneLakeAccountName,
		dnsSuffix:   OneLakeDNSSuffix,
		pipeline: azruntime.NewPipeline("test", "v1", azruntime.PipelineOptions{}, &policy.ClientOptions{
			Transport: transport,
		}),
	}

	err := client.RenameIfNotExists(t.Context(), "ws", "a/b.tmp", "a/c.json")
	require.Error(t, err)
	assert.True(t, datalakeerror.HasCode(err, datalakeerror.PathAlreadyExists))
}

func TestRenameIfNotExistsAppendsSASToken(t *testing.T) {
	transport := &recordingTransport{status: http.StatusCreated}
	client := &DataLakeClient{
		accountName: OneLakeAccountName,
		dnsSuffix:   OneLakeDNSSuffix,
		sasToken:    "sv=2021&sig=abc",
		pipeline: azruntime.NewPipeline("test", "v1", azruntime.PipelineOptions{}, &policy.ClientOptions{
			Transport: transport,
		}),
	}

	require.NoError(t, client.RenameIfNotExists(t.Context(), "Fabric Dev", "a/b.tmp", "a/c.json"))
	assert.Equal(t, "mode=legacy&sv=2021&sig=abc", transport.req.URL.RawQuery)
	assert.Equal(t, "/Fabric%20Dev/a/b.tmp?sv=2021&sig=abc", transport.req.Header.Get("x-ms-rename-source"))
}

// TestRenameIfNotExistsMatchesSDKRequest pins the hand-built rename to the
// request datalakefile.Client.Rename produces. The two diverged once already:
// sending "resource=file" instead of "mode=legacy" turned the Create Path call
// into a plain create, and OneLake rejected x-ms-rename-source with a 400
// UnsupportedHeader. Only the header value may differ, and only for names that
// need percent-encoding, which is why the rename is hand-built at all.
func TestRenameIfNotExistsMatchesSDKRequest(t *testing.T) {
	const (
		workspace = "FabricDev"
		srcPath   = "lh.Lakehouse/Tables/t/_bruin_delta_tmp/x.tmp"
		destPath  = "lh.Lakehouse/Tables/t/_delta_log/00000000000000000000.json"
	)

	srcURL, err := PathURLWithSuffix(OneLakeAccountName, OneLakeDNSSuffix, workspace, srcPath)
	require.NoError(t, err)

	sdkTransport := &recordingTransport{status: http.StatusCreated}
	sdkClient, err := datalakefile.NewClientWithNoCredential(srcURL, &datalakefile.ClientOptions{
		ClientOptions: policy.ClientOptions{Transport: sdkTransport},
	})
	require.NoError(t, err)

	etagAny := azcore.ETagAny
	_, err = sdkClient.Rename(t.Context(), destPath, &datalakefile.RenameOptions{
		AccessConditions: &datalakefile.AccessConditions{
			ModifiedAccessConditions: &datalakefile.ModifiedAccessConditions{IfNoneMatch: &etagAny},
		},
	})
	require.NoError(t, err)

	ourTransport := &recordingTransport{status: http.StatusCreated}
	client := &DataLakeClient{
		accountName: OneLakeAccountName,
		dnsSuffix:   OneLakeDNSSuffix,
		pipeline: azruntime.NewPipeline("test", "v1", azruntime.PipelineOptions{}, &policy.ClientOptions{
			Transport: ourTransport,
		}),
	}
	require.NoError(t, client.RenameIfNotExists(t.Context(), workspace, srcPath, destPath))

	require.NotNil(t, sdkTransport.req)
	require.NotNil(t, ourTransport.req)
	assert.Equal(t, sdkTransport.req.Method, ourTransport.req.Method)
	assert.Equal(t, sdkTransport.req.URL.EscapedPath(), ourTransport.req.URL.EscapedPath())
	assert.Equal(t, sdkTransport.req.URL.RawQuery, ourTransport.req.URL.RawQuery)
	for _, header := range []string{"x-ms-rename-source", "x-ms-version", "If-None-Match"} {
		assert.Equal(t, headerValue(sdkTransport.req.Header, header), headerValue(ourTransport.req.Header, header), header)
	}
}

// headerValue looks a header up without canonicalizing the name. The generated
// SDK client assigns straight into the map with lowercase keys, so http.Header.Get
// misses those entries even though the wire format is case-insensitive.
func headerValue(h http.Header, name string) string {
	for key, values := range h {
		if strings.EqualFold(key, name) && len(values) > 0 {
			return values[0]
		}
	}
	return ""
}
