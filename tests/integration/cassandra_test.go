//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/stretchr/testify/require"
)

func TestCassandraSourceToDuckDB(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if cassandraShared.uri == "" {
		t.Skip("shared Cassandra container not available")
	}

	ctx := context.Background()
	session := openCassandraSession(t)
	defer session.Close()

	table := uniqueCassandraTable("source_events")
	require.NoError(t, session.Query(fmt.Sprintf("DROP TABLE IF EXISTS %s.%s", cassandraKeyspace, table)).ExecContext(ctx))
	require.NoError(t, session.Query(fmt.Sprintf(`
		CREATE TABLE %s.%s (
			id uuid PRIMARY KEY,
			name text,
			score int,
			active boolean,
			created_at timestamp
		)
	`, cassandraKeyspace, table)).ExecContext(ctx))
	require.NoError(t, session.AwaitSchemaAgreement(ctx))

	firstID, err := gocql.ParseUUID("11111111-1111-1111-1111-111111111111")
	require.NoError(t, err)
	secondID, err := gocql.ParseUUID("22222222-2222-2222-2222-222222222222")
	require.NoError(t, err)
	require.NoError(t, session.Query(
		fmt.Sprintf("INSERT INTO %s.%s (id, name, score, active, created_at) VALUES (?, ?, ?, ?, ?)", cassandraKeyspace, table),
		firstID, "alice", int32(10), true, time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
	).ExecContext(ctx))
	require.NoError(t, session.Query(
		fmt.Sprintf("INSERT INTO %s.%s (id, name, score, active, created_at) VALUES (?, ?, ?, ?, ?)", cassandraKeyspace, table),
		secondID, "bob", int32(20), false, time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC),
	).ExecContext(ctx))

	duckDBPath := filepath.Join(t.TempDir(), "cassandra_source.duckdb")
	cfg := &config.IngestConfig{
		SourceURI:           cassandraShared.uri,
		SourceTable:         table,
		DestURI:             "duckdb:///" + duckDBPath,
		DestTable:           "events",
		IncrementalStrategy: config.StrategyReplace,
		PageSize:            1,
		Yes:                 true,
	}
	require.NoError(t, runPipeline(t, ctx, pipeline.New(cfg)))

	db, err := sql.Open("adbc_generic", fmt.Sprintf("driver=duckdb;path=%s", duckDBPath))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	rows, err := db.QueryContext(ctx, "SELECT name, score, active FROM events ORDER BY score")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	var got []string
	for rows.Next() {
		var name string
		var score int32
		var active bool
		require.NoError(t, rows.Scan(&name, &score, &active))
		got = append(got, fmt.Sprintf("%s:%d:%t", name, score, active))
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"alice:10:true", "bob:20:false"}, got)
}

func TestCassandraDestinationReplaceAndMerge(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if cassandraShared.uri == "" {
		t.Skip("shared Cassandra container not available")
	}

	ctx := context.Background()
	session := openCassandraSession(t)
	defer session.Close()

	table := uniqueCassandraTable("dest_users")
	require.NoError(t, session.Query(fmt.Sprintf("DROP TABLE IF EXISTS %s.%s", cassandraKeyspace, table)).ExecContext(ctx))

	initialPath := writeJSONL(t, "initial.jsonl", []string{
		`{"id":"u1","name":"Alice","score":10,"active":true}`,
		`{"id":"u2","name":"Bob","score":20,"active":false}`,
	})
	cfg := &config.IngestConfig{
		SourceURI:           "jsonl://" + initialPath,
		SourceTable:         "records",
		DestURI:             cassandraShared.uri,
		DestTable:           table,
		IncrementalStrategy: config.StrategyReplace,
		PrimaryKeys:         []string{"id"},
		PageSize:            1,
		Yes:                 true,
	}
	require.NoError(t, runPipeline(t, ctx, pipeline.New(cfg)))

	updatePath := writeJSONL(t, "updates.jsonl", []string{
		`{"id":"u1","name":"Alice Updated","score":30,"active":true}`,
		`{"id":"u3","name":"Carol","score":40,"active":true}`,
	})
	cfg.SourceURI = "jsonl://" + updatePath
	cfg.IncrementalStrategy = config.StrategyMerge
	require.NoError(t, runPipeline(t, ctx, pipeline.New(cfg)))

	got := readCassandraUsers(t, session, table)
	require.Equal(t, map[string]string{
		"u1": "Alice Updated:30:true",
		"u2": "Bob:20:false",
		"u3": "Carol:40:true",
	}, got)
}

func openCassandraSession(t *testing.T) *gocql.Session {
	t.Helper()
	cluster := gocql.NewCluster(cassandraShared.host)
	cluster.Port = cassandraShared.port
	cluster.Consistency = gocql.One
	cluster.DisableInitialHostLookup = true
	session, err := cluster.CreateSession()
	require.NoError(t, err)
	return session
}

func writeJSONL(t *testing.T, name string, lines []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644))
	return path
}

func readCassandraUsers(t *testing.T, session *gocql.Session, table string) map[string]string {
	t.Helper()
	iter := session.Query(fmt.Sprintf("SELECT id, name, score, active FROM %s.%s", cassandraKeyspace, table)).Iter()

	got := make(map[string]string)
	for {
		row := make(map[string]interface{})
		if !iter.MapScan(row) {
			break
		}
		got[row["id"].(string)] = fmt.Sprintf("%s:%s:%t", row["name"], numericString(row["score"]), row["active"].(bool))
	}
	require.NoError(t, iter.Close())
	return got
}

func numericString(v interface{}) string {
	switch n := v.(type) {
	case float64:
		return fmt.Sprintf("%.0f", n)
	case float32:
		return fmt.Sprintf("%.0f", n)
	case int:
		return fmt.Sprintf("%d", n)
	case int32:
		return fmt.Sprintf("%d", n)
	case int64:
		return fmt.Sprintf("%d", n)
	default:
		return fmt.Sprintf("%v", n)
	}
}
