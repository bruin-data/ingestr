//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcmongo "github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func setupMongoDBCDCContainer(t *testing.T, ctx context.Context) (*tcmongo.MongoDBContainer, string) {
	t.Helper()

	container, err := tcmongo.Run(
		ctx,
		"mongo:7",
		tcmongo.WithReplicaSet("rs0"),
	)
	require.NoError(t, err)

	mongoURI, err := container.ConnectionString(ctx)
	require.NoError(t, err)
	u, err := url.Parse(mongoURI)
	require.NoError(t, err)
	q := u.Query()
	q.Set("directConnection", "true")
	u.RawQuery = q.Encode()

	return container, u.String()
}

func mongodbCDCURI(t *testing.T, baseURI, database string, params map[string]string) string {
	t.Helper()

	u, err := url.Parse(baseURI)
	require.NoError(t, err)
	switch u.Scheme {
	case "mongodb":
		u.Scheme = "mongodb+cdc"
	case "mongodb+srv":
		u.Scheme = "mongodb+srv+cdc"
	default:
		t.Fatalf("unexpected MongoDB URI scheme: %s", u.Scheme)
	}
	u.Path = "/" + database

	q := u.Query()
	for key, value := range params {
		q.Set(key, value)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func TestMongoDBCDC_SnapshotAndIncremental_SQLite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	container, mongoURI := setupMongoDBCDCContainer(t, ctx)
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Disconnect(ctx) })

	coll := client.Database("cdc").Collection("items")
	_, err = coll.InsertMany(ctx, []any{
		bson.M{"_id": int64(1), "name": "item1", "value": int64(100)},
		bson.M{"_id": int64(2), "name": "item2", "value": int64(200)},
		bson.M{"_id": int64(3), "name": "item3", "value": int64(300)},
	})
	require.NoError(t, err)

	sqlitePath := filepath.Join(t.TempDir(), "mongodb_cdc.db")
	cfg := &config.IngestConfig{
		SourceURI:   mongodbCDCURI(t, mongoURI, "cdc", map[string]string{"mode": "batch", "max_await_time": "500ms"}),
		SourceTable: "cdc.items",
		DestURI:     "sqlite:///" + sqlitePath,
		DestTable:   "items_dest",
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	dest, err := sql.Open("sqlite3", sqlitePath)
	require.NoError(t, err)
	defer func() { _ = dest.Close() }()

	count := func(where string) int {
		t.Helper()
		query := `SELECT COUNT(*) FROM items_dest`
		if where != "" {
			query += ` WHERE ` + where
		}
		var n int
		require.NoError(t, dest.QueryRow(query).Scan(&n))
		return n
	}

	assert.Equal(t, 3, count(""))
	assert.Equal(t, 3, count(`"_cdc_deleted" = 0`))

	_, err = coll.InsertOne(ctx, bson.M{"_id": int64(4), "name": "item4", "value": int64(400)})
	require.NoError(t, err)
	_, err = coll.UpdateOne(ctx, bson.M{"_id": int64(1)}, bson.M{"$set": bson.M{"value": int64(150)}})
	require.NoError(t, err)
	_, err = coll.DeleteOne(ctx, bson.M{"_id": int64(2)})
	require.NoError(t, err)
	_, err = coll.UpdateOne(ctx, bson.M{"_id": int64(3)}, bson.M{"$set": bson.M{"name": "item3_final", "value": int64(999)}})
	require.NoError(t, err)
	_, err = coll.DeleteOne(ctx, bson.M{"_id": int64(3)})
	require.NoError(t, err)

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	assert.Equal(t, 4, count(""))
	assert.Equal(t, 1, count(`"_id" = 1 AND value = 150 AND "_cdc_deleted" = 0`))
	assert.Equal(t, 1, count(`"_id" = 2 AND value = 200 AND "_cdc_deleted" = 1`))
	assert.Equal(t, 1, count(`"_id" = 3 AND "_cdc_deleted" = 1`))
	assert.Equal(t, 1, count(`"_id" = 4 AND name = 'item4' AND value = 400 AND "_cdc_deleted" = 0`))
	assert.Greater(t, count(`"_cdc_lsn" IS NOT NULL`), 0)
}

func TestMongoDBCDC_MultiTableSQLite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	container, mongoURI := setupMongoDBCDCContainer(t, ctx)
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Disconnect(ctx) })

	db := client.Database("cdc_multi")
	_, err = db.Collection("users").InsertMany(ctx, []any{
		bson.M{"_id": int64(1), "email": "a@example.com"},
		bson.M{"_id": int64(2), "email": "b@example.com"},
	})
	require.NoError(t, err)
	_, err = db.Collection("orders").InsertMany(ctx, []any{
		bson.M{"_id": int64(10), "user_id": int64(1), "total": int64(25)},
		bson.M{"_id": int64(11), "user_id": int64(2), "total": int64(40)},
		bson.M{"_id": int64(12), "user_id": int64(2), "total": int64(60)},
	})
	require.NoError(t, err)

	sqlitePath := filepath.Join(t.TempDir(), "mongodb_cdc_multi.db")
	cfg := &config.IngestConfig{
		SourceURI: mongodbCDCURI(t, mongoURI, "cdc_multi", map[string]string{"mode": "batch", "max_await_time": "500ms"}),
		DestURI:   "sqlite:///" + sqlitePath,
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	dest, err := sql.Open("sqlite3", sqlitePath)
	require.NoError(t, err)
	defer func() { _ = dest.Close() }()

	tableCount := func(table string) int {
		t.Helper()
		var n int
		require.NoError(t, dest.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE "_cdc_deleted" = 0`, table)).Scan(&n))
		return n
	}

	assert.Equal(t, 2, tableCount("users"))
	assert.Equal(t, 3, tableCount("orders"))
}

func TestMongoDBCDC_ZeroSchemaSampleSQLite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	container, mongoURI := setupMongoDBCDCContainer(t, ctx)
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Disconnect(ctx) })

	coll := client.Database("cdc_zero_sample").Collection("items")
	_, err = coll.InsertOne(ctx, bson.M{"_id": int64(1), "name": "item1", "value": int64(100)})
	require.NoError(t, err)

	sqlitePath := filepath.Join(t.TempDir(), "mongodb_cdc_zero_sample.db")
	cfg := &config.IngestConfig{
		SourceURI:   mongodbCDCURI(t, mongoURI, "cdc_zero_sample", map[string]string{"mode": "batch", "schema_sample_size": "0", "max_await_time": "500ms"}),
		SourceTable: "cdc_zero_sample.items",
		DestURI:     "sqlite:///" + sqlitePath,
		DestTable:   "items_dest",
	}
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	dest, err := sql.Open("sqlite3", sqlitePath)
	require.NoError(t, err)
	defer func() { _ = dest.Close() }()

	columns := map[string]bool{}
	rows, err := dest.Query(`PRAGMA table_info(items_dest)`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		require.NoError(t, rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk))
		columns[name] = true
	}
	require.NoError(t, rows.Err())

	assert.True(t, columns["_id"])
	assert.True(t, columns["_cdc_lsn"])
	assert.False(t, columns["name"])
	assert.False(t, columns["value"])

	var count int
	require.NoError(t, dest.QueryRow(`SELECT COUNT(*) FROM items_dest WHERE "_cdc_deleted" = 0`).Scan(&count))
	assert.Equal(t, 1, count)
}
