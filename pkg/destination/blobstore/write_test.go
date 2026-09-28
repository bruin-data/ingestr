package blobstore

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"cloud.google.com/go/storage"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	datalakedirectory "github.com/Azure/azure-sdk-for-go/sdk/storage/azdatalake/directory"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/strategy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"
)

// These HTTP fakes exercise the real cloud SDKs without cloud credentials or data.
func TestCloudWriteReplaceAndAppend(t *testing.T) {
	for _, provider := range []Provider{ProviderS3, ProviderGCS, ProviderAzure} {
		for _, layout := range []string{"{load_id}.{file_id}.{ext}", "{table_name}/partition=1/{load_id}.{ext}"} {
			t.Run(string(provider)+"/"+layout, func(t *testing.T) {
				var mu sync.Mutex
				var failMethod string
				objects := map[string][]byte{"records-old/keep.parquet": []byte("sibling"), "unrelated": []byte("keep")}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					defer mu.Unlock()
					if r.Method == failMethod {
						w.WriteHeader(http.StatusForbidden)
						return
					}
					key := strings.TrimPrefix(r.URL.Path, "/bucket/")
					if provider == ProviderGCS {
						key = strings.TrimPrefix(r.URL.Path, "/storage/v1/b/bucket/o/")
					}
					switch r.Method {
					case http.MethodGet:
						prefix := r.URL.Query().Get("prefix")
						marker := r.URL.Query().Get("continuation-token") + r.URL.Query().Get("pageToken") + r.URL.Query().Get("marker")
						var keys []string
						for name := range objects {
							if strings.HasPrefix(name, prefix) && name > marker {
								keys = append(keys, name)
							}
						}
						sort.Strings(keys)
						var next string
						if len(keys) > 1 {
							keys = keys[:1]
							next = keys[0]
						}
						if provider == ProviderGCS {
							items := make([]map[string]string, 0, len(keys))
							for _, name := range keys {
								items = append(items, map[string]string{"name": name})
							}
							w.Header().Set("Content-Type", "application/json")
							_ = json.NewEncoder(w).Encode(map[string]interface{}{"items": items, "nextPageToken": next})
						} else {
							w.Header().Set("Content-Type", "application/xml")
							if provider == ProviderS3 {
								_, _ = io.WriteString(w, "<ListBucketResult>")
							} else {
								_, _ = io.WriteString(w, "<EnumerationResults><Blobs>")
							}
							for _, name := range keys {
								var escaped bytes.Buffer
								_ = xml.EscapeText(&escaped, []byte(name))
								if provider == ProviderS3 {
									_, _ = fmt.Fprintf(w, "<Contents><Key>%s</Key></Contents>", escaped.String())
								} else {
									_, _ = fmt.Fprintf(w, "<Blob><Name>%s</Name></Blob>", escaped.String())
								}
							}
							if provider == ProviderS3 {
								_, _ = fmt.Fprintf(w, "<IsTruncated>%t</IsTruncated><NextContinuationToken>%s</NextContinuationToken></ListBucketResult>", next != "", next)
							} else {
								_, _ = fmt.Fprintf(w, "</Blobs><NextMarker>%s</NextMarker></EnumerationResults>", next)
							}
						}
					case http.MethodDelete:
						delete(objects, key)
						if provider == ProviderAzure {
							w.WriteHeader(http.StatusAccepted)
						} else {
							w.WriteHeader(http.StatusNoContent)
						}
					case http.MethodPut, http.MethodPost:
						data, err := io.ReadAll(r.Body)
						if err != nil {
							t.Error(err)
							w.WriteHeader(400)
							return
						}
						if provider == ProviderGCS {
							_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
							if err != nil {
								t.Error(err)
								w.WriteHeader(400)
								return
							}
							reader := multipart.NewReader(bytes.NewReader(data), params["boundary"])
							part, err := reader.NextPart()
							if err != nil {
								t.Error(err)
								w.WriteHeader(400)
								return
							}
							var metadata struct {
								Name string `json:"name"`
							}
							if err := json.NewDecoder(part).Decode(&metadata); err != nil {
								t.Error(err)
								w.WriteHeader(400)
								return
							}
							part, err = reader.NextPart()
							if err != nil {
								t.Error(err)
								w.WriteHeader(400)
								return
							}
							data, err = io.ReadAll(part)
							if err != nil {
								t.Error(err)
								w.WriteHeader(400)
								return
							}
							key = metadata.Name
						}
						objects[key] = data
						if provider == ProviderGCS {
							w.Header().Set("Content-Type", "application/json")
							_, _ = fmt.Fprintf(w, `{"name":%q,"bucket":"bucket"}`, key)
						} else {
							w.WriteHeader(http.StatusCreated)
						}
					default:
						t.Errorf("unexpected request: %s %s", r.Method, r.URL)
						w.WriteHeader(400)
					}
				}))
				defer server.Close()
				ctx := context.Background()
				d := &BlobstoreDestination{provider: provider, layout: layout}
				var err error
				switch provider {
				case ProviderS3:
					d.s3Client, err = createS3Client(ctx, &parsedBlobstoreURI{endpointURL: server.URL, accessKeyID: "test", secretAccessKey: "test"})
				case ProviderGCS:
					d.gcsClient, err = storage.NewClient(ctx, option.WithEndpoint(server.URL+"/storage/v1/"), option.WithoutAuthentication(), option.WithHTTPClient(server.Client()))
				case ProviderAzure:
					d.azureClient, err = azblob.NewClientWithNoCredential(server.URL, nil)
				}
				require.NoError(t, err)
				defer func() { require.NoError(t, d.Close(ctx)) }()
				write := func(drop bool, values ...int64) {
					records := make(chan source.RecordBatchResult, 1)
					if len(values) > 0 {
						builder := array.NewInt64Builder(memory.DefaultAllocator)
						builder.AppendValues(values, nil)
						col := builder.NewArray()
						builder.Release()
						records <- source.RecordBatchResult{Batch: array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil), []arrow.Array{col}, int64(len(values)))}
						col.Release()
					}
					close(records)
					job := &strategy.IngestionJob{
						Config:          &config.IngestConfig{DestTable: "bucket/records/"},
						Destination:     d,
						Schema:          &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}},
						BufferedRecords: records,
					}
					if drop {
						require.NoError(t, (&strategy.ReplaceStrategy{}).Execute(ctx, job))
					} else {
						require.NoError(t, (&strategy.AppendStrategy{}).Execute(ctx, job))
					}
				}
				check := func(want []int64) {
					mu.Lock()
					defer mu.Unlock()
					require.Equal(t, []byte("sibling"), objects["records-old/keep.parquet"])
					require.Equal(t, []byte("keep"), objects["unrelated"])
					var got []int64
					for key, data := range objects {
						if !strings.HasPrefix(key, "records/") {
							continue
						}
						table, err := pqarrow.ReadTable(ctx, bytes.NewReader(data), nil, pqarrow.ArrowReadProperties{}, memory.DefaultAllocator)
						require.NoError(t, err)
						for _, chunk := range table.Column(0).Data().Chunks() {
							got = append(got, chunk.(*array.Int64).Int64Values()...)
						}
						table.Release()
					}
					sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
					require.Equal(t, want, got)
				}
				write(true, 1, 2, 3)
				check([]int64{1, 2, 3})
				write(true, 9)
				check([]int64{9})
				write(false, 12, 13)
				check([]int64{9, 12, 13})
				write(true)
				check(nil)
				for _, method := range []string{http.MethodGet, http.MethodDelete} {
					mu.Lock()
					objects["records/old.parquet"] = []byte("old")
					failMethod = method
					mu.Unlock()
					require.Error(t, d.PrepareTable(ctx, destination.PrepareOptions{Table: "bucket/records", DropFirst: true}))
					mu.Lock()
					require.Equal(t, []byte("old"), objects["records/old.parquet"])
					mu.Unlock()
				}
				mu.Lock()
				failMethod = http.MethodPut
				if provider == ProviderGCS {
					failMethod = http.MethodPost
				}
				mu.Unlock()
				require.Error(t, d.writeBlob(ctx, "failed.parquet", []byte("test")))
			})
		}
	}
}

func TestReplaceRejectsBucketRoot(t *testing.T) {
	for _, table := range []string{"bucket", "bucket/", "bucket///", "/records"} {
		d := NewBlobstoreDestination()
		require.ErrorContains(t, d.PrepareTable(context.Background(), destination.PrepareOptions{Table: table, DropFirst: true}), "requires a non-empty destination path")
	}
}

func TestADLSReplaceDirectory(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNotFound, http.StatusForbidden} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				assert.Equal(t, http.MethodDelete, r.Method)
				assert.Equal(t, "/bucket/records", r.URL.Path)
				assert.Equal(t, "true", r.URL.Query().Get("recursive"))
				if status == http.StatusNotFound {
					w.Header().Set("x-ms-error-code", "PathNotFound")
				}
				w.WriteHeader(status)
			}))
			defer server.Close()
			d := &BlobstoreDestination{provider: ProviderAzureDatalake, adlsClient: &azureDatalakeClient{
				accountName: "test",
				newDirectoryClient: func(path string) (*datalakedirectory.Client, error) {
					require.Equal(t, "https://test.dfs.core.windows.net/bucket/records", path)
					return datalakedirectory.NewClientWithNoCredential(server.URL+"/bucket/records", nil)
				},
			}}
			err := d.PrepareTable(context.Background(), destination.PrepareOptions{Table: "bucket/records/", DropFirst: true})
			if status == http.StatusForbidden {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, int32(1), calls.Load())
		})
	}
}
