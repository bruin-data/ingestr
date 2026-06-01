package netsuite

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestParseURIWithAccessToken(t *testing.T) {
	cfg, err := parseURI("netsuite://123456?access_token=test-token")
	require.NoError(t, err)

	assert.Equal(t, "123456", cfg.accountID)
	assert.Equal(t, "https://123456.suitetalk.api.netsuite.com", cfg.baseURL)
	assert.Equal(t, "test-token", cfg.accessToken)
}

func TestParseURIWithSandboxAccountID(t *testing.T) {
	cfg, err := parseURI("netsuite://123456_SB1?access_token=test-token")
	require.NoError(t, err)

	assert.Equal(t, "123456_SB1", cfg.accountID)
	assert.Equal(t, "https://123456-sb1.suitetalk.api.netsuite.com", cfg.baseURL)
}

func TestParseURIWithClientCredentials(t *testing.T) {
	keyPath := writeTestPrivateKey(t)

	cfg, err := parseURI("netsuite://?account_id=123456&client_id=client-1&kid=cert-1&private_key_path=" + url.QueryEscape(keyPath) + "&scope=rest_webservices,restlets")
	require.NoError(t, err)

	assert.Equal(t, "123456", cfg.accountID)
	assert.Equal(t, "client-1", cfg.clientID)
	assert.Equal(t, "cert-1", cfg.certificateID)
	assert.Equal(t, []string{"rest_webservices", "restlets"}, cfg.scopes)
	require.NotEmpty(t, cfg.privateKeyPEM)
}

func TestParseURIErrors(t *testing.T) {
	tests := []struct {
		name string
		uri  string
	}{
		{"wrong scheme", "https://123456?access_token=x"},
		{"missing account without base URL", "netsuite://?access_token=x"},
		{"missing auth", "netsuite://123456"},
		{"missing certificate", "netsuite://123456?client_id=client-1&private_key=key"},
		{"missing private key", "netsuite://123456?client_id=client-1&certificate_id=cert-1"},
		{"invalid base URL", "netsuite://?base_url=not-a-url&access_token=x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseURI(tt.uri)
			require.Error(t, err)
		})
	}
}

func TestBuildSuiteQL(t *testing.T) {
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	end := time.Date(2026, 1, 3, 3, 4, 5, 0, time.UTC)

	got := buildSuiteQL("transaction", source.ReadOptions{
		IncrementalKey: "lastmodifieddate",
		IntervalStart:  &start,
		IntervalEnd:    &end,
	})

	assert.Equal(t, `SELECT * FROM transaction WHERE lastmodifieddate >= TO_TIMESTAMP_TZ('2026-01-02T03:04:05.000 +00:00', 'YYYY-MM-DD"T"HH24:MI:SS.FF TZH:TZM') AND lastmodifieddate < TO_TIMESTAMP_TZ('2026-01-03T03:04:05.000 +00:00', 'YYYY-MM-DD"T"HH24:MI:SS.FF TZH:TZM') ORDER BY lastmodifieddate ASC`, got)
}

func TestNetSuiteSourceGetTable(t *testing.T) {
	s := NewNetSuiteSource()

	table, err := s.GetTable(context.Background(), source.TableRequest{
		Name:           "customer",
		IncrementalKey: "lastmodifieddate",
		Strategy:       config.StrategyMerge,
	})
	require.NoError(t, err)

	assert.Equal(t, "customer", table.Name())
	assert.Equal(t, []string{"id"}, table.PrimaryKeys())
	assert.Equal(t, "lastmodifieddate", table.IncrementalKey())
	assert.Equal(t, config.StrategyMerge, table.Strategy())
	assert.False(t, table.HasKnownSchema())
}

func TestNetSuiteSourceReadUsesSuiteQLPagination(t *testing.T) {
	var requestCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/services/rest/query/v1/suiteql", r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "transient", r.Header.Get("Prefer"))
		assert.Equal(t, "2", r.URL.Query().Get("limit"))

		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "SELECT * FROM customer", body["q"])

		w.Header().Set("Content-Type", "application/json")
		call := requestCount.Add(1)
		if call == 1 {
			assert.Equal(t, "0", r.URL.Query().Get("offset"))
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"count":        2,
				"offset":       0,
				"hasMore":      true,
				"totalResults": 3,
				"items": []map[string]interface{}{
					{"links": []interface{}{}, "id": "1", "name": "Acme"},
					{"links": []interface{}{}, "id": "2", "name": "Globex"},
				},
			})
			return
		}

		assert.Equal(t, "2", r.URL.Query().Get("offset"))
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"count":        1,
			"offset":       2,
			"hasMore":      false,
			"totalResults": 3,
			"items": []map[string]interface{}{
				{"links": []interface{}{}, "id": "3", "name": "Umbrella"},
			},
		})
	}))
	defer server.Close()

	s := &NetSuiteSource{
		client: NewClient(server.URL, NewStaticTokenProvider("test-token")),
	}
	defer func() { _ = s.Close(context.Background()) }()

	table, err := s.GetTable(context.Background(), source.TableRequest{Name: "customer"})
	require.NoError(t, err)

	ch, err := table.Read(context.Background(), source.ReadOptions{PageSize: 2})
	require.NoError(t, err)

	batches := collectResults(t, ch)
	require.Len(t, batches, 2)
	assert.Equal(t, int64(2), batches[0].Batch.NumRows())
	assert.Equal(t, int64(1), batches[1].Batch.NumRows())
	assert.True(t, hasColumn(batches[0], "id"))
	assert.True(t, hasColumn(batches[0], "name"))
	assert.False(t, hasColumn(batches[0], "links"))
	assert.Equal(t, int32(2), requestCount.Load())
}

func TestNetSuiteSourceReadHonorsLimit(t *testing.T) {
	var requestCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		assert.Equal(t, "2", r.URL.Query().Get("limit"))
		assert.Equal(t, "0", r.URL.Query().Get("offset"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"count":        2,
			"offset":       0,
			"hasMore":      true,
			"totalResults": 10,
			"items": []map[string]interface{}{
				{"id": "1"},
				{"id": "2"},
			},
		})
	}))
	defer server.Close()

	s := &NetSuiteSource{
		client: NewClient(server.URL, NewStaticTokenProvider("test-token")),
	}
	defer func() { _ = s.Close(context.Background()) }()

	ch, err := s.readTable(context.Background(), "customer", source.ReadOptions{Limit: 2})
	require.NoError(t, err)

	batches := collectResults(t, ch)
	require.Len(t, batches, 1)
	assert.Equal(t, int64(2), batches[0].Batch.NumRows())
	assert.Equal(t, int32(1), requestCount.Load())
}

func TestExecuteCustomQueryTrimsSemicolon(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "SELECT id FROM customer", body["q"])

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"count":        0,
			"offset":       0,
			"hasMore":      false,
			"totalResults": 0,
			"items":        []map[string]interface{}{},
		})
	}))
	defer server.Close()

	s := &NetSuiteSource{
		client: NewClient(server.URL, NewStaticTokenProvider("test-token")),
	}
	defer func() { _ = s.Close(context.Background()) }()

	ch, err := s.ExecuteCustomQuery(context.Background(), "SELECT id FROM customer;", source.ReadOptions{})
	require.NoError(t, err)
	collectResults(t, ch)
}

func TestClientCredentialsProviderCachesToken(t *testing.T) {
	keyPath := writeTestPrivateKey(t)
	privateKeyPEM, err := os.ReadFile(keyPath)
	require.NoError(t, err)

	var requestCount atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "client_credentials", r.Form.Get("grant_type"))
		assert.Equal(t, clientAssertionType, r.Form.Get("client_assertion_type"))

		parser := jwt.NewParser()
		token, _, err := parser.ParseUnverified(r.Form.Get("client_assertion"), jwt.MapClaims{})
		require.NoError(t, err)
		assert.Equal(t, "cert-1", token.Header["kid"])
		claims := token.Claims.(jwt.MapClaims)
		assert.Equal(t, "client-1", claims["iss"])
		assert.Equal(t, server.URL+tokenPath, claims["aud"])
		assert.Equal(t, []interface{}{"rest_webservices"}, claims["scope"])

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "generated-token",
			"expires_in":   "3600",
			"token_type":   "Bearer",
		})
	}))
	defer server.Close()

	provider, err := NewClientCredentialsProvider(ClientCredentialsConfig{
		TokenURL:      server.URL + tokenPath,
		ClientID:      "client-1",
		CertificateID: "cert-1",
		PrivateKeyPEM: privateKeyPEM,
		Scopes:        []string{"rest_webservices"},
		Algorithm:     "PS256",
	})
	require.NoError(t, err)
	defer func() { _ = provider.Close() }()

	token, err := provider.AccessToken(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "generated-token", token)

	token, err = provider.AccessToken(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "generated-token", token)
	assert.Equal(t, int32(1), requestCount.Load())
}

func writeTestPrivateKey(t *testing.T) string {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	data := x509.MarshalPKCS1PrivateKey(key)
	pemData := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: data})
	path := t.TempDir() + "/netsuite-test-key.pem"
	require.NoError(t, os.WriteFile(path, pemData, 0o600))
	return path
}

func collectResults(t *testing.T, ch <-chan source.RecordBatchResult) []source.RecordBatchResult {
	t.Helper()

	var results []source.RecordBatchResult
	for result := range ch {
		require.NoError(t, result.Err)
		if result.Batch != nil {
			results = append(results, result)
		}
	}
	return results
}

func hasColumn(result source.RecordBatchResult, name string) bool {
	for i := 0; i < int(result.Batch.NumCols()); i++ {
		if result.Batch.ColumnName(i) == name {
			return true
		}
	}
	return false
}
