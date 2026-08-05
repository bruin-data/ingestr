package esclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientPerform(t *testing.T) {
	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/index/_search", r.URL.Path)
		assert.Equal(t, "5m", r.URL.Query().Get("scroll"))
		assert.Equal(t, JSONContentType, r.Header.Get("Content-Type"))
		username, password, ok := r.BasicAuth()
		assert.True(t, ok)
		assert.Equal(t, "user", username)
		assert.Equal(t, "password", password)
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.JSONEq(t, `{"query":{"match_all":{}}}`, string(body))

		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := New(Config{
		BaseURL:     server.URL,
		Username:    "user",
		Password:    "password",
		VerifyCerts: true,
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, client.Close(context.Background())) }()

	for range 2 {
		res, err := client.Perform(
			context.Background(),
			http.MethodPost,
			"/index/_search",
			url.Values{"scroll": {"5m"}},
			strings.NewReader(`{"query":{"match_all":{}}}`),
			JSONContentType,
		)
		require.NoError(t, err)
		require.NoError(t, res.Body.Close())
	}
	assert.Equal(t, 2, requestCount)
}

func TestClientAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "APIKey secret", r.Header.Get("Authorization"))
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, APIKey: "secret", VerifyCerts: true})
	require.NoError(t, err)
	defer func() { require.NoError(t, client.Close(context.Background())) }()

	res, err := client.Perform(context.Background(), http.MethodGet, "/", nil, nil, "")
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())
}

func TestClientRejectsNonElasticsearchServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, VerifyCerts: true})
	require.NoError(t, err)
	defer func() { require.NoError(t, client.Close(context.Background())) }()

	_, err = client.Perform(context.Background(), http.MethodGet, "/", nil, nil, "")
	require.ErrorContains(t, err, "server is not Elasticsearch")
}

func TestNewRejectsInvalidURL(t *testing.T) {
	_, err := New(Config{BaseURL: "localhost:9200", VerifyCerts: true})
	require.ErrorContains(t, err, "invalid Elasticsearch URL")
}

func TestClientVerifyCerts(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	t.Run("skips verification when disabled", func(t *testing.T) {
		client, err := New(Config{BaseURL: server.URL, VerifyCerts: false})
		require.NoError(t, err)
		defer func() { require.NoError(t, client.Close(context.Background())) }()

		res, err := client.Perform(context.Background(), http.MethodGet, "/", nil, nil, "")
		require.NoError(t, err)
		require.NoError(t, res.Body.Close())
	})

	t.Run("rejects untrusted certificate when enabled", func(t *testing.T) {
		client, err := New(Config{BaseURL: server.URL, VerifyCerts: true})
		require.NoError(t, err)
		defer func() { require.NoError(t, client.Close(context.Background())) }()

		_, err = client.Perform(context.Background(), http.MethodGet, "/", nil, nil, "")
		require.ErrorContains(t, err, "certificate")
	})
}

func TestStatusMessage(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "includes the elasticsearch error body",
			body: `{"error":{"type":"security_exception","reason":"missing authentication credentials"}}`,
			want: `401 Unauthorized: {"error":{"type":"security_exception","reason":"missing authentication credentials"}}`,
		},
		{
			name: "falls back to the status when the body is empty",
			body: "",
			want: "401 Unauthorized",
		},
		{
			name: "truncates an oversized body",
			body: strings.Repeat("x", maxErrorBodyBytes*2),
			want: "401 Unauthorized: " + strings.Repeat("x", maxErrorBodyBytes),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := &http.Response{
				Status: "401 Unauthorized",
				Body:   io.NopCloser(strings.NewReader(tt.body)),
			}
			assert.Equal(t, tt.want, StatusMessage(res))
		})
	}
}

func TestClientCloseIsIdempotent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, VerifyCerts: true})
	require.NoError(t, err)

	res, err := client.Perform(context.Background(), http.MethodGet, "/", nil, nil, "")
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())

	require.NoError(t, client.Close(context.Background()))
	require.NoError(t, client.Close(context.Background()))
}
