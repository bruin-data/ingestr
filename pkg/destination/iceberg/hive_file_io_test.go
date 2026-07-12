package iceberg

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	icebergio "github.com/apache/iceberg-go/io"
	"github.com/stretchr/testify/require"
)

func TestHiveLocalIONormalizesSingleSlashFileURI(t *testing.T) {
	working := t.TempDir()
	t.Chdir(working)
	target := filepath.Join(t.TempDir(), "warehouse", "metadata", "v1.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))

	fileIO, err := icebergio.LoadFS(context.Background(), nil, "file:"+target)
	require.NoError(t, err)
	writeIO, ok := fileIO.(icebergio.WriteFileIO)
	require.True(t, ok)
	require.NoError(t, writeIO.WriteFile("file:"+target, []byte("metadata")))
	content, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, []byte("metadata"), content)
	require.NoDirExists(t, filepath.Join(working, "file:"), "file:/ must never be treated as a relative workspace path")
}

func TestFileURIContainsPlainAbsoluteChild(t *testing.T) {
	root := t.TempDir()
	require.True(t, locationContains("file:"+root, filepath.Join(root, "data", "file.parquet")))
	require.True(t, locationContains("file://"+root, filepath.Join(root, "metadata", "v1.json")))
	require.False(t, locationContains("file:"+root, filepath.Join(filepath.Dir(root), "outside.parquet")))
}
