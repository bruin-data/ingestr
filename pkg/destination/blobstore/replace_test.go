package blobstore

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplaceScope(t *testing.T) {
	tests := []struct {
		name          string
		layout        string
		basePath      string
		wantPrefix    string
		wantRecursive bool
		match         []string
		noMatch       []string
	}{
		{
			name:       "default layout lists the table folder only",
			layout:     "{load_id}.{file_id}.{ext}",
			basePath:   "exports/orders",
			wantPrefix: "exports/orders/",
			match:      []string{"a1b2c3d4.0.parquet", "a1b2c3d4.12.parquet"},
			noMatch:    []string{"notes.txt", "a1b2c3d4.0.csv", "nested/a1b2c3d4.0.parquet", "A1B2C3D4.0.parquet"},
		},
		{
			name:       "static folder in layout is part of the prefix",
			layout:     "{table_name}/{load_id}.{file_id}.{ext}",
			basePath:   "exports",
			wantPrefix: "exports/users/",
			match:      []string{"users/a1b2c3d4.0.parquet"},
			noMatch:    []string{"orders/a1b2c3d4.0.parquet", "a1b2c3d4.0.parquet"},
		},
		{
			name:          "per-load folder lists recursively",
			layout:        "{load_id}/{file_id}.{ext}",
			basePath:      "exports/users",
			wantPrefix:    "exports/users/",
			wantRecursive: true,
			match:         []string{"a1b2c3d4/0.parquet"},
			noMatch:       []string{"a1b2c3d4/sub/0.parquet", "other/0.parquet"},
		},
		{
			name:          "static folder before a per-load folder is part of the prefix",
			layout:        "exports/{load_id}/{file_id}.{ext}",
			wantPrefix:    "exports/",
			wantRecursive: true,
			match:         []string{"exports/a1b2c3d4/0.parquet"},
			noMatch:       []string{"a1b2c3d4/0.parquet", "exports/other/0.parquet"},
		},
		{
			name:       "table at container root has no prefix",
			layout:     "{load_id}.{file_id}.{ext}",
			wantPrefix: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &BlobstoreDestination{layout: tt.layout}
			rendered := d.renderLayout("lake/"+tt.basePath+"/users", loadIDToken, fileIDToken)
			prefix, recursive, pattern := replaceScope(rendered, tt.basePath)
			assert.Equal(t, tt.wantPrefix, prefix)
			assert.Equal(t, tt.wantRecursive, recursive)
			for _, key := range tt.match {
				assert.True(t, pattern.MatchString(key), key)
			}
			for _, key := range tt.noMatch {
				assert.False(t, pattern.MatchString(key), key)
			}
		})
	}
}

func TestPrepareReplaceSkipsContainerRoot(t *testing.T) {
	d := &BlobstoreDestination{provider: ProviderAzureDatalake, layout: "{load_id}.{file_id}.{ext}"}
	require.NoError(t, d.prepareReplace(t.Context(), "lake"))
	assert.Empty(t, d.replaces)
}

func TestFinalizeReplaceKeepsRewrittenFiles(t *testing.T) {
	d := &BlobstoreDestination{
		provider: ProviderAzure,
		replaces: map[string]*replaceState{
			"lake/users": {
				bucket:   "lake",
				previous: []string{"users/users.parquet"},
				written:  map[string]struct{}{},
			},
		},
	}
	d.markWritten("lake/users", "users/users.parquet")

	require.NoError(t, d.FinalizeReplace(t.Context(), "lake/users"))
	assert.Empty(t, d.replaces)
}

func TestFinalizeReplaceReportsDeleteFailure(t *testing.T) {
	d := &BlobstoreDestination{
		provider: Provider("unknown"),
		replaces: map[string]*replaceState{
			"lake/users": {bucket: "lake", previous: []string{"users/a1b2c3d4.0.parquet"}, written: map[string]struct{}{}},
		},
	}

	err := d.FinalizeReplace(t.Context(), "lake/users")
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "use the append strategy"), err.Error())
	assert.NoError(t, d.FinalizeReplace(t.Context(), "lake/users"))
}
