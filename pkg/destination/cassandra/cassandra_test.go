package cassandra

import (
	"strings"
	"testing"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/stretchr/testify/require"
)

func TestManagedCDCStateConsistency(t *testing.T) {
	for _, consistency := range []gocql.Consistency{gocql.Any, gocql.One, gocql.Two, gocql.Three, gocql.LocalOne, gocql.Quorum, gocql.LocalQuorum, gocql.All, gocql.EachQuorum, gocql.Serial, gocql.LocalSerial} {
		t.Run(consistency.String(), func(t *testing.T) {
			dest := &CassandraDestination{consistency: consistency}
			require.ErrorContains(t, dest.ValidateManagedCDCState(), "does not support managed CDC")
		})
	}
}

func TestCDCStateFenceQueriesUseClusteringKeysWithoutFiltering(t *testing.T) {
	latest := cassandraFenceLatestQuery(`"ks"."cdc_state_fence"`)
	generation := cassandraFenceGenerationQuery(`"ks"."cdc_state_fence"`)
	require.NotContains(t, latest, "ALLOW FILTERING")
	require.NotContains(t, generation, "ALLOW FILTERING")
	require.Contains(t, latest, `WHERE "connector_id" = ? ORDER BY "state_generation" DESC LIMIT 1`)
	require.Contains(t, generation, `WHERE "connector_id" = ? AND "state_generation" = ?`)
}

func TestCDCTargetClaimUsesCanonicalKeyAndLWT(t *testing.T) {
	canonical, err := canonicalCassandraTable("default_ks", "events")
	require.NoError(t, err)
	require.Equal(t, destination.CDCTargetKey("default_ks", "events"), canonical)
	query := cassandraTargetClaimQuery(`"ks"."cdc_targets"`)
	require.Contains(t, query, "IF NOT EXISTS")
	require.Contains(t, query, `"destination_table"`)
	require.Contains(t, query, `"connector_id"`)
}

func TestBuildCreateKeyspaceSQLIsRaceSafe(t *testing.T) {
	require.Equal(
		t,
		`CREATE KEYSPACE IF NOT EXISTS "_bruin_staging" WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 3}`,
		buildCreateKeyspaceSQL("_bruin_staging", 3),
	)
}

func TestCDCStateBatchChunksAreBounded(t *testing.T) {
	const operations = 10_000
	chunks := 0
	covered := 0
	err := runCDCStateChunks(operations, func(start, end int) error {
		chunks++
		if size := end - start; size <= 0 || size > cassandraCDCStateBatchSize {
			t.Fatalf("chunk size = %d", size)
		}
		if start != covered {
			t.Fatalf("chunk starts at %d, want %d", start, covered)
		}
		covered = end
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, operations, covered)
	require.Equal(t, operations/cassandraCDCStateBatchSize, chunks)
}

func TestCDCStateInsertChunksBoundMaximumWidthRowsByBytes(t *testing.T) {
	statement := `INSERT INTO "_bruin_staging"."cdc_state" ("event_id", "state_version", "connector_id", "source_table", "destination_table", "state_kind", "state_generation", "state_status", "_cdc_lsn", "recorded_at") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	values := []interface{}{
		strings.Repeat("e", 128),
		strings.Repeat("v", 16),
		strings.Repeat("c", 64),
		strings.Repeat("s", 1000),
		strings.Repeat("d", 1000),
		strings.Repeat("k", 32),
		int64(1),
		strings.Repeat("x", 32),
		strings.Repeat("l", 64),
		time.Now().UTC(),
	}
	rowSize := estimateCassandraBatchEntrySize(statement, values)
	require.Less(t, rowSize, cassandraCDCStateBatchByteLimit)

	const operations = 10_000
	rowSizes := make([]int, operations)
	for i := range rowSizes {
		rowSizes[i] = rowSize
	}
	calls := 0
	covered := 0
	err := runCDCStateInsertChunks(rowSizes, func(start, end, estimatedBytes int) error {
		calls++
		require.Equal(t, covered, start)
		require.LessOrEqual(t, end-start, cassandraCDCStateBatchSize)
		require.LessOrEqual(t, estimatedBytes, cassandraCDCStateBatchByteLimit)
		covered = end
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, operations, covered)
	rowsPerCall := min(cassandraCDCStateBatchSize, cassandraCDCStateBatchByteLimit/rowSize)
	require.Equal(t, (operations+rowsPerCall-1)/rowsPerCall, calls)
}

func TestCDCStatePruneBatchSize(t *testing.T) {
	require.Equal(t, 10_000, (&CassandraDestination{}).CDCStatePruneBatchSize())
}

func TestBuildCreateTableSQL(t *testing.T) {
	sch := []schema.Column{
		{Name: "tenant_id", DataType: schema.TypeString},
		{Name: "id", DataType: schema.TypeUUID},
		{Name: "amount", DataType: schema.TypeDecimal},
		{Name: "tags", DataType: schema.TypeArray, ArrayType: schema.TypeString},
	}

	sql, err := buildCreateTableSQL(`"analytics"."events"`, sch, []string{"tenant_id", "id"})
	require.NoError(t, err)
	require.Equal(t, `CREATE TABLE IF NOT EXISTS "analytics"."events" ("tenant_id" text, "id" uuid, "amount" decimal, "tags" list<text>, PRIMARY KEY (("tenant_id"), "id"))`, sql)
}

func TestBuildCreateTableSQLRequiresPrimaryKey(t *testing.T) {
	_, err := buildCreateTableSQL(`"ks"."events"`, []schema.Column{{Name: "id", DataType: schema.TypeInt64}}, nil)
	require.ErrorContains(t, err, "requires at least one primary key")
}

func TestDialectAddColumnSQL(t *testing.T) {
	d := &Dialect{}
	require.Equal(t, `ALTER TABLE "ks"."events" ADD "payload" text`,
		d.AddColumnSQL("KS.Events", schema.Column{Name: "payload", DataType: schema.TypeJSON}))
	require.False(t, d.SupportsAlterType())
}
