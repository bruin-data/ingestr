package gliner

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAssetDownloadAndCache(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte("weights"))
	}))
	defer server.Close()
	dir := t.TempDir()
	checksum := fmt.Sprintf("%x", sha256.Sum256([]byte("weights")))
	for range 2 {
		require.NoError(t, ensureAsset(t.Context(), server.Client(), server.URL, dir, "onnx/model.onnx", checksum, true))
	}
	require.EqualValues(t, 1, requests.Load())
	require.NoError(t, os.WriteFile(filepath.Join(dir, "onnx/model.onnx"), []byte("corrupt"), 0o600))
	require.ErrorContains(t, ensureAsset(t.Context(), server.Client(), server.URL, dir, "onnx/model.onnx", checksum, true), "checksum mismatch")
	require.EqualValues(t, 1, requests.Load())
}

func TestAssetFailureDoesNotPublish(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("wrong"))
	}))
	defer server.Close()
	dir := t.TempDir()
	require.ErrorContains(t, ensureAsset(t.Context(), server.Client(), server.URL, dir, "model.onnx", "bad", true), "checksum mismatch")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, ensureAsset(ctx, server.Client(), server.URL, dir, "model.onnx", "bad", true), context.Canceled)
}

func TestOfflineDirectoryNeverDownloads(t *testing.T) {
	t.Setenv("INGESTR_GLINER_MODEL_DIR", t.TempDir())
	r, err := New(t.Context())
	require.Nil(t, r)
	require.ErrorContains(t, err, "offline mode")
}
