package blobstore

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeS3 is an in-memory, path-style S3 server covering list and batch delete.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string]bool // "bucket/key"
	lists   int
}

func newFakeS3(t *testing.T, keys ...string) (*fakeS3, string) {
	t.Helper()
	f := &fakeS3{objects: map[string]bool{}}
	for _, k := range keys {
		f.objects[k] = true
	}
	server := httptest.NewServer(f)
	t.Cleanup(server.Close)
	return f, server.URL
}

func (f *fakeS3) remaining() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var keys []string
	for k := range f.objects {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	bucket := strings.Trim(r.URL.Path, "/")
	switch {
	case r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2":
		f.lists++
		prefix := r.URL.Query().Get("prefix")
		var b strings.Builder
		b.WriteString(`<ListBucketResult><IsTruncated>false</IsTruncated>`)
		for k := range f.objects {
			if key, ok := strings.CutPrefix(k, bucket+"/"); ok && strings.HasPrefix(key, prefix) {
				fmt.Fprintf(&b, `<Contents><Key>%s</Key></Contents>`, key)
			}
		}
		b.WriteString(`</ListBucketResult>`)
		_, _ = io.WriteString(w, b.String())
	case r.Method == http.MethodPost && r.URL.Query().Has("delete"):
		var req struct {
			Objects []struct{ Key string } `xml:"Object"`
		}
		if err := xml.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		for _, o := range req.Objects {
			delete(f.objects, bucket+"/"+o.Key)
		}
		_, _ = io.WriteString(w, `<DeleteResult></DeleteResult>`)
	default:
		http.Error(w, "unsupported", http.StatusNotImplemented)
	}
}

func connectFakeS3(t *testing.T, endpoint string) *BlobstoreDestination {
	t.Helper()
	d := NewBlobstoreDestination()
	require.NoError(t, d.Connect(t.Context(), "s3://?access_key_id=a&secret_access_key=b&endpoint_url="+endpoint))
	return d
}

func TestParseBlobstoreURI_BucketAndPrefix(t *testing.T) {
	for _, tt := range []struct {
		uri, bucket, prefix string
	}{
		{"s3://?access_key_id=a", "", ""},
		{"s3://my-bucket?access_key_id=a", "my-bucket", ""},
		{"s3://my-bucket/exports/daily/?access_key_id=a", "my-bucket", "exports/daily"},
		{"gs://my-bucket/exports", "my-bucket", "exports"},
		{"az://?account_name=acct", "", ""},
	} {
		parsed, err := parseBlobstoreURI(tt.uri)
		require.NoError(t, err, tt.uri)
		assert.Equal(t, tt.bucket, parsed.bucket, tt.uri)
		assert.Equal(t, tt.prefix, parsed.prefix, tt.uri)
	}
}

func TestResolveLocation(t *testing.T) {
	for _, tt := range []struct {
		uriBucket, uriPrefix, table, wantBucket, wantPath string
	}{
		// No bucket in the URI: the table is bucket/path, as before.
		{"", "", "my-bucket/reports/t", "my-bucket", "reports/t"},
		// Bucket in the URI: the table is a path under the bucket and prefix.
		{"my-bucket", "exports", "public.users", "my-bucket", "exports/public.users"},
		{"my-bucket", "", "reports/t", "my-bucket", "reports/t"},
		// A table that repeats the URI's bucket keeps the bucket/path form.
		{"my-bucket", "exports", "my-bucket/reports/t", "my-bucket", "reports/t"},
	} {
		d := &BlobstoreDestination{uriBucket: tt.uriBucket, uriPrefix: tt.uriPrefix}
		bucket, path := d.resolveLocation(tt.table)
		assert.Equal(t, tt.wantBucket, bucket, tt.table)
		assert.Equal(t, tt.wantPath, path, tt.table)
	}
}

func TestRemoveStaleBlobs_DeletesOnlyOldFilesUnderTablePath(t *testing.T) {
	fake, endpoint := newFakeS3(t,
		"b/reports/t/old.0.parquet",
		"b/reports/t/nested/old.1.parquet",
		"b/reports/t/new.0.parquet",
		"b/reports/t2/keep.parquet",
		"b/reports/notes.txt",
		"other/reports/t/keep.parquet",
	)
	d := connectFakeS3(t, endpoint)
	require.NoError(t, d.PrepareTable(t.Context(), destination.PrepareOptions{Table: "b/reports/t", DropFirst: true}))
	d.writtenKeys["reports/t/new.0.parquet"] = struct{}{}

	require.NoError(t, d.removeStaleBlobs(t.Context()))
	assert.Equal(t, []string{
		"b/reports/notes.txt",
		"b/reports/t/new.0.parquet",
		"b/reports/t2/keep.parquet",
		"other/reports/t/keep.parquet",
	}, fake.remaining())
}

func TestRemoveStaleBlobs_NeverClearsBucketRoot(t *testing.T) {
	fake, endpoint := newFakeS3(t, "b/data.parquet")
	d := connectFakeS3(t, endpoint)
	require.NoError(t, d.PrepareTable(t.Context(), destination.PrepareOptions{Table: "b", DropFirst: true}))

	require.NoError(t, d.removeStaleBlobs(t.Context()))
	assert.Equal(t, []string{"b/data.parquet"}, fake.remaining())
	assert.Zero(t, fake.lists, "the bucket root must not even be listed")
}

func TestWriteParallel_FailedReplaceKeepsPreviousFiles(t *testing.T) {
	fake, endpoint := newFakeS3(t, "b/reports/t/old.0.parquet")
	d := connectFakeS3(t, endpoint)
	require.NoError(t, d.PrepareTable(t.Context(), destination.PrepareOptions{Table: "b/reports/t", DropFirst: true}))

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Err: errors.New("source failed")}
	close(records)

	require.EqualError(t, d.WriteParallel(t.Context(), records, destination.WriteOptions{}), "source failed")
	assert.Equal(t, []string{"b/reports/t/old.0.parquet"}, fake.remaining())
}

func TestWriteParallel_AppendKeepsPreviousFiles(t *testing.T) {
	fake, endpoint := newFakeS3(t, "b/reports/t/old.0.parquet")
	d := connectFakeS3(t, endpoint)
	require.NoError(t, d.PrepareTable(t.Context(), destination.PrepareOptions{Table: "b/reports/t", DropFirst: false}))

	records := make(chan source.RecordBatchResult)
	close(records)

	require.NoError(t, d.WriteParallel(t.Context(), records, destination.WriteOptions{}))
	assert.Equal(t, []string{"b/reports/t/old.0.parquet"}, fake.remaining())
	assert.Zero(t, fake.lists)
}
