//go:build integration

package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcmongo "github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// TestMongoDBToBigQuery_PreStageEquivalence runs the full user-facing path:
// a schema-less MongoDB collection with realistic document shapes (ObjectIDs,
// nested documents, arrays, BSON datetimes, missing fields) merged into
// BigQuery, once with extract-time pre-staging and once with the replay path,
// asserting identical results.
func TestMongoDBToBigQuery_PreStageEquivalence(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	env := bigqueryPreStageEnv(t)

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

	coll := client.Database("prestage").Collection("policies")
	baseTime := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	docs := make([]any, 0, 10)
	for i := 1; i <= 10; i++ {
		doc := bson.M{
			"_id":        primitive.NewObjectID(),
			"policyId":   int64(i),
			"holderName": fmt.Sprintf("holder-%d", i),
			"premium":    float64(i) * 10.5,
			"active":     i%2 == 0,
			"createdAt":  primitive.NewDateTimeFromTime(baseTime.Add(time.Duration(i) * time.Hour)),
			"details": bson.M{
				"tier":    i % 3,
				"regions": bson.A{"eu", "us"},
			},
		}
		if i > 7 {
			doc["riskScore"] = float64(i) / 2 // column appears mid-extract
		}
		docs = append(docs, doc)
	}
	_, err = coll.InsertMany(ctx, docs)
	require.NoError(t, err)

	suffix := uniqueSuffix()
	tables := map[bool]string{
		false: "prestage_mongo_on_" + suffix,
		true:  "prestage_mongo_off_" + suffix,
	}

	for disable, table := range tables {
		t.Cleanup(func() { dropBQTable(ctx, t, env, table) })
		runBQIngest(ctx, t, &config.IngestConfig{
			SourceURI:           mongoURI,
			SourceTable:         "prestage.policies",
			DestURI:             env.uri,
			DestTable:           env.dataset + "." + table,
			IncrementalStrategy: config.StrategyMerge,
			PrimaryKeys:         []string{"_id"},
			PageSize:            3, // several batches; riskScore appears in a later batch
			LoaderFileSize:      3,
			DisablePreStaging:   disable,
		})
	}

	preStagedRows := bqRowsWithoutLoadTS(ctx, t, env, tables[false], "policy_id")
	replayRows := bqRowsWithoutLoadTS(ctx, t, env, tables[true], "policy_id")

	require.Len(t, preStagedRows, 10)
	require.Equal(t, replayRows, preStagedRows, "MongoDB pre-staged load must match the replay path")

	first := preStagedRows[0]
	require.Contains(t, first, "holder_name")
	require.Equal(t, "holder-1", first["holder_name"])
	require.Contains(t, first, "created_at")
	require.NotEmpty(t, first["created_at"])
	require.Contains(t, first, "details")

	last := preStagedRows[9]
	require.Equal(t, float64(5), last["risk_score"])
	if _, hasEarlyRisk := first["risk_score"]; hasEarlyRisk {
		require.Nil(t, first["risk_score"], "early rows must have NULL risk_score")
	}
}
