package postgres_cdc

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnapshotDiskStoreSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	directory := filepath.Join(t.TempDir(), "nested", "snapshot")
	store, err := newSnapshotDiskStore(directory, "source:target")
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, "wal/epoch/00000000000000000000", []byte("committed changes")))
	require.NoError(t, store.Save(ctx, "wal/epoch/manifest", []byte("old manifest")))
	require.NoError(t, store.Save(ctx, "wal/epoch/manifest", []byte("new manifest")))
	require.NoError(t, store.Save(ctx, "empty", nil))

	reopened, err := newSnapshotDiskStore(directory, "source:target")
	require.NoError(t, err)
	identity, err := reopened.Identity(ctx)
	require.NoError(t, err)
	assert.Equal(t, "source:target", identity)
	for key, expected := range map[string][]byte{
		"wal/epoch/00000000000000000000": []byte("committed changes"),
		"wal/epoch/manifest":             []byte("new manifest"),
		"empty":                          {},
	} {
		actual, err := reopened.Load(ctx, key)
		require.NoError(t, err)
		require.NotNil(t, actual)
		assert.Equal(t, expected, actual)
	}
	missing, err := reopened.Load(ctx, "missing")
	require.NoError(t, err)
	assert.Nil(t, missing)

	disk := store.(*snapshotDiskStore)
	for _, path := range []string{filepath.Dir(directory), directory, disk.directory} {
		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
	}
	entries, err := os.ReadDir(disk.directory)
	require.NoError(t, err)
	require.Len(t, entries, 3)
	for _, entry := range entries {
		info, err := entry.Info()
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
}

func TestSnapshotDiskStoreRejectsCorruption(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		name   string
		mutate func([]byte) []byte
		error  string
	}{
		{
			name: "truncated header",
			mutate: func(data []byte) []byte {
				return data[:snapshotDiskHeaderSize-1]
			},
			error: "invalid snapshot WAL file",
		},
		{
			name: "invalid header",
			mutate: func(data []byte) []byte {
				data[0] ^= 1
				return data
			},
			error: "invalid snapshot WAL file",
		},
		{
			name: "unsupported version",
			mutate: func(data []byte) []byte {
				data[len(snapshotDiskMagic)]++
				return data
			},
			error: "unsupported snapshot WAL version",
		},
		{
			name: "changed payload",
			mutate: func(data []byte) []byte {
				data[len(data)-1] ^= 1
				return data
			},
			error: "snapshot WAL checksum mismatch",
		},
		{
			name: "truncated payload",
			mutate: func(data []byte) []byte {
				return data[:len(data)-1]
			},
			error: "snapshot WAL checksum mismatch",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := newSnapshotDiskStore(t.TempDir(), "identity")
			require.NoError(t, err)
			require.NoError(t, store.Save(ctx, "page", []byte("changes")))
			path := store.(*snapshotDiskStore).path("page")
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(path, test.mutate(data), 0o600))
			loaded, err := store.Load(ctx, "page")
			require.ErrorContains(t, err, test.error)
			assert.Nil(t, loaded)
		})
	}
}

func TestSnapshotDiskStoreBindsKeysAndIdentities(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	first, err := newSnapshotDiskStore(directory, "first")
	require.NoError(t, err)
	second, err := newSnapshotDiskStore(directory, "second")
	require.NoError(t, err)
	key := "../../wal/epoch/page"
	require.NoError(t, first.Save(ctx, key, []byte("first changes")))
	missing, err := second.Load(ctx, key)
	require.NoError(t, err)
	assert.Nil(t, missing)
	require.NoError(t, second.Save(ctx, key, []byte("second changes")))
	loaded, err := first.Load(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, []byte("first changes"), loaded)

	firstDisk := first.(*snapshotDiskStore)
	secondDisk := second.(*snapshotDiskStore)
	assert.Equal(t, firstDisk.directory, filepath.Dir(firstDisk.path(key)))
	encoded, err := os.ReadFile(firstDisk.path(key))
	require.NoError(t, err)
	for _, target := range []struct {
		store *snapshotDiskStore
		key   string
	}{
		{store: firstDisk, key: "different key"},
		{store: secondDisk, key: key},
	} {
		require.NoError(t, os.WriteFile(target.store.path(target.key), encoded, 0o600))
		_, err := target.store.Load(ctx, target.key)
		require.ErrorContains(t, err, "snapshot WAL identity or key mismatch")
	}
}

func TestSnapshotDiskStoreDeleteSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	store, err := newSnapshotDiskStore(directory, "identity")
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, "page", []byte("changes")))
	require.NoError(t, store.Delete(ctx, "page"))
	require.NoError(t, store.Delete(ctx, "page"))
	reopened, err := newSnapshotDiskStore(directory, "identity")
	require.NoError(t, err)
	loaded, err := reopened.Load(ctx, "page")
	require.NoError(t, err)
	assert.Nil(t, loaded)
}

func TestSnapshotDiskStoreFailedSaveLeavesNoTemporaryFiles(t *testing.T) {
	ctx := context.Background()
	store, err := newSnapshotDiskStore(t.TempDir(), "identity")
	require.NoError(t, err)
	disk := store.(*snapshotDiskStore)
	require.NoError(t, store.Save(ctx, "manifest", []byte("durable manifest")))
	require.NoError(t, os.Mkdir(disk.path("page"), 0o700))
	require.ErrorContains(t, store.Save(ctx, "page", []byte("new changes")), "publish snapshot WAL")
	entries, err := os.ReadDir(disk.directory)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	for _, entry := range entries {
		assert.NotContains(t, entry.Name(), ".wal-")
	}
	require.NoError(t, os.Remove(disk.path("page")))
	loaded, err := store.Load(ctx, "page")
	require.NoError(t, err)
	assert.Nil(t, loaded)
	loaded, err = store.Load(ctx, "manifest")
	require.NoError(t, err)
	assert.Equal(t, []byte("durable manifest"), loaded)
}

func TestSnapshotDiskStoreCancelledOperationsPreserveData(t *testing.T) {
	store, err := newSnapshotDiskStore(t.TempDir(), "identity")
	require.NoError(t, err)
	require.NoError(t, store.Save(context.Background(), "page", []byte("durable changes")))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, store.Save(ctx, "page", []byte("replacement changes")), context.Canceled)
	require.ErrorIs(t, store.Delete(ctx, "page"), context.Canceled)
	_, err = store.Load(ctx, "page")
	require.ErrorIs(t, err, context.Canceled)
	_, err = store.Identity(ctx)
	require.ErrorIs(t, err, context.Canceled)
	loaded, err := store.Load(context.Background(), "page")
	require.NoError(t, err)
	assert.Equal(t, []byte("durable changes"), loaded)
}

func TestSnapshotDiskStoreRequiresDirectoryAndIdentity(t *testing.T) {
	_, err := newSnapshotDiskStore("", "identity")
	require.Error(t, err)
	_, err = newSnapshotDiskStore(t.TempDir(), "")
	require.Error(t, err)
	directory := t.TempDir()
	file := filepath.Join(directory, "file")
	require.NoError(t, os.WriteFile(file, []byte("occupied"), 0o600))
	_, err = newSnapshotDiskStore(file, "identity")
	require.Error(t, err)
}
