package blobstore

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"cloud.google.com/go/storage"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	datalakedirectory "github.com/Azure/azure-sdk-for-go/sdk/storage/azdatalake/directory"
	datalakefile "github.com/Azure/azure-sdk-for-go/sdk/storage/azdatalake/file"
	datalakefilesystem "github.com/Azure/azure-sdk-for-go/sdk/storage/azdatalake/filesystem"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/strategy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"
)

const fakeStorePageSize = 2

// fakeObjectStore serves the S3, GCS, Azure Blob and ADLS Gen2 calls the destination makes,
// for one bucket named "lake". Directories are implicit except on ADLS.
type fakeObjectStore struct {
	mu         sync.Mutex
	provider   Provider
	files      map[string]bool
	data       map[string][]byte
	dirs       map[string]bool
	denyList   bool
	denyDelete bool
}

func newFakeObjectStore(files ...string) *fakeObjectStore {
	s := &fakeObjectStore{files: map[string]bool{}, data: map[string][]byte{}, dirs: map[string]bool{}}
	for _, f := range files {
		s.addFile(f)
	}
	return s
}

func (s *fakeObjectStore) addFile(name string) {
	s.files[name] = true
	for dir := parentDirectory(name); dir != ""; dir = parentDirectory(dir) {
		s.dirs[dir] = true
	}
}

func (s *fakeObjectStore) list() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for f := range s.files {
		out = append(out, f)
	}
	slices.Sort(out)
	return out
}

func (s *fakeObjectStore) fail(w http.ResponseWriter, status int, code string) {
	w.Header().Set("x-ms-error-code", code)
	w.WriteHeader(status)
}

func (s *fakeObjectStore) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch s.provider {
	case ProviderS3:
		s.serveS3(w, r)
	case ProviderGCS:
		s.serveGCS(w, r)
	default:
		s.serveAzure(w, r)
	}
}

func (s *fakeObjectStore) listNames(prefix, delimiter string) []string {
	var names []string
	for f := range s.files {
		if strings.HasPrefix(f, prefix) && (delimiter == "" || !strings.Contains(strings.TrimPrefix(f, prefix), delimiter)) {
			names = append(names, f)
		}
	}
	return names
}

func (s *fakeObjectStore) serveS3(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	name, _ := url.PathUnescape(strings.TrimPrefix(strings.TrimPrefix(r.URL.EscapedPath(), "/lake"), "/"))
	denied := func() {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "<Error><Code>AccessDenied</Code><Message>denied</Message></Error>")
	}

	switch {
	case r.Method == http.MethodGet && q.Get("list-type") == "2":
		if s.denyList {
			denied()
			return
		}
		names, next := s.page(s.listNames(q.Get("prefix"), q.Get("delimiter")), q.Get("continuation-token"))
		type object struct {
			Key string `xml:"Key"`
		}
		resp := struct {
			XMLName               xml.Name `xml:"ListBucketResult"`
			IsTruncated           bool     `xml:"IsTruncated"`
			NextContinuationToken string   `xml:"NextContinuationToken,omitempty"`
			Contents              []object `xml:"Contents"`
		}{IsTruncated: next != "", NextContinuationToken: next}
		for _, n := range names {
			resp.Contents = append(resp.Contents, object{Key: n})
		}
		_ = xml.NewEncoder(w).Encode(resp)
	case r.Method == http.MethodPut:
		s.addFile(name)
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodDelete:
		if s.denyDelete {
			denied()
			return
		}
		delete(s.files, name)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}

func (s *fakeObjectStore) serveGCS(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	path := r.URL.EscapedPath()
	gcsError := func(status int) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": status, "message": http.StatusText(status)}})
	}

	switch {
	case r.Method == http.MethodGet && path == "/storage/v1/b/lake/o":
		if s.denyList {
			gcsError(http.StatusForbidden)
			return
		}
		names, next := s.page(s.listNames(q.Get("prefix"), q.Get("delimiter")), q.Get("pageToken"))
		var items []map[string]string
		for _, n := range names {
			items = append(items, map[string]string{"name": n, "bucket": "lake"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "nextPageToken": next})
	case r.Method == http.MethodPost && path == "/upload/storage/v1/b/lake/o":
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			gcsError(http.StatusBadRequest)
			return
		}
		part, err := multipart.NewReader(r.Body, params["boundary"]).NextPart()
		if err != nil {
			gcsError(http.StatusBadRequest)
			return
		}
		var meta struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(part).Decode(&meta)
		s.addFile(meta.Name)
		_ = json.NewEncoder(w).Encode(map[string]string{"name": meta.Name, "bucket": "lake"})
	case r.Method == http.MethodDelete && strings.HasPrefix(path, "/storage/v1/b/lake/o/"):
		name, _ := url.PathUnescape(strings.TrimPrefix(path, "/storage/v1/b/lake/o/"))
		switch {
		case s.denyDelete:
			gcsError(http.StatusForbidden)
		case !s.files[name]:
			gcsError(http.StatusNotFound)
		default:
			delete(s.files, name)
			w.WriteHeader(http.StatusNoContent)
		}
	default:
		gcsError(http.StatusBadRequest)
	}
}

func (s *fakeObjectStore) serveAzure(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	name, _ := url.PathUnescape(strings.TrimPrefix(strings.TrimPrefix(r.URL.EscapedPath(), "/lake"), "/"))

	switch {
	case r.Method == http.MethodGet && q.Get("comp") == "list":
		if s.denyList {
			s.fail(w, http.StatusForbidden, "AuthorizationPermissionMismatch")
			return
		}
		s.serveBlobList(w, q)
	case r.Method == http.MethodGet && q.Get("resource") == "filesystem":
		if s.denyList {
			s.fail(w, http.StatusForbidden, "AuthorizationPermissionMismatch")
			return
		}
		s.servePathList(w, q)
	case r.Method == http.MethodPut && q.Get("resource") == "directory":
		s.dirs[name] = true
		w.WriteHeader(http.StatusCreated)
	case r.Method == http.MethodPut && q.Get("resource") == "file":
		if s.files[name] {
			s.fail(w, http.StatusConflict, "PathAlreadyExists")
			return
		}
		s.addFile(name)
		w.WriteHeader(http.StatusCreated)
	case r.Method == http.MethodPut:
		s.addFile(name)
		s.data[name], _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
	case r.Method == http.MethodPatch && q.Get("action") == "flush":
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPatch:
		w.WriteHeader(http.StatusAccepted)
	case r.Method == http.MethodDelete:
		switch {
		case s.denyDelete:
			s.fail(w, http.StatusForbidden, "AuthorizationPermissionMismatch")
		case !s.files[name]:
			s.fail(w, http.StatusNotFound, "BlobNotFound")
		case s.provider == ProviderAzureDatalake:
			delete(s.files, name)
			w.WriteHeader(http.StatusOK)
		default:
			delete(s.files, name)
			w.WriteHeader(http.StatusAccepted)
		}
	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}

func (s *fakeObjectStore) page(names []string, marker string) ([]string, string) {
	slices.Sort(names)
	start, _ := strconv.Atoi(marker)
	end := min(start+fakeStorePageSize, len(names))
	next := ""
	if end < len(names) {
		next = strconv.Itoa(end)
	}
	return names[start:end], next
}

func (s *fakeObjectStore) serveBlobList(w http.ResponseWriter, q url.Values) {
	names, next := s.page(s.listNames(q.Get("prefix"), q.Get("delimiter")), q.Get("marker"))

	type blob struct {
		Name string `xml:"Name"`
	}
	resp := struct {
		XMLName    xml.Name `xml:"EnumerationResults"`
		Blobs      []blob   `xml:"Blobs>Blob"`
		NextMarker string   `xml:"NextMarker"`
	}{NextMarker: next}
	for _, n := range names {
		resp.Blobs = append(resp.Blobs, blob{Name: n})
	}
	w.Header().Set("Content-Type", "application/xml")
	_ = xml.NewEncoder(w).Encode(resp)
}

func (s *fakeObjectStore) servePathList(w http.ResponseWriter, q url.Values) {
	dir, recursive := q.Get("directory"), q.Get("recursive") == "true"
	if !s.dirs[dir] {
		s.fail(w, http.StatusNotFound, "PathNotFound")
		return
	}
	var names []string
	for _, set := range []map[string]bool{s.files, s.dirs} {
		for p := range set {
			rest, ok := strings.CutPrefix(p, dir+"/")
			if ok && (recursive || !strings.Contains(rest, "/")) {
				names = append(names, p)
			}
		}
	}
	names, next := s.page(names, q.Get("continuation"))
	if next != "" {
		w.Header().Set("x-ms-continuation", next)
	}

	type path struct {
		Name        string `json:"name"`
		IsDirectory string `json:"isDirectory,omitempty"`
	}
	var paths []path
	for _, n := range names {
		p := path{Name: n}
		if s.dirs[n] {
			p.IsDirectory = "true"
		}
		paths = append(paths, p)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"paths": paths})
}

func newFakeObjectDestination(t *testing.T, provider Provider, layout string, store *fakeObjectStore) *BlobstoreDestination {
	t.Helper()
	srv := httptest.NewServer(store)
	t.Cleanup(srv.Close)

	store.provider = provider
	d := &BlobstoreDestination{provider: provider, layout: layout}
	switch provider {
	case ProviderS3:
		d.s3Client = s3.New(s3.Options{
			Region:       "us-east-1",
			BaseEndpoint: aws.String(srv.URL),
			UsePathStyle: true,
			Credentials:  aws.AnonymousCredentials{},
		})
	case ProviderGCS:
		client, err := storage.NewClient(t.Context(), option.WithEndpoint(srv.URL+"/storage/v1/"), option.WithoutAuthentication())
		require.NoError(t, err)
		t.Cleanup(func() { _ = client.Close() })
		d.gcsClient = client
	case ProviderAzure:
		client, err := azblob.NewClientWithNoCredential(srv.URL+"/", nil)
		require.NoError(t, err)
		d.azureClient = client
	case ProviderAzureDatalake:
		local := func(u string) string { return strings.Replace(u, "https://acct.dfs.core.windows.net", srv.URL, 1) }
		d.adlsClient = &azureDatalakeClient{
			accountName: "acct",
			newFileClient: func(u string) (*datalakefile.Client, error) {
				return datalakefile.NewClientWithNoCredential(local(u), nil)
			},
			newDirectoryClient: func(u string) (*datalakedirectory.Client, error) {
				return datalakedirectory.NewClientWithNoCredential(local(u), nil)
			},
			newFileSystemClient: func(u string) (*datalakefilesystem.Client, error) {
				return datalakefilesystem.NewClientWithNoCredential(local(u), nil)
			},
		}
	}
	return d
}

var flowSchema = &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}

func flowRecords(err error) <-chan source.RecordBatchResult {
	b := array.NewInt64Builder(memory.DefaultAllocator)
	defer b.Release()
	b.AppendValues([]int64{1, 2, 3}, nil)
	col := b.NewArray()
	defer col.Release()

	ch := make(chan source.RecordBatchResult, 2)
	ch <- source.RecordBatchResult{Batch: array.NewRecordBatch(flowSchema.ToArrowSchema(), []arrow.Array{col}, 3)}
	if err != nil {
		ch <- source.RecordBatchResult{Err: err}
	}
	close(ch)
	return ch
}

func runFlow(t *testing.T, d *BlobstoreDestination, strat config.IncrementalStrategy, table string, sourceErr error) error {
	t.Helper()
	job := &strategy.IngestionJob{
		Config:          &config.IngestConfig{DestTable: table},
		Destination:     d,
		Schema:          flowSchema,
		SourceSchema:    flowSchema,
		BufferedRecords: flowRecords(sourceErr),
	}
	if strat == config.StrategyAppend {
		return (&strategy.AppendStrategy{}).Execute(t.Context(), job)
	}
	return (&strategy.ReplaceStrategy{}).Execute(t.Context(), job)
}

// newFiles returns the files in after that are not in before.
func newFiles(before, after []string) []string {
	var out []string
	for _, f := range after {
		if !slices.Contains(before, f) {
			out = append(out, f)
		}
	}
	return out
}

func TestReplaceFlow(t *testing.T) {
	kept := []string{
		"exports/orders/day=18/a1b2c3d4.0.parquet",
		"exports/orders/day=190/a1b2c3d4.0.parquet",
		"exports/orders/day=19/notes.txt",
		"exports/orders/day=19/a1b2c3d4.0.csv",
		"exports/orders/day=19/A1B2C3D4.0.parquet",
		"exports/orders/day=19/nested/a1b2c3d4.0.parquet",
		"exports/orders/day=19/a1b2c3d4.parquet",
	}
	stale := []string{
		"exports/orders/day=19/a1b2c3d4.0.parquet",
		"exports/orders/day=19/a1b2c3d4.1.parquet",
		"exports/orders/day=19/0f0f0f0f.0.parquet",
	}
	defaultLayout := "{load_id}.{file_id}.{ext}"
	table := "lake/exports/orders/day=19"

	for _, provider := range []Provider{ProviderS3, ProviderGCS, ProviderAzure, ProviderAzureDatalake} {
		t.Run(string(provider), func(t *testing.T) {
			t.Run("replace removes only the previous load files of the target folder", func(t *testing.T) {
				store := newFakeObjectStore(append(slices.Clone(kept), stale...)...)
				d := newFakeObjectDestination(t, provider, defaultLayout, store)

				require.NoError(t, runFlow(t, d, config.StrategyReplace, table, nil))
				first := newFiles(kept, store.list())
				require.Len(t, first, 1)
				assert.Regexp(t, `^exports/orders/day=19/[0-9a-f]{8}\.0\.parquet$`, first[0])
				assert.Subset(t, store.list(), kept)

				require.NoError(t, runFlow(t, d, config.StrategyReplace, table, nil))
				second := newFiles(kept, store.list())
				require.Len(t, second, 1)
				assert.NotEqual(t, first, second)
				assert.Subset(t, store.list(), kept)
			})

			t.Run("replace into a new folder", func(t *testing.T) {
				store := newFakeObjectStore(kept...)
				d := newFakeObjectDestination(t, provider, defaultLayout, store)

				require.NoError(t, runFlow(t, d, config.StrategyReplace, "lake/exports/orders/day=20", nil))
				added := newFiles(kept, store.list())
				require.Len(t, added, 1)
				assert.True(t, strings.HasPrefix(added[0], "exports/orders/day=20/"))
			})

			t.Run("failed load keeps the previous files", func(t *testing.T) {
				store := newFakeObjectStore(stale...)
				d := newFakeObjectDestination(t, provider, defaultLayout, store)

				err := runFlow(t, d, config.StrategyReplace, table, errors.New("source failed"))
				require.ErrorContains(t, err, "source failed")
				assert.Equal(t, sorted(stale), store.list())
			})

			t.Run("append keeps the previous files", func(t *testing.T) {
				store := newFakeObjectStore(stale...)
				d := newFakeObjectDestination(t, provider, defaultLayout, store)

				require.NoError(t, runFlow(t, d, config.StrategyAppend, table, nil))
				assert.Subset(t, store.list(), stale)
				assert.Len(t, store.list(), len(stale)+1)
			})

			t.Run("table name layout only matches its own folder", func(t *testing.T) {
				store := newFakeObjectStore(
					"exports/orders/orders/a1b2c3d4.0.parquet",
					"exports/orders/users/a1b2c3d4.0.parquet",
					"exports/orders/a1b2c3d4.0.parquet",
				)
				d := newFakeObjectDestination(t, provider, "{table_name}/{load_id}.{file_id}.{ext}", store)

				require.NoError(t, runFlow(t, d, config.StrategyReplace, "lake/exports/orders", nil))
				files := store.list()
				assert.NotContains(t, files, "exports/orders/orders/a1b2c3d4.0.parquet")
				assert.Contains(t, files, "exports/orders/users/a1b2c3d4.0.parquet")
				assert.Contains(t, files, "exports/orders/a1b2c3d4.0.parquet")
				assert.Len(t, files, 3)
			})

			t.Run("per-load folder layout removes previous load folders", func(t *testing.T) {
				store := newFakeObjectStore(
					"exports/orders/a1b2c3d4/0.parquet",
					"exports/orders/a1b2c3d4/1.parquet",
					"exports/orders/a1b2c3d4/sub/0.parquet",
					"exports/orders/manual/0.parquet",
				)
				d := newFakeObjectDestination(t, provider, "{load_id}/{file_id}.{ext}", store)

				require.NoError(t, runFlow(t, d, config.StrategyReplace, "lake/exports/orders", nil))
				files := store.list()
				assert.Len(t, files, 3)
				assert.Contains(t, files, "exports/orders/a1b2c3d4/sub/0.parquet")
				assert.Contains(t, files, "exports/orders/manual/0.parquet")
			})

			t.Run("container table with a static layout folder is cleaned", func(t *testing.T) {
				store := newFakeObjectStore("exports/a1b2c3d4/0.parquet", "exports/manual/0.parquet", "a1b2c3d4/0.parquet")
				d := newFakeObjectDestination(t, provider, "exports/{load_id}/{file_id}.{ext}", store)

				require.NoError(t, runFlow(t, d, config.StrategyReplace, "lake", nil))
				files := store.list()
				assert.Len(t, files, 3)
				assert.NotContains(t, files, "exports/a1b2c3d4/0.parquet")
				assert.Contains(t, files, "exports/manual/0.parquet")
				assert.Contains(t, files, "a1b2c3d4/0.parquet")
			})

			t.Run("fixed file name layout keeps the rewritten file", func(t *testing.T) {
				store := newFakeObjectStore("exports/orders.parquet")
				d := newFakeObjectDestination(t, provider, "{table_name}.{ext}", store)

				require.NoError(t, runFlow(t, d, config.StrategyReplace, "lake/exports/orders", nil))
				assert.Equal(t, []string{"exports/orders.parquet", "exports/orders/orders.parquet"}, store.list())

				require.NoError(t, runFlow(t, d, config.StrategyReplace, "lake/exports/orders", nil))
				assert.Equal(t, []string{"exports/orders.parquet", "exports/orders/orders.parquet"}, store.list())
			})

			t.Run("container root is never cleared", func(t *testing.T) {
				store := newFakeObjectStore("a1b2c3d4.0.parquet")
				d := newFakeObjectDestination(t, provider, defaultLayout, store)

				require.NoError(t, runFlow(t, d, config.StrategyReplace, "lake", nil))
				assert.Contains(t, store.list(), "a1b2c3d4.0.parquet")
				assert.Len(t, store.list(), 2)
			})

			t.Run("missing list permission fails before writing", func(t *testing.T) {
				store := newFakeObjectStore(stale...)
				store.denyList = true
				d := newFakeObjectDestination(t, provider, defaultLayout, store)

				err := runFlow(t, d, config.StrategyReplace, table, nil)
				require.ErrorContains(t, err, "use the append strategy")
				assert.Equal(t, sorted(stale), store.list())
			})

			t.Run("missing delete permission fails the load", func(t *testing.T) {
				store := newFakeObjectStore(stale...)
				store.denyDelete = true
				d := newFakeObjectDestination(t, provider, defaultLayout, store)

				err := runFlow(t, d, config.StrategyReplace, table, nil)
				require.ErrorContains(t, err, "use the append strategy")
				assert.Subset(t, store.list(), stale)
			})

			t.Run("concurrent tables keep their own folders", func(t *testing.T) {
				store := newFakeObjectStore("exports/orders/a1b2c3d4.0.parquet", "exports/users/a1b2c3d4.0.parquet")
				d := newFakeObjectDestination(t, provider, defaultLayout, store)
				tables := []string{"lake/exports/orders", "lake/exports/users"}

				for _, tbl := range tables {
					require.NoError(t, d.PrepareTable(t.Context(), destination.PrepareOptions{Table: tbl, Schema: flowSchema, DropFirst: true}))
				}
				var wg sync.WaitGroup
				for _, tbl := range tables {
					wg.Go(func() {
						assert.NoError(t, d.WriteParallel(t.Context(), flowRecords(nil), destination.WriteOptions{Table: tbl}))
					})
				}
				wg.Wait()
				for _, tbl := range tables {
					require.NoError(t, d.FinalizeReplace(t.Context(), tbl))
				}

				files := store.list()
				require.Len(t, files, 2)
				assert.True(t, strings.HasPrefix(files[0], "exports/orders/"))
				assert.True(t, strings.HasPrefix(files[1], "exports/users/"))
				assert.NotContains(t, files, "exports/orders/a1b2c3d4.0.parquet")
				assert.NotContains(t, files, "exports/users/a1b2c3d4.0.parquet")
			})
		})
	}
}

func TestConcurrentTablesUseTheirOwnSchema(t *testing.T) {
	store := newFakeObjectStore()
	d := newFakeObjectDestination(t, ProviderAzure, "{load_id}.{file_id}.{ext}", store)
	usersSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "name", DataType: schema.TypeString}}}

	b := array.NewStringBuilder(memory.DefaultAllocator)
	defer b.Release()
	b.AppendValues([]string{"a", "b"}, nil)
	col := b.NewArray()
	defer col.Release()
	users := make(chan source.RecordBatchResult, 1)
	users <- source.RecordBatchResult{Batch: array.NewRecordBatch(usersSchema.ToArrowSchema(), []arrow.Array{col}, 2)}
	close(users)

	require.NoError(t, d.PrepareTable(t.Context(), destination.PrepareOptions{Table: "lake/orders", Schema: flowSchema, DropFirst: true}))
	require.NoError(t, d.PrepareTable(t.Context(), destination.PrepareOptions{Table: "lake/users", Schema: usersSchema, DropFirst: true}))

	var wg sync.WaitGroup
	wg.Go(func() {
		assert.NoError(t, d.WriteParallel(t.Context(), flowRecords(nil), destination.WriteOptions{Table: "lake/orders", Schema: flowSchema}))
	})
	wg.Go(func() {
		assert.NoError(t, d.WriteParallel(t.Context(), users, destination.WriteOptions{Table: "lake/users", Schema: usersSchema}))
	})
	wg.Wait()

	columns := map[string]string{}
	for _, f := range store.list() {
		tbl, err := pqarrow.ReadTable(t.Context(), bytes.NewReader(store.data[f]), nil, pqarrow.ArrowReadProperties{}, memory.DefaultAllocator)
		require.NoError(t, err)
		columns[strings.Split(f, "/")[0]] = tbl.Schema().Field(0).Name
		tbl.Release()
	}
	assert.Equal(t, map[string]string{"orders": "id", "users": "name"}, columns)
}

func sorted(files []string) []string {
	out := slices.Clone(files)
	slices.Sort(out)
	return out
}
