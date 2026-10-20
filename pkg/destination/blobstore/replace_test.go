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

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeS3 is an in-memory, path-style S3 server.
type fakeS3 struct {
	mu            sync.Mutex
	objects       map[string]bool // "bucket/key"
	lists         int
	batchDeletes  int
	singleDeletes int
	denyList      bool
	denyDelete    bool
	listPrefixes  []string
	delimiters    []string
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

func (f *fakeS3) put(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[key] = true
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

// forbidden answers like an S3-compatible store or proxy that does not use the AccessDenied code.
func forbidden(w http.ResponseWriter) {
	w.WriteHeader(http.StatusForbidden)
	_, _ = io.WriteString(w, `<Error><Code>Forbidden</Code><Message>no</Message></Error>`)
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()

	bucket, key, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	switch {
	case r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2":
		f.lists++
		if f.denyList {
			forbidden(w)
			return
		}
		prefix := r.URL.Query().Get("prefix")
		delimiter := r.URL.Query().Get("delimiter")
		f.listPrefixes = append(f.listPrefixes, prefix)
		f.delimiters = append(f.delimiters, delimiter)
		var b strings.Builder
		b.WriteString(`<ListBucketResult><IsTruncated>false</IsTruncated>`)
		for k := range f.objects {
			key, ok := strings.CutPrefix(k, bucket+"/")
			if ok && delimiter != "" && strings.Contains(strings.TrimPrefix(key, prefix), delimiter) {
				continue
			}
			if ok && strings.HasPrefix(key, prefix) {
				fmt.Fprintf(&b, `<Contents><Key>%s</Key></Contents>`, key)
			}
		}
		b.WriteString(`</ListBucketResult>`)
		_, _ = io.WriteString(w, b.String())
	case r.Method == http.MethodPost && r.URL.Query().Has("delete"):
		f.batchDeletes++
		var req struct {
			Objects []struct{ Key string } `xml:"Object"`
		}
		if err := xml.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var b strings.Builder
		b.WriteString(`<DeleteResult>`)
		for _, o := range req.Objects {
			if f.denyDelete {
				fmt.Fprintf(&b, `<Error><Key>%s</Key><Code>AccessDenied</Code><Message>Access Denied</Message></Error>`, o.Key)
				continue
			}
			delete(f.objects, bucket+"/"+o.Key)
		}
		b.WriteString(`</DeleteResult>`)
		_, _ = io.WriteString(w, b.String())
	case r.Method == http.MethodDelete:
		f.singleDeletes++
		if f.denyDelete {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		delete(f.objects, bucket+"/"+key)
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPut:
		f.objects[bucket+"/"+key] = true
	default:
		http.Error(w, "unsupported", http.StatusNotImplemented)
	}
}

func connectFakeS3(t *testing.T, endpoint, location string) *BlobstoreDestination {
	t.Helper()
	d := NewBlobstoreDestination()
	require.NoError(t, d.Connect(t.Context(), "s3://"+location+"?access_key_id=a&secret_access_key=b&endpoint_url="+endpoint))
	return d
}

func oneRow(column string) <-chan source.RecordBatchResult {
	b := array.NewRecordBuilder(memory.DefaultAllocator, arrow.NewSchema([]arrow.Field{{Name: column, Type: arrow.PrimitiveTypes.Int64, Nullable: true}}, nil))
	defer b.Release()
	b.Field(0).(*array.Int64Builder).Append(1)
	ch := make(chan source.RecordBatchResult, 1)
	ch <- source.RecordBatchResult{Batch: b.NewRecordBatch()}
	close(ch)
	return ch
}

func noRows() <-chan source.RecordBatchResult {
	ch := make(chan source.RecordBatchResult)
	close(ch)
	return ch
}

func replace(t *testing.T, d *BlobstoreDestination, table string, records <-chan source.RecordBatchResult) {
	t.Helper()
	require.NoError(t, d.PrepareTable(t.Context(), destination.PrepareOptions{Table: table, DropFirst: true}))
	require.NoError(t, d.WriteParallel(t.Context(), records, destination.WriteOptions{Table: table}))
	require.NoError(t, d.FinalizeReplace(t.Context(), table))
}

// newFiles returns the keys under prefix that are not in before.
func newFiles(f *fakeS3, prefix string, before ...string) []string {
	var keys []string
	for _, k := range f.remaining() {
		if strings.HasPrefix(k, prefix) && !slices.Contains(before, k) {
			keys = append(keys, k)
		}
	}
	return keys
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
		wantAmbiguous                                     bool
	}{
		{"", "", "my-bucket/reports/t", "my-bucket", "reports/t", false},
		{"my-bucket", "exports", "public.users", "my-bucket", "exports/public.users", false},
		{"my-bucket", "", "reports/t", "my-bucket", "reports/t", false},
		{"my-bucket", "", "my-bucket/reports/t", "my-bucket", "reports/t", false},
		// It could also mean my-bucket/exports/my-bucket/reports/t.
		{"my-bucket", "exports", "my-bucket/reports/t", "my-bucket", "reports/t", true},
		// A table named after the bucket is a path, not the bucket root.
		{"logs", "", "logs", "logs", "logs", false},
		{"logs", "exports", "logs", "logs", "exports/logs", false},
	} {
		d := &BlobstoreDestination{uriBucket: tt.uriBucket, uriPrefix: tt.uriPrefix}
		bucket, path, ambiguous := d.resolveLocation(tt.table)
		assert.Equal(t, tt.wantBucket, bucket, tt.table)
		assert.Equal(t, tt.wantPath, path, tt.table)
		assert.Equal(t, tt.wantAmbiguous, ambiguous, tt.table)
	}
}

func TestLayoutPattern(t *testing.T) {
	for _, tt := range []struct {
		layout, table  string
		match, noMatch []string
	}{
		{
			layout:  "{load_id}.{file_id}.{ext}",
			table:   "b/reports/users",
			match:   []string{"0a1b2c3d.0.parquet", "0a1b2c3d.12.parquet"},
			noMatch: []string{"notes.txt", "nested/0a1b2c3d.0.parquet", "0a1b2c3d.0.parquet.bak", "0A1B2C3D.0.parquet", "0a1b2c3.0.parquet"},
		},
		{
			layout:  "{table_name}/{load_id}.{file_id}.{ext}",
			table:   "b/reports/users",
			match:   []string{"users/0a1b2c3d.0.parquet"},
			noMatch: []string{"orders/0a1b2c3d.0.parquet", "0a1b2c3d.0.parquet"},
		},
		{
			layout:  "{table_name}.{ext}",
			table:   "public.users",
			match:   []string{"users.parquet"},
			noMatch: []string{"usersXparquet", "orders.parquet"},
		},
	} {
		pattern := layoutPattern(layoutReplacer(tt.table, loadIDToken, fileIDToken).Replace(tt.layout))
		for _, key := range tt.match {
			assert.True(t, pattern.MatchString(key), "%s should match %s", tt.layout, key)
		}
		for _, key := range tt.noMatch {
			assert.False(t, pattern.MatchString(key), "%s should not match %s", tt.layout, key)
		}
	}
}

func TestReplace_RemovesOnlyPreviousFilesOfTheTable(t *testing.T) {
	keep := []string{
		"b/reports/0a1b2c3d.0.parquet",
		"b/reports/t/nested/0a1b2c3d.0.parquet",
		"b/reports/t/notes.txt",
		"b/reports/t2/0a1b2c3d.0.parquet",
		"other/reports/t/0a1b2c3d.0.parquet",
	}
	fake, endpoint := newFakeS3(t, append([]string{"b/reports/t/0a1b2c3d.0.parquet", "b/reports/t/0a1b2c3d.1.parquet"}, keep...)...)
	d := connectFakeS3(t, endpoint, "")

	replace(t, d, "b/reports/t", oneRow("id"))

	written := newFiles(fake, "b/reports/t/", keep...)
	require.Len(t, written, 1)
	assert.ElementsMatch(t, append(keep, written[0]), fake.remaining())
}

func TestReplace_KeepsFilesFromOverlappingLoad(t *testing.T) {
	fake, endpoint := newFakeS3(t, "b/reports/t/0a1b2c3d.0.parquet")
	d := connectFakeS3(t, endpoint, "")
	require.NoError(t, d.PrepareTable(t.Context(), destination.PrepareOptions{Table: "b/reports/t", DropFirst: true}))

	// Another replace on the same path writes its file while this load is running.
	fake.put("b/reports/t/ffffffff.0.parquet")
	require.NoError(t, d.WriteParallel(t.Context(), noRows(), destination.WriteOptions{Table: "b/reports/t"}))
	require.NoError(t, d.FinalizeReplace(t.Context(), "b/reports/t"))

	assert.Equal(t, []string{"b/reports/t/ffffffff.0.parquet"}, fake.remaining())
}

func TestReplace_UsesSameKeysAsUploads(t *testing.T) {
	// "b//reports/t" uploads keys starting with "/reports/t/"; cleanup must not touch "reports/t/".
	fake, endpoint := newFakeS3(t, "b//reports/t/0a1b2c3d.0.parquet", "b/reports/t/0a1b2c3d.0.parquet")
	d := connectFakeS3(t, endpoint, "")

	replace(t, d, "b//reports/t", noRows())

	assert.Equal(t, []string{"b/reports/t/0a1b2c3d.0.parquet"}, fake.remaining())
}

func TestReplace_NeverClearsBucketRoot(t *testing.T) {
	fake, endpoint := newFakeS3(t, "b/0a1b2c3d.0.parquet")
	d := connectFakeS3(t, endpoint, "")

	replace(t, d, "b", noRows())

	assert.Equal(t, []string{"b/0a1b2c3d.0.parquet"}, fake.remaining())
	assert.Zero(t, fake.lists, "the bucket root must not even be listed")
}

func TestReplace_AmbiguousTableKeepsFiles(t *testing.T) {
	fake, endpoint := newFakeS3(t, "b/reports/t/0a1b2c3d.0.parquet", "b/exports/b/reports/t/0a1b2c3d.0.parquet")
	d := connectFakeS3(t, endpoint, "b/exports")

	replace(t, d, "b/reports/t", noRows())

	assert.Equal(t, []string{"b/exports/b/reports/t/0a1b2c3d.0.parquet", "b/reports/t/0a1b2c3d.0.parquet"}, fake.remaining())
	assert.Zero(t, fake.lists)
}

func TestReplace_MultipleTablesAtOnce(t *testing.T) {
	previous := []string{"b/exports/t1/0a1b2c3d.0.parquet", "b/exports/t2/0a1b2c3d.0.parquet"}
	fake, endpoint := newFakeS3(t, previous...)
	d := connectFakeS3(t, endpoint, "b/exports")

	// Each table has its own schema; a shared one would make the parquet writer reject rows.
	tables := map[string]string{"t1": "a", "t2": "b"}
	var wg sync.WaitGroup
	for table, column := range tables {
		wg.Go(func() {
			assert.NoError(t, d.PrepareTable(t.Context(), destination.PrepareOptions{
				Table:     table,
				Schema:    &schema.TableSchema{Columns: []schema.Column{{Name: column, DataType: schema.TypeInt64, Nullable: true}}},
				DropFirst: true,
			}))
		})
	}
	wg.Wait()
	for table, column := range tables {
		wg.Go(func() {
			assert.NoError(t, d.WriteParallel(t.Context(), oneRow(column), destination.WriteOptions{Table: table}))
			assert.NoError(t, d.FinalizeReplace(t.Context(), table))
		})
	}
	wg.Wait()

	assert.Len(t, newFiles(fake, "b/exports/t1/", previous...), 1)
	assert.Len(t, newFiles(fake, "b/exports/t2/", previous...), 1)
	assert.Len(t, fake.remaining(), 2)
}

func TestReplace_FailedLoadKeepsPreviousFiles(t *testing.T) {
	fake, endpoint := newFakeS3(t, "b/reports/t/0a1b2c3d.0.parquet")
	d := connectFakeS3(t, endpoint, "")
	require.NoError(t, d.PrepareTable(t.Context(), destination.PrepareOptions{Table: "b/reports/t", DropFirst: true}))

	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Err: errors.New("source failed")}
	close(records)

	require.EqualError(t, d.WriteParallel(t.Context(), records, destination.WriteOptions{Table: "b/reports/t"}), "source failed")
	assert.Equal(t, []string{"b/reports/t/0a1b2c3d.0.parquet"}, fake.remaining())
}

func TestAppend_KeepsPreviousFiles(t *testing.T) {
	fake, endpoint := newFakeS3(t, "b/reports/t/0a1b2c3d.0.parquet")
	d := connectFakeS3(t, endpoint, "")
	require.NoError(t, d.PrepareTable(t.Context(), destination.PrepareOptions{Table: "b/reports/t"}))

	require.NoError(t, d.WriteParallel(t.Context(), oneRow("id"), destination.WriteOptions{Table: "b/reports/t"}))

	assert.Len(t, fake.remaining(), 2)
	assert.Zero(t, fake.lists)
	assert.Nil(t, d.table("b/reports/t").writtenKeys)
}

func TestReplace_WithoutPermissionFails(t *testing.T) {
	for _, tt := range []struct {
		name                 string
		denyList, denyDelete bool
		customEndpoint       bool
	}{
		{name: "list", denyList: true, customEndpoint: true},
		{name: "delete object", denyDelete: true, customEndpoint: true},
		{name: "batch delete", denyDelete: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fake, endpoint := newFakeS3(t, "b/reports/t/0a1b2c3d.0.parquet")
			fake.denyList, fake.denyDelete = tt.denyList, tt.denyDelete
			d := connectFakeS3(t, endpoint, "")
			d.customEndpoint = tt.customEndpoint

			err := d.PrepareTable(t.Context(), destination.PrepareOptions{Table: "b/reports/t", DropFirst: true})
			if err == nil {
				require.NoError(t, d.WriteParallel(t.Context(), noRows(), destination.WriteOptions{Table: "b/reports/t"}))
				err = d.FinalizeReplace(t.Context(), "b/reports/t")
			}

			require.ErrorContains(t, err, "replace needs list and delete permission")
			assert.Equal(t, []string{"b/reports/t/0a1b2c3d.0.parquet"}, fake.remaining())
		})
	}
}

func TestReplace_DeletesPerObjectOnlyOnCustomEndpoints(t *testing.T) {
	keys := []string{"b/reports/t/0a1b2c3d.0.parquet", "b/reports/t/0a1b2c3d.1.parquet"}

	fake, endpoint := newFakeS3(t, keys...)
	replace(t, connectFakeS3(t, endpoint, ""), "b/reports/t", noRows())
	assert.Empty(t, fake.remaining())
	assert.Equal(t, 2, fake.singleDeletes)
	assert.Zero(t, fake.batchDeletes)

	fake, endpoint = newFakeS3(t, keys...)
	d := connectFakeS3(t, endpoint, "")
	d.customEndpoint = false
	replace(t, d, "b/reports/t", noRows())
	assert.Empty(t, fake.remaining())
	assert.Zero(t, fake.singleDeletes)
	assert.Equal(t, 1, fake.batchDeletes)
}

func TestWriteParallel_NeverDeletes(t *testing.T) {
	fake, endpoint := newFakeS3(t, "b/reports/t/0a1b2c3d.0.parquet")
	d := connectFakeS3(t, endpoint, "")
	require.NoError(t, d.PrepareTable(t.Context(), destination.PrepareOptions{Table: "b/reports/t", DropFirst: true}))

	// The truncate-boundary writer, used by CDC and multi-table loads, reports a source
	// error after the data channel closed normally, so WriteParallel sees a clean end.
	records := make(chan source.RecordBatchResult, 2)
	records <- <-oneRow("id")
	records <- source.RecordBatchResult{Err: errors.New("source failed")}
	close(records)
	_, err := destination.WriteWithTruncateBoundaries(t.Context(), d, records, destination.WriteOptions{Table: "b/reports/t"})
	require.EqualError(t, err, "source failed")

	assert.Contains(t, fake.remaining(), "b/reports/t/0a1b2c3d.0.parquet")
	assert.Len(t, fake.remaining(), 2)
	assert.Zero(t, fake.singleDeletes+fake.batchDeletes)
}

func TestReplace_ListsOnlyTheLayoutFolder(t *testing.T) {
	for _, tt := range []struct {
		layout, wantPrefix, wantDelimiter string
	}{
		{"{load_id}.{file_id}.{ext}", "reports/t/", "/"},
		{"{table_name}/{load_id}.{file_id}.{ext}", "reports/t/t/", "/"},
		// The folder changes per load, so subfolders have to be listed.
		{"{load_id}/{file_id}.{ext}", "reports/t/", ""},
	} {
		fake, endpoint := newFakeS3(t)
		d := connectFakeS3(t, endpoint, "")
		d.layout = tt.layout

		require.NoError(t, d.PrepareTable(t.Context(), destination.PrepareOptions{Table: "b/reports/t", DropFirst: true}))

		assert.Equal(t, []string{tt.wantPrefix}, fake.listPrefixes, tt.layout)
		assert.Equal(t, []string{tt.wantDelimiter}, fake.delimiters, tt.layout)
	}
}

func TestReplace_NestedLayoutRemovesPreviousFiles(t *testing.T) {
	fake, endpoint := newFakeS3(t, "b/reports/t/0a1b2c3d/0.parquet", "b/reports/t/0a1b2c3d/notes.txt")
	d := connectFakeS3(t, endpoint, "")
	d.layout = "{load_id}/{file_id}.{ext}"

	replace(t, d, "b/reports/t", noRows())

	assert.Equal(t, []string{"b/reports/t/0a1b2c3d/notes.txt"}, fake.remaining())
}

func TestReplace_TableFolderInBucketRootIsCleared(t *testing.T) {
	fake, endpoint := newFakeS3(t, "b/b/0a1b2c3d.0.parquet", "b/0a1b2c3d.0.parquet")
	d := connectFakeS3(t, endpoint, "")
	d.layout = "{table_name}/{load_id}.{file_id}.{ext}"

	replace(t, d, "b", noRows())

	assert.Equal(t, []string{"b/0a1b2c3d.0.parquet"}, fake.remaining())
}

func TestReplace_EndpointFromEnvironmentDeletesPerObject(t *testing.T) {
	fake, endpoint := newFakeS3(t, "b/reports/t/0a1b2c3d.0.parquet")
	t.Setenv("AWS_ENDPOINT_URL_S3", endpoint)
	d := NewBlobstoreDestination()
	require.NoError(t, d.Connect(t.Context(), "s3://?access_key_id=a&secret_access_key=b"))
	d.s3Client = s3.New(d.s3Client.Options(), func(o *s3.Options) { o.UsePathStyle = true })

	replace(t, d, "b/reports/t", noRows())

	assert.Empty(t, fake.remaining())
	assert.Equal(t, 1, fake.singleDeletes)
	assert.Zero(t, fake.batchDeletes)
}

func TestDeleteS3Batches_ReportsEveryErrorCode(t *testing.T) {
	fake, endpoint := newFakeS3(t, "b/k1", "b/k2")
	fake.denyDelete = true
	d := connectFakeS3(t, endpoint, "")

	err := d.deleteS3Batches(t.Context(), "b", []string{"k1", "k2"})

	require.ErrorContains(t, err, "failed to delete 2 files in s3://b (codes: AccessDenied)")
}
