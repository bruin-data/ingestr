package athena

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/athena"
	"github.com/stretchr/testify/require"
)

func TestParseAthenaConfig_EndpointURL(t *testing.T) {
	t.Parallel()

	base := "athena://?bucket=my-bucket&access_key_id=ak&secret_access_key=sk&region_name=us-east-1"

	cfg, err := parseAthenaConfig(base)
	require.NoError(t, err)
	require.Empty(t, cfg.EndpointURL)

	cfg, err = parseAthenaConfig(base + "&endpoint_url=http://localhost:4566")
	require.NoError(t, err)
	require.Equal(t, "http://localhost:4566", cfg.EndpointURL)

	for _, endpoint := range []string{"http://athena.example.com", "ftp://localhost", "localhost:4566"} {
		_, err := parseAthenaConfig(base + "&endpoint_url=" + endpoint)
		require.Error(t, err, endpoint)
	}
}

func TestConnect_UsesEndpointURL(t *testing.T) {
	t.Parallel()

	var (
		mu  sync.Mutex
		ops []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ops = append(ops, strings.TrimPrefix(r.Header.Get("X-Amz-Target"), "AmazonAthena."))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_, _ = w.Write([]byte(`{"WorkGroups":[]}`))
	}))
	t.Cleanup(server.Close)

	target := NewAthenaSource()
	require.NoError(t, target.Connect(t.Context(), "athena://?bucket=my-bucket&access_key_id=ak&secret_access_key=sk&region_name=us-east-1&endpoint_url="+server.URL))
	_, err := target.client.ListWorkGroups(t.Context(), &athena.ListWorkGroupsInput{})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"ListWorkGroups"}, ops)
}
