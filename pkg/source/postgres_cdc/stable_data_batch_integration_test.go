//go:build integration

package postgres_cdc

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestPostgresCDCStableDataBatchIDsReplay(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	container, connString := startConnectorLeasePostgres(t, ctx)
	defer func() { _ = container.Terminate(context.Background()) }()

	pool, err := pgxpool.New(ctx, connString)
	require.NoError(t, err)
	defer pool.Close()
	_, err = pool.Exec(ctx, `
		CREATE TABLE public.stable_keyed (id bigint PRIMARY KEY, value text);
		CREATE TABLE public.stable_keyless (id bigint, value text);
		ALTER TABLE public.stable_keyless REPLICA IDENTITY FULL;
		CREATE PUBLICATION stable_batch_pub FOR TABLE public.stable_keyed, public.stable_keyless;
		ALTER USER testuser REPLICATION;
	`)
	require.NoError(t, err)

	cdcURI := strings.Replace(connString, "postgres://", "postgres+cdc://", 1) + "&publication=stable_batch_pub"
	for _, tc := range []struct {
		name  string
		table string
		seed  func()
	}{
		{
			name:  "keyed_append",
			table: "public.stable_keyed",
			seed: func() {
				for _, statement := range []string{
					"INSERT INTO public.stable_keyed VALUES (1, 'same')",
					"INSERT INTO public.stable_keyed VALUES (2, 'same')",
					"INSERT INTO public.stable_keyed SELECT g, 'large' FROM generate_series(3, 1104) g",
				} {
					_, execErr := pool.Exec(ctx, statement)
					require.NoError(t, execErr)
				}
			},
		},
		{
			name:  "keyless",
			table: "public.stable_keyless",
			seed: func() {
				for _, statement := range []string{
					"INSERT INTO public.stable_keyless VALUES (1, 'same')",
					"INSERT INTO public.stable_keyless VALUES (1, 'same')",
					"INSERT INTO public.stable_keyless SELECT 2, 'large' FROM generate_series(1, 1102)",
				} {
					_, execErr := pool.Exec(ctx, statement)
					require.NoError(t, execErr)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			suffix := "stable_" + tc.name
			resume, incarnation, fingerprint := createEmptyCDCSnapshot(t, ctx, cdcURI, tc.table, suffix)
			tc.seed()

			first := readStableCDCReplay(t, ctx, cdcURI, tc.table, suffix, resume, incarnation, fingerprint, 1)
			second := readStableCDCReplay(t, ctx, cdcURI, tc.table, suffix, resume, incarnation, fingerprint, 10000)
			require.Equal(t, first, second, "replay identities changed with page size and connector restart")
			require.Len(t, first, 4, "two small transactions and two fixed windows from the large transaction must be distinct")

			seen := make(map[source.DurableID]struct{}, len(first))
			var rows int64
			for _, batch := range first {
				require.NotEmpty(t, batch.id)
				require.NotEmpty(t, batch.position)
				_, duplicate := seen[batch.id]
				require.False(t, duplicate, "distinct transaction windows shared durable identity %q", batch.id)
				seen[batch.id] = struct{}{}
				rows += batch.rows
			}
			require.EqualValues(t, 1104, rows)
		})
	}
}

type stableCDCReplayBatch struct {
	id       source.DurableID
	position string
	rows     int64
}

func createEmptyCDCSnapshot(
	t *testing.T,
	ctx context.Context,
	cdcURI, tableName, slotSuffix string,
) (string, string, string) {
	t.Helper()
	src := NewPostgresCDCSource()
	require.NoError(t, src.Connect(ctx, cdcURI))
	t.Cleanup(func() { _ = src.Close(context.Background()) })
	table, err := src.GetTable(ctx, source.TableRequest{
		Name: tableName, Strategy: config.StrategyAppend, StrategySet: true,
	})
	require.NoError(t, err)
	records, err := table.Read(ctx, source.ReadOptions{
		PageSize: 7, CDCSlotSuffix: slotSuffix, CDCStableDataBatches: true,
	})
	require.NoError(t, err)
	for result := range records {
		if result.Batch != nil {
			result.Batch.Release()
		}
		require.NoError(t, result.Err)
	}
	state := src.CDCState()
	resume := state.SnapshotPositions[tableName]
	require.NotEmpty(t, resume)
	incarnation, err := src.TableIncarnation(ctx, tableName)
	require.NoError(t, err)
	fingerprint, err := src.TableSchemaFingerprint(ctx, tableName)
	require.NoError(t, err)
	require.NoError(t, src.Close(context.Background()))
	return resume, incarnation, fingerprint
}

func readStableCDCReplay(
	t *testing.T,
	ctx context.Context,
	cdcURI, tableName, slotSuffix, resume, incarnation, fingerprint string,
	pageSize int,
) []stableCDCReplayBatch {
	t.Helper()
	src := NewPostgresCDCSource()
	require.NoError(t, src.Connect(ctx, cdcURI))
	table, err := src.GetTable(ctx, source.TableRequest{
		Name: tableName, Strategy: config.StrategyAppend, StrategySet: true,
	})
	require.NoError(t, err)
	records, err := table.Read(ctx, source.ReadOptions{
		PageSize:                   pageSize,
		CDCResumeLSN:               resume,
		CDCResumeIncarnation:       incarnation,
		CDCResumeSchemaFingerprint: fingerprint,
		CDCSlotSuffix:              slotSuffix,
		CDCStableDataBatches:       true,
	})
	require.NoError(t, err)

	var batches []stableCDCReplayBatch
	for result := range records {
		if result.Err != nil {
			if result.Batch != nil {
				result.Batch.Release()
			}
			require.NoError(t, result.Err)
		}
		if result.Batch == nil {
			continue
		}
		token, ok := result.CommitToken.(source.CDCStateCommitToken)
		rows := result.Batch.NumRows()
		result.Batch.Release()
		require.True(t, ok, "WAL data result carried token %T", result.CommitToken)
		require.NotEmpty(t, token.DataBatchID)
		require.NotEmpty(t, token.Position)
		batches = append(batches, stableCDCReplayBatch{id: token.DataBatchID, position: token.Position, rows: rows})
	}
	require.NoError(t, src.Close(context.Background()), fmt.Sprintf("close replay source for %s", tableName))
	return batches
}
