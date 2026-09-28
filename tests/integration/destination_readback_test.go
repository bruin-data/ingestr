//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/destination/discard"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tcmongo "github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func starRocksConformanceBackend() *sqlBackend {
	b := mysqlBackend()
	b.openDB = func(destURI string) (*sql.DB, error) {
		u, err := url.Parse(destURI)
		if err != nil {
			return nil, err
		}
		u.Scheme = "mysql"
		u.RawQuery = ""
		return sql.Open("mysql", mysqlDSN(u.String()))
	}
	schemaTypes := b.schemaTypes
	b.schemaTypes = func(db *sql.DB, table string) (map[string]string, error) {
		_, name := splitSchemaTable(table, "conformance")
		return schemaTypes(db, name)
	}
	b.countQuery = func(table string) string {
		return "SELECT COUNT(*) FROM `" + strings.ReplaceAll(table, ".", "`.`") + "`"
	}
	b.nameByIDQuery = func(table string, id int) string {
		return fmt.Sprintf("SELECT name FROM `%s` WHERE id=%d", strings.ReplaceAll(table, ".", "`.`"), id)
	}
	b.ageByIDQuery = func(table string, id int) string {
		return fmt.Sprintf("SELECT age FROM `%s` WHERE id=%d", strings.ReplaceAll(table, ".", "`.`"), id)
	}
	return b
}

var starRocksConformance struct {
	sync.Once
	uri     string
	cleanup func()
}

func setupStarRocksConformance(t *testing.T, ctx context.Context) (string, string, func()) {
	t.Helper()
	if !containerWanted("starrocks") {
		t.Skip("StarRocks excluded by INTEGRATION_BACKENDS")
	}
	requireDocker(t)
	starRocksConformance.Do(func() {
		dsn, destURI, cleanup := startStarRocksContainerWithCleanup(ctx, t)
		starRocksConformance.cleanup = cleanup
		waitForStarRocksBackend(t, dsn)
		db, err := sql.Open("mysql", dsn)
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		execEventually(t, db, "CREATE DATABASE IF NOT EXISTS conformance")
		// Alive precedes tablet placement readiness; probe independently of ingestion.
		execEventually(t, db, `CREATE TABLE IF NOT EXISTS conformance.ready (id BIGINT) DUPLICATE KEY(id) DISTRIBUTED BY HASH(id) BUCKETS 1 PROPERTIES ('replication_num'='1')`)
		u, err := url.Parse(destURI)
		require.NoError(t, err)
		u.Path = "/conformance"
		starRocksConformance.uri = u.String()
	})
	require.NotEmpty(t, starRocksConformance.uri, "shared StarRocks setup failed")
	table := "conformance.conformance_" + uniqueSuffix()
	return starRocksConformance.uri, table, func() {
		db, err := starRocksConformanceBackend().openDB(starRocksConformance.uri)
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		_, err = db.Exec("DROP TABLE IF EXISTS " + table)
		require.NoError(t, err)
	}
}

func setupMongoDBConformance(t *testing.T, ctx context.Context) (string, string, func()) {
	t.Helper()
	if !containerWanted("mongodb") {
		t.Skip("MongoDB excluded by INTEGRATION_BACKENDS")
	}
	requireDocker(t)
	c, err := tcmongo.Run(ctx, "mongo:7")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, c.Terminate(context.Background())) })
	u, err := c.ConnectionString(ctx)
	require.NoError(t, err)
	return u, "conformance.rows", func() {}
}

func readJSONLConformance(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	d := json.NewDecoder(f)
	var rows []map[string]any
	for {
		var row map[string]any
		err := d.Decode(&row)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		for name := range row {
			if isIngestrMetadataColumn(name) {
				delete(row, name)
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func validateJSONLReplace(t *testing.T, destURI, _ string) {
	u, err := url.Parse(destURI)
	require.NoError(t, err)
	assert.ElementsMatch(t, readJSONLConformance(t, "testdata/conformance.jsonl"), readJSONLConformance(t, u.Path))
}

func validateJSONLAppend(t *testing.T, destURI, _ string) {
	u, err := url.Parse(destURI)
	require.NoError(t, err)
	want := append(readJSONLConformance(t, "testdata/conformance_append_initial.jsonl"), readJSONLConformance(t, "testdata/conformance_append_more.jsonl")...)
	assert.ElementsMatch(t, want, readJSONLConformance(t, u.Path))
}

func readMongoDBConformance(t *testing.T, destURI, table string) []map[string]any {
	t.Helper()
	ctx := context.Background()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(destURI))
	require.NoError(t, err)
	defer func() { _ = client.Disconnect(ctx) }()
	db, collection := splitSchemaTable(table, "conformance")
	cursor, err := client.Database(db).Collection(collection).Find(ctx, bson.M{})
	require.NoError(t, err)
	defer func() { _ = cursor.Close(ctx) }()
	var docs []bson.M
	require.NoError(t, cursor.All(ctx, &docs))
	var rows []map[string]any
	for _, doc := range docs {
		delete(doc, "_id")
		for name := range doc {
			if isIngestrMetadataColumn(name) {
				delete(doc, name)
			}
		}
		data, err := json.Marshal(doc)
		require.NoError(t, err)
		var row map[string]any
		require.NoError(t, json.Unmarshal(data, &row))
		rows = append(rows, row)
	}
	return rows
}

func validateMongoDBReplace(t *testing.T, destURI, table string) {
	assert.ElementsMatch(t, readJSONLConformance(t, "testdata/conformance.jsonl"), readMongoDBConformance(t, destURI, table))
}

func validateMongoDBAppend(t *testing.T, destURI, table string) {
	want := append(readJSONLConformance(t, "testdata/conformance_append_initial.jsonl"), readJSONLConformance(t, "testdata/conformance_append_more.jsonl")...)
	assert.ElementsMatch(t, want, readMongoDBConformance(t, destURI, table))
}

func TestDestinations_DiscardPipeline(t *testing.T) {
	for _, strategy := range []config.IncrementalStrategy{config.StrategyReplace, config.StrategyAppend} {
		t.Run(string(strategy), func(t *testing.T) {
			cfg := &config.IngestConfig{
				SourceURI: jsonlURI(t, "testdata/conformance.jsonl"), SourceTable: "conformance",
				DestURI: "discard://", DestTable: "conformance", IncrementalStrategy: strategy,
			}
			require.NoError(t, pipeline.New(cfg).Run(context.Background()))
		})
	}
}

// Discard has no readable output. Its observable contract is consuming and
// releasing every batch, and returning upstream errors without swallowing them.
func TestDestinations_DiscardConsumesAndReleases(t *testing.T) {
	for _, parallel := range []bool{false, true} {
		for _, upstreamError := range []bool{false, true} {
			t.Run(fmt.Sprintf("parallel=%t/error=%t", parallel, upstreamError), func(t *testing.T) {
				mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
				defer mem.AssertSize(t, 0)
				rb := array.NewRecordBuilder(mem, arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil))
				defer rb.Release()
				records := make(chan source.RecordBatchResult, 3)
				for _, id := range []int64{7, 19} {
					rb.Field(0).(*array.Int64Builder).Append(id)
					records <- source.RecordBatchResult{Batch: rb.NewRecordBatch()}
				}
				var wantErr error
				if upstreamError {
					wantErr = errors.New("upstream read failed")
					records <- source.RecordBatchResult{Err: wantErr}
				}
				close(records)
				d := discard.NewDiscardDestination()
				write := d.Write
				if parallel {
					write = d.WriteParallel
				}
				err := write(context.Background(), records, destination.WriteOptions{})
				if wantErr != nil {
					require.ErrorIs(t, err, wantErr)
				} else {
					require.NoError(t, err)
				}
				assert.Empty(t, records, "all batches and the final error must be consumed")
			})
		}
	}
}
