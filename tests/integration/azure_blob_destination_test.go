//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	"github.com/bruin-data/ingestr/internal/config"
	_ "github.com/bruin-data/ingestr/pkg/destination/blobstore"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	_ "github.com/bruin-data/ingestr/pkg/source/csv"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const azuriteAccount = "ingestrtest"

func startAzurite(t *testing.T, ctx context.Context) string {
	t.Helper()
	key := base64.StdEncoding.EncodeToString([]byte("ingestr-azurite-synthetic-key"))
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "mcr.microsoft.com/azure-storage/azurite:3.35.0",
			ExposedPorts: []string{"10000/tcp"},
			Env:          map[string]string{"AZURITE_ACCOUNTS": azuriteAccount + ":" + key},
			Cmd:          []string{"azurite-blob", "--blobHost", "0.0.0.0", "--skipApiVersionCheck", "--loose"},
			WaitingFor:   wait.ForListeningPort("10000/tcp").WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "10000")
	require.NoError(t, err)
	return fmt.Sprintf("DefaultEndpointsProtocol=http;AccountName=%s;AccountKey=%s;BlobEndpoint=http://%s:%s/%s;",
		azuriteAccount, key, host, port.Port(), azuriteAccount)
}

func listAzureBlobs(t *testing.T, ctx context.Context, client *azblob.Client, container string) []string {
	t.Helper()
	var names []string
	pager := client.NewListBlobsFlatPager(container, nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		require.NoError(t, err)
		for _, item := range page.Segment.BlobItems {
			names = append(names, *item.Name)
		}
	}
	slices.Sort(names)
	return names
}

func readAzureParquetNames(t *testing.T, ctx context.Context, client *azblob.Client, container, blob string) []string {
	t.Helper()
	resp, err := client.DownloadStream(ctx, container, blob, nil)
	require.NoError(t, err)
	var buf bytes.Buffer
	_, err = buf.ReadFrom(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	tbl, err := pqarrow.ReadTable(ctx, bytes.NewReader(buf.Bytes()), nil, pqarrow.ArrowReadProperties{}, memory.DefaultAllocator)
	require.NoError(t, err)
	defer tbl.Release()
	idx := tbl.Schema().FieldIndices("name")
	require.Len(t, idx, 1)
	var names []string
	for _, chunk := range tbl.Column(idx[0]).Data().Chunks() {
		col := chunk.(*array.String)
		for i := 0; i < col.Len(); i++ {
			names = append(names, col.Value(i))
		}
	}
	return names
}

func TestCSVToAzureBlob_ReplaceAndAppend(t *testing.T) {
	t.Parallel()
	requireDocker(t)
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	connStr := startAzurite(t, ctx)

	client, err := azblob.NewClientFromConnectionString(connStr, nil)
	require.NoError(t, err)
	_, err = client.CreateContainer(ctx, "lake", nil)
	require.NoError(t, err)
	_, err = client.UploadBuffer(ctx, "lake", "exports/users/notes.txt", []byte("keep me"), nil)
	require.NoError(t, err)

	csvPath := filepath.Join(t.TempDir(), "users.csv")
	run := func(body string, strategy config.IncrementalStrategy) {
		t.Helper()
		require.NoError(t, os.WriteFile(csvPath, []byte(body), 0o644))
		cfg := &config.IngestConfig{
			SourceURI:           "csv://" + csvPath,
			SourceTable:         "users",
			DestURI:             "az://lake/exports?connection_string=" + url.QueryEscape(connStr),
			DestTable:           "users",
			IncrementalStrategy: strategy,
		}
		require.NoError(t, cfg.Validate())
		require.NoError(t, pipeline.New(cfg).Run(ctx))
	}
	parquetBlobs := func() []string {
		var out []string
		for _, name := range listAzureBlobs(t, ctx, client, "lake") {
			if strings.HasSuffix(name, ".parquet") {
				require.Regexp(t, `^exports/users/[0-9a-f]{8}\.0\.parquet$`, name)
				out = append(out, name)
			}
		}
		return out
	}

	run("id,name\n1,alice\n2,bob\n", config.StrategyReplace)
	first := parquetBlobs()
	require.Len(t, first, 1)
	require.Equal(t, []string{"alice", "bob"}, readAzureParquetNames(t, ctx, client, "lake", first[0]))

	run("id,name\n3,carol\n", config.StrategyReplace)
	second := parquetBlobs()
	require.Len(t, second, 1)
	require.NotEqual(t, first, second)
	require.Equal(t, []string{"carol"}, readAzureParquetNames(t, ctx, client, "lake", second[0]))
	require.Contains(t, listAzureBlobs(t, ctx, client, "lake"), "exports/users/notes.txt")

	run("id,name\n4,dave\n", config.StrategyAppend)
	appended := parquetBlobs()
	require.Len(t, appended, 2)
	var names []string
	for _, blob := range appended {
		names = append(names, readAzureParquetNames(t, ctx, client, "lake", blob)...)
	}
	slices.Sort(names)
	require.Equal(t, []string{"carol", "dave"}, names)
}
