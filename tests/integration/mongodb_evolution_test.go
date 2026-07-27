//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcmongo "github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// TestMongoDB_SchemaEvolution_EndToEnd validates that the MongoDB source's
// typed-column builder, the schema inferrer, and the cast-on-replay path
// together preserve schema-drift behavior end-to-end.
//
// Scenarios:
//  1. New column appears in a later document (must back-fill nulls)
//  2. Column missing in later docs (must keep column, fill nulls)
//  3. Type changes int → string across batches (final type: string)
//  4. Numeric promotion int → float across batches (final type: float)
//  5. Mixed types within a single batch (typed builder promotes to unknown)
func TestMongoDB_SchemaEvolution_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	mc, err := tcmongo.Run(
		ctx, "mongo:7",
		tcmongo.WithUsername("admin"),
		tcmongo.WithPassword("admin"),
	)
	require.NoError(t, err, "start mongo container")
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(mc) })

	mongoURI, err := mc.ConnectionString(ctx)
	require.NoError(t, err)
	// The driver-issued URI doesn't include /<db>; gong's source extracts
	// the db from the source-table parameter, so this works as-is.

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Disconnect(ctx) })

	db := client.Database("evol")

	tests := []struct {
		name      string
		coll      string
		docs      []any
		pageSize  int
		wantRows  int
		wantTypes map[string]string
		wantCol   map[string][]any // column name -> ordered values per row
	}{
		{
			name: "s1_new_column_appears_mid_stream",
			coll: "s1_new_col",
			docs: []any{
				bson.M{"_id": int64(1), "name": "alice", "age": int64(30)},
				bson.M{"_id": int64(2), "name": "bob", "age": int64(25)},
				bson.M{"_id": int64(3), "name": "carol", "age": int64(28), "email": "carol@x.com"},
				bson.M{"_id": int64(4), "name": "dave", "age": int64(31), "email": "dave@x.com"},
			},
			pageSize: 2, // forces 2 batches so "email" first appears in batch 2
			wantRows: 4,
			wantTypes: map[string]string{
				"_id":   "INTEGER",
				"name":  "TEXT",
				"age":   "INTEGER",
				"email": "TEXT",
			},
			wantCol: map[string][]any{
				"email": {nil, nil, "carol@x.com", "dave@x.com"},
			},
		},
		{
			name: "s2_column_dropped_in_later_docs",
			coll: "s2_dropped_col",
			docs: []any{
				bson.M{"_id": int64(1), "name": "alice", "dept": "eng"},
				bson.M{"_id": int64(2), "name": "bob", "dept": "eng"},
				bson.M{"_id": int64(3), "name": "carol"},
				bson.M{"_id": int64(4), "name": "dave"},
			},
			pageSize: 2,
			wantRows: 4,
			wantTypes: map[string]string{
				"_id":  "INTEGER",
				"name": "TEXT",
				"dept": "TEXT",
			},
			wantCol: map[string][]any{
				"dept": {"eng", "eng", nil, nil},
			},
		},
		{
			name: "s3_int_to_string_across_batches",
			coll: "s3_type_change",
			docs: []any{
				bson.M{"_id": int64(1), "value": int64(10)},
				bson.M{"_id": int64(2), "value": int64(20)},
				bson.M{"_id": int64(3), "value": "thirty"},
				bson.M{"_id": int64(4), "value": "forty"},
			},
			pageSize: 2,
			wantRows: 4,
			wantTypes: map[string]string{
				"_id":   "INTEGER",
				"value": "TEXT",
			},
			wantCol: map[string][]any{
				"value": {"10", "20", "thirty", "forty"},
			},
		},
		{
			name: "s4_int_to_float_across_batches",
			coll: "s4_int_to_float",
			docs: []any{
				bson.M{"_id": int64(1), "value": int64(10)},
				bson.M{"_id": int64(2), "value": int64(20)},
				bson.M{"_id": int64(3), "value": 30.5},
				bson.M{"_id": int64(4), "value": 40.7},
			},
			pageSize: 2,
			wantRows: 4,
			wantTypes: map[string]string{
				"_id":   "INTEGER",
				"value": "REAL",
			},
			wantCol: map[string][]any{
				"value": {10.0, 20.0, 30.5, 40.7},
			},
		},
		{
			name:     "s5_mixed_types_within_one_batch",
			coll:     "s5_mixed_one_batch",
			docs:     mixedBatchDocs(50, 50),
			pageSize: 200, // entire collection fits in one batch
			wantRows: 100,
			wantTypes: map[string]string{
				"_id":   "INTEGER",
				"value": "TEXT",
			},
			// First half: int values JSON-encoded as their decimal string (no quotes).
			// Second half: original strings.
			wantCol: map[string][]any{
				"value": expectedMixedValues(50, 50),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			coll := db.Collection(tc.coll)
			_, err := coll.InsertMany(ctx, tc.docs)
			require.NoError(t, err, "seed mongo collection")

			sqlitePath := filepath.Join(t.TempDir(), tc.coll+".db")
			cfg := &config.IngestConfig{
				SourceURI:           mongoURI,
				SourceTable:         "evol." + tc.coll,
				DestURI:             "sqlite:///" + sqlitePath,
				DestTable:           tc.coll,
				IncrementalStrategy: config.StrategyReplace,
				PageSize:            tc.pageSize,
				Yes:                 true,
			}
			require.NoError(t, pipeline.New(cfg).Run(ctx), "pipeline.Run")

			conn, err := sql.Open("sqlite3", sqlitePath)
			require.NoError(t, err)
			defer func() { _ = conn.Close() }()

			// Row count.
			var got int
			require.NoError(t, conn.QueryRow(
				fmt.Sprintf("SELECT count(*) FROM %s", tc.coll),
			).Scan(&got))
			require.Equal(t, tc.wantRows, got, "row count")

			// Column types.
			rows, err := conn.Query(fmt.Sprintf("PRAGMA table_info(%s)", tc.coll))
			require.NoError(t, err)
			gotTypes := map[string]string{}
			for rows.Next() {
				var cid int
				var name, ctype string
				var notnull, pk int
				var dflt sql.NullString
				require.NoError(t, rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk))
				gotTypes[name] = ctype
			}
			_ = rows.Close()
			for col, want := range tc.wantTypes {
				require.Equalf(t, want, gotTypes[col],
					"column %q type: got %s, want %s", col, gotTypes[col], want)
			}

			// Per-column values, ordered by _id.
			for col, want := range tc.wantCol {
				rows, err := conn.Query(
					fmt.Sprintf("SELECT %s FROM %s ORDER BY _id", col, tc.coll),
				)
				require.NoError(t, err)
				var actual []any
				for rows.Next() {
					var raw any
					require.NoError(t, rows.Scan(&raw))
					actual = append(actual, normalizeSQLite(raw))
				}
				_ = rows.Close()
				require.Equalf(t, want, actual, "column %q values", col)
			}
		})
	}
}

func TestMongoDB_NumericExtractPartitioning_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	mc, err := tcmongo.Run(
		ctx, "mongo:7",
		tcmongo.WithUsername("admin"),
		tcmongo.WithPassword("admin"),
	)
	require.NoError(t, err, "start mongo container")
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(mc) })

	mongoURI, err := mc.ConnectionString(ctx)
	require.NoError(t, err)
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Disconnect(ctx) })

	coll := client.Database("partitioned").Collection("events")
	docs := make([]any, 0, 1002)
	for i := int64(1); i <= 1000; i++ {
		docs = append(docs, bson.M{"id": i, "payload": bson.M{"value": i}})
	}
	docs = append(
		docs,
		bson.M{"id": nil, "payload": bson.M{"value": "null"}},
		bson.M{"payload": bson.M{"value": "missing"}},
	)
	_, err = coll.InsertMany(ctx, docs)
	require.NoError(t, err)
	_, err = coll.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "id", Value: 1}}})
	require.NoError(t, err)

	sqlitePath := filepath.Join(t.TempDir(), "partitioned.db")
	cfg := config.DefaultConfig()
	cfg.SourceURI = mongoURI
	cfg.SourceTable = "partitioned.events"
	cfg.DestURI = "sqlite:///" + sqlitePath
	cfg.DestTable = "events"
	cfg.IncrementalStrategy = config.StrategyAppend
	cfg.ExtractPartitionBy = "id"
	cfg.ExtractPartitionAuto = true
	cfg.ExtractParallelism = 4
	cfg.PageSize = 25
	cfg.Yes = true
	require.NoError(t, pipeline.New(cfg).Run(ctx))

	conn, err := sql.Open("sqlite3", sqlitePath)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	var count, idCount, distinctIDs int
	var idSum int64
	require.NoError(t, conn.QueryRow(`
		SELECT COUNT(*), COUNT(id), COUNT(DISTINCT id), COALESCE(SUM(id), 0)
		FROM events
	`).Scan(&count, &idCount, &distinctIDs, &idSum))
	require.Equal(t, 1002, count)
	require.Equal(t, 1000, idCount)
	require.Equal(t, 1000, distinctIDs)
	require.Equal(t, int64(500500), idSum)
}

func mixedBatchDocs(numInts, numStrings int) []any {
	docs := make([]any, 0, numInts+numStrings)
	for i := range numInts {
		docs = append(docs, bson.M{"_id": int64(i), "value": int64(i)})
	}
	for i := numInts; i < numInts+numStrings; i++ {
		docs = append(docs, bson.M{"_id": int64(i), "value": fmt.Sprintf("str_%d", i)})
	}
	return docs
}

// expectedMixedValues is what we expect each row's "value" column to hold once
// the in-batch promotion + the JSON-decode-on-replay cast have both run. The
// promoted column is JSON-string-encoded internally, but the cast to the
// inferred VARCHAR type unwraps the JSON, so both ints and strings come back
// as their bare textual representation.
func expectedMixedValues(numInts, numStrings int) []any {
	out := make([]any, 0, numInts+numStrings)
	for i := range numInts {
		out = append(out, fmt.Sprintf("%d", i))
	}
	for i := numInts; i < numInts+numStrings; i++ {
		out = append(out, fmt.Sprintf("str_%d", i))
	}
	return out
}

// normalizeSQLite collapses sqlite's []byte / int64 / float64 returns into the
// types our test expectations use: nil, string, int64, float64.
func normalizeSQLite(v any) any {
	switch t := v.(type) {
	case []byte:
		return string(t)
	case nil:
		return nil
	default:
		return v
	}
}
