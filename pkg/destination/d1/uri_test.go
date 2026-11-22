package d1

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseURI(t *testing.T) {
	for _, scheme := range []string{"d1", "cloudflare-d1"} {
		account, database, token, err := parseURI(scheme + "://account/1234-abcd?api_token=" + url.QueryEscape("a+b/c=="))
		require.NoError(t, err)
		require.Equal(t, "account", account)
		require.Equal(t, "1234-abcd", database)
		require.Equal(t, "a+b/c==", token)
	}
	for _, raw := range []string{
		"sqlite://account/database?api_token=secret",
		"d1:///database?api_token=secret",
		"d1://account?api_token=secret",
		"d1://account/database/extra?api_token=secret",
		"d1://account/database",
		"d1://user:secret@account/database?api_token=secret",
		"d1://account:443/database?api_token=secret",
		"d1://account/database?api_token=secret#fragment",
		"d1://account/database?api_token=secret&api_token=other",
		"d1://account/database?api_token=secret&base_url=https://example.com",
		"d1://account/database?api_token=%zzsecret",
		"d1://account/../database?api_token=secret",
	} {
		t.Run(strings.Split(raw, "?")[0], func(t *testing.T) {
			_, _, _, err := parseURI(raw)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret")
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestConnectUsesAccountAndDatabaseID(t *testing.T) {
	d := newHTTPTestDestination(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/client/v4/accounts/account/d1/database/database/query", r.URL.Path)
		require.Equal(t, "Bearer a+b/c==", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"success":true,"result":[{"success":true,"results":[{"1":1}]}]}`))
	})
	endpoint, err := url.Parse(d.endpoint)
	require.NoError(t, err)
	transport := d.client.Transport
	d.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, "api.cloudflare.com", req.URL.Host)
		req.URL.Scheme, req.URL.Host = endpoint.Scheme, endpoint.Host
		return transport.RoundTrip(req)
	})
	require.NoError(t, d.Connect(t.Context(), "d1://account/database?api_token="+url.QueryEscape("a+b/c==")))
	require.NoError(t, d.Close(t.Context()))
}
