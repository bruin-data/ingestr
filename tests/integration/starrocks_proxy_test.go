//go:build integration || stress

package integration

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// proxyStarRocksPorts keeps FE transaction coordination while making its
// container-private BE redirects reachable from the test process.
func proxyStarRocksPorts(t *testing.T, host, sqlPort, fePort, bePort string) (dsn, uri string, cleanup func()) {
	t.Helper()
	frontend := &url.URL{Scheme: "http", Host: net.JoinHostPort(host, fePort)}
	proxy := httputil.NewSingleHostReverseProxy(frontend)
	proxy.ModifyResponse = func(response *http.Response) error {
		if location := response.Header.Get("Location"); location != "" {
			target, err := url.Parse(location)
			if err != nil {
				return err
			}
			if target.Port() == "8040" {
				target.Host = net.JoinHostPort(host, bePort)
				response.Header.Set("Location", target.String())
			}
		}
		return nil
	}
	server := httptest.NewServer(proxy)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		server.Close()
		require.NoError(t, err)
	}
	var workers sync.WaitGroup
	var connections sync.Map
	accepted := make(chan struct{})
	workers.Go(func() {
		defer close(accepted)
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Store(client, struct{}{})
			workers.Go(func() {
				defer connections.Delete(client)
				defer func() { _ = client.Close() }()
				backend, err := net.DialTimeout("tcp", net.JoinHostPort(host, sqlPort), 5*time.Second)
				if err != nil {
					return
				}
				defer func() { _ = backend.Close() }()
				done := make(chan struct{})
				go func() {
					_, _ = io.Copy(backend, client)
					_ = backend.Close()
					close(done)
				}()
				_, _ = io.Copy(client, backend)
				_ = client.Close()
				<-done
			})
		}
	})
	cleanup = func() {
		_ = listener.Close()
		<-accepted
		connections.Range(func(key, _ any) bool {
			_ = key.(net.Conn).Close()
			return true
		})
		workers.Wait()
		server.CloseClientConnections()
		server.Close()
	}
	proxyURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	dsn = fmt.Sprintf("root@tcp(%s)/", listener.Addr().String())
	uri = fmt.Sprintf("starrocks://root@%s/?http_port=%s&replication_num=1", listener.Addr().String(), proxyURL.Port())
	return dsn, uri, cleanup
}

func TestStarRocksProxyPreservesFrontendAndBackendRoutes(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/transaction/load", r.URL.Path)
		user, password, ok := r.BasicAuth()
		require.True(t, ok)
		require.Equal(t, "root", user)
		require.Equal(t, "test-password", password)
		_, _ = io.WriteString(w, "backend")
	}))
	defer backend.Close()
	frontend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/transaction/load" {
			http.Redirect(w, r, "http://container-private:8040"+r.URL.Path, http.StatusTemporaryRedirect)
			return
		}
		require.Equal(t, "/api/transaction/commit", r.URL.Path)
		_, _ = io.WriteString(w, "frontend")
	}))
	defer frontend.Close()
	sqlListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = sqlListener.Close() }()
	go func() {
		connection, err := sqlListener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = connection.Close() }()
		_, _ = io.Copy(connection, connection)
	}()
	feURL, err := url.Parse(frontend.URL)
	require.NoError(t, err)
	beURL, err := url.Parse(backend.URL)
	require.NoError(t, err)
	_, sqlPort, err := net.SplitHostPort(sqlListener.Addr().String())
	require.NoError(t, err)
	_, uri, cleanup := proxyStarRocksPorts(t, "127.0.0.1", sqlPort, feURL.Port(), beURL.Port())
	defer cleanup()
	connectionURL, err := url.Parse(uri)
	require.NoError(t, err)
	connection, err := net.Dial("tcp", connectionURL.Host)
	require.NoError(t, err)
	defer func() { _ = connection.Close() }()
	require.NoError(t, connection.SetDeadline(time.Now().Add(5*time.Second)))
	_, err = connection.Write([]byte("SQL"))
	require.NoError(t, err)
	responseBytes := make([]byte, 3)
	_, err = io.ReadFull(connection, responseBytes)
	require.NoError(t, err)
	require.Equal(t, "SQL", string(responseBytes))
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	endpoint := "http://" + net.JoinHostPort(connectionURL.Hostname(), connectionURL.Query().Get("http_port"))
	response, err := client.Post(endpoint+"/api/transaction/commit", "", nil)
	require.NoError(t, err)
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	require.NoError(t, err)
	require.Equal(t, "frontend", string(body))
	response, err = client.Get(endpoint + "/api/transaction/load")
	require.NoError(t, err)
	_ = response.Body.Close()
	require.Equal(t, http.StatusTemporaryRedirect, response.StatusCode)
	require.Equal(t, backend.URL+"/api/transaction/load", response.Header.Get("Location"))
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPut, response.Header.Get("Location"), nil)
	require.NoError(t, err)
	request.SetBasicAuth("root", "test-password")
	response, err = client.Do(request)
	require.NoError(t, err)
	body, err = io.ReadAll(response.Body)
	_ = response.Body.Close()
	require.NoError(t, err)
	require.Equal(t, "backend", string(body))
}
