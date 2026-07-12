//go:build integration

package strategy

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/destination/postgres"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

type racingTargetPostgresDestination struct {
	*postgres.PostgresDestination
	target        string
	beforePrepare func(context.Context) error
	afterPrepare  func(context.Context) error
}

func (d *racingTargetPostgresDestination) PrepareTable(ctx context.Context, opts destination.PrepareOptions) error {
	if opts.Table == d.target && d.beforePrepare != nil {
		if err := d.beforePrepare(ctx); err != nil {
			return err
		}
	}
	if err := d.PostgresDestination.PrepareTable(ctx, opts); err != nil {
		return err
	}
	if opts.Table == d.target && d.afterPrepare != nil {
		return d.afterPrepare(ctx)
	}
	return nil
}

func TestPostgresOwnedTargetCleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	postgresDest, db := newPostgresTruncateInsertHarness(t, ctx)
	tableSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}

	run := func(t *testing.T, dest destination.Destination, target string) error {
		t.Helper()
		src := &fakeSourceTable{
			name: "public.events", hasKnownSchema: true, tableSchema: tableSchema,
			readErr: errors.New("source became unavailable"),
		}
		job := &IngestionJob{
			Config: &config.IngestConfig{
				SourceTable: "public.events", DestTable: target,
				IncrementalStrategy: config.StrategyTruncateInsert,
			},
			Table: src, Destination: dest, Schema: tableSchema, SourceSchema: tableSchema,
		}
		return (&TruncateInsertStrategy{}).Execute(ctx, job)
	}
	tableExists := func(t *testing.T, table string) bool {
		t.Helper()
		var exists bool
		require.NoError(t, db.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists))
		return exists
	}
	createSentinel := func(ctx context.Context, table string) error {
		if _, err := db.Exec(ctx, "CREATE TABLE "+destination.QuoteTableName(table)+" (id bigint)"); err != nil {
			return err
		}
		_, err := db.Exec(ctx, "INSERT INTO "+destination.QuoteTableName(table)+" (id) VALUES (99)")
		return err
	}
	assertSentinel := func(t *testing.T, table string) {
		t.Helper()
		var id int64
		require.NoError(t, db.QueryRow(ctx, "SELECT id FROM "+destination.QuoteTableName(table)).Scan(&id))
		require.Equal(t, int64(99), id)
	}

	t.Run("removes target created by failed run", func(t *testing.T) {
		target := "public.owned_failed_target"
		require.ErrorContains(t, run(t, postgresDest, target), "source became unavailable")
		require.False(t, tableExists(t, target))
	})

	t.Run("handles arbitrary ownership token", func(t *testing.T) {
		target := "public.arbitrary_token_target"
		token := `untrusted\'; DROP TABLE public.arbitrary_token_target; --`
		require.NoError(t, postgresDest.PrepareTable(ctx, destination.PrepareOptions{
			Table: target, Schema: tableSchema, OwnershipToken: token,
		}))
		require.True(t, tableExists(t, target))
		require.NoError(t, postgresDest.DropTableIfOwned(ctx, target, token))
		require.False(t, tableExists(t, target))
	})

	t.Run("preserves target created before owned prepare", func(t *testing.T) {
		target := "public.concurrent_target"
		dest := &racingTargetPostgresDestination{
			PostgresDestination: postgresDest,
			target:              target,
			beforePrepare: func(ctx context.Context) error {
				return createSentinel(ctx, target)
			},
		}
		require.ErrorContains(t, run(t, dest, target), "appeared concurrently")
		assertSentinel(t, target)
	})

	t.Run("preserves replacement after owned prepare", func(t *testing.T) {
		target := "public.replaced_owned_target"
		dest := &racingTargetPostgresDestination{
			PostgresDestination: postgresDest,
			target:              target,
			afterPrepare: func(ctx context.Context) error {
				if _, err := db.Exec(ctx, "DROP TABLE "+destination.QuoteTableName(target)); err != nil {
					return err
				}
				return createSentinel(ctx, target)
			},
		}
		require.ErrorContains(t, run(t, dest, target), "source became unavailable")
		assertSentinel(t, target)
	})
}

func TestKeylessCDCTruncateInsertProjectsStagingOnlyColumnsPostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	dest, db := newPostgresTruncateInsertHarness(t, ctx)

	tableSchema := &schema.TableSchema{Columns: []schema.Column{
		{Name: "id", DataType: schema.TypeInt64},
		{Name: "payload", DataType: schema.TypeString},
		{Name: destination.CDCLSNColumn, DataType: schema.TypeString},
		{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean},
		{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ},
		{Name: destination.CDCUnchangedColsColumn, DataType: schema.TypeString, Nullable: true},
	}}
	records := make(chan source.RecordBatchResult, 3)
	records <- source.RecordBatchResult{Batch: postgresCDCRecordBatch(tableSchema, []postgresCDCRow{
		{id: 1, payload: "before-boundary", lsn: "0/01", unchangedCols: `[]`},
	})}
	records <- source.RecordBatchResult{Truncate: true}
	records <- source.RecordBatchResult{Batch: keylessCDCRecordBatch(tableSchema)}
	close(records)
	src := &fakeSourceTable{
		name: "public.events", hasKnownSchema: true, tableSchema: tableSchema, readCh: records,
	}
	job := &IngestionJob{
		Config: &config.IngestConfig{
			SourceTable: "public.events", DestTable: "public.keyless_events", FullRefresh: true,
			IncrementalStrategy: config.StrategyTruncateInsert, ExtractParallelism: 2,
		},
		Table: src, Destination: dest, Schema: tableSchema, SourceSchema: tableSchema,
	}

	require.NoError(t, (&TruncateInsertStrategy{}).Execute(ctx, job))
	require.True(t, src.readOpts.CDCSnapshotReplace)
	var id int64
	var payload, lsn string
	require.NoError(t, db.QueryRow(ctx, `
		SELECT id, payload, _cdc_lsn
		FROM public.keyless_events
	`).Scan(&id, &payload, &lsn))
	require.Equal(t, int64(42), id)
	require.Equal(t, "snapshot", payload)
	require.Equal(t, "0/10", lsn)
	var preBoundaryRows int
	require.NoError(t, db.QueryRow(ctx, `SELECT COUNT(*) FROM public.keyless_events WHERE id = 1`).Scan(&preBoundaryRows))
	require.Zero(t, preBoundaryRows)
	var markerColumns int
	require.NoError(t, db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'keyless_events'
		  AND column_name = $1
	`, destination.CDCUnchangedColsColumn).Scan(&markerColumns))
	require.Zero(t, markerColumns)
}

func TestPKCDCTruncateInsertDiscardsPreBoundaryStagingRowsPostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	dest, db := newPostgresTruncateInsertHarness(t, ctx)

	tableSchema := &schema.TableSchema{
		PrimaryKeys: []string{"id"},
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "payload", DataType: schema.TypeString},
			{Name: destination.CDCLSNColumn, DataType: schema.TypeString},
			{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean},
			{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ},
			{Name: destination.CDCUnchangedColsColumn, DataType: schema.TypeString, Nullable: true},
		},
	}
	records := make(chan source.RecordBatchResult, 3)
	records <- source.RecordBatchResult{Batch: postgresCDCRecordBatch(tableSchema, []postgresCDCRow{
		{id: 1, payload: "before-boundary", lsn: "0/10", unchangedCols: `["payload"]`},
	})}
	records <- source.RecordBatchResult{Truncate: true}
	records <- source.RecordBatchResult{Batch: postgresCDCRecordBatch(tableSchema, []postgresCDCRow{
		{id: 2, payload: "older-post-boundary", lsn: "0/20", unchangedCols: `[]`},
		{id: 2, payload: "latest-post-boundary", lsn: "0/21", unchangedCols: `["payload"]`},
	})}
	close(records)
	src := &fakeSourceTable{
		name:              "public.events",
		primaryKeys:       []string{"id"},
		primaryKeysUnique: true,
		hasKnownSchema:    true,
		tableSchema:       tableSchema,
		readCh:            records,
	}
	job := &IngestionJob{
		Config: &config.IngestConfig{
			SourceTable: "public.events", DestTable: "public.pk_events", FullRefresh: true,
			PrimaryKeys: []string{"id"}, IncrementalStrategy: config.StrategyTruncateInsert,
			ExtractParallelism: 2,
		},
		Table: src, Destination: dest, Schema: tableSchema, SourceSchema: tableSchema,
	}

	require.NoError(t, (&TruncateInsertStrategy{}).Execute(ctx, job))
	require.True(t, src.readOpts.CDCSnapshotReplace)

	var preBoundaryRows int
	require.NoError(t, db.QueryRow(ctx, `SELECT COUNT(*) FROM public.pk_events WHERE id = 1`).Scan(&preBoundaryRows))
	require.Zero(t, preBoundaryRows)
	var id int64
	var payload, lsn string
	var deleted bool
	require.NoError(t, db.QueryRow(ctx, `
		SELECT id, payload, _cdc_lsn, _cdc_deleted
		FROM public.pk_events
	`).Scan(&id, &payload, &lsn, &deleted))
	require.Equal(t, int64(2), id)
	require.Equal(t, "latest-post-boundary", payload)
	require.Equal(t, "0/21", lsn)
	require.False(t, deleted)
	var markerColumns int
	require.NoError(t, db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'pk_events'
		  AND column_name = $1
	`, destination.CDCUnchangedColsColumn).Scan(&markerColumns))
	require.Zero(t, markerColumns)
}

func newPostgresTruncateInsertHarness(
	t *testing.T,
	ctx context.Context,
) (*postgres.PostgresDestination, *pgxpool.Pool) {
	t.Helper()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: "postgres:16-alpine",
			Env: map[string]string{
				"POSTGRES_USER": "testuser", "POSTGRES_PASSWORD": "testpass", "POSTGRES_DB": "testdb",
			},
			ExposedPorts: []string{"5432/tcp"},
			WaitingFor: wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(time.Minute),
		},
		Started: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)

	connString := fmt.Sprintf(
		"postgres://testuser:testpass@%s:%s/testdb?sslmode=disable", host, port.Port(),
	)
	dest := postgres.NewPostgresDestination()
	require.NoError(t, dest.Connect(ctx, connString))
	t.Cleanup(func() { _ = dest.Close(context.Background()) })
	db, err := pgxpool.New(ctx, connString)
	require.NoError(t, err)
	t.Cleanup(db.Close)
	return dest, db
}

type postgresCDCRow struct {
	id            int64
	payload       string
	lsn           string
	deleted       bool
	unchangedCols string
}

func postgresCDCRecordBatch(tableSchema *schema.TableSchema, rows []postgresCDCRow) arrow.RecordBatch {
	builder := array.NewRecordBuilder(memory.DefaultAllocator, tableSchema.ToArrowSchema())
	defer builder.Release()
	for _, row := range rows {
		builder.Field(0).(*array.Int64Builder).Append(row.id)
		builder.Field(1).(*array.StringBuilder).Append(row.payload)
		builder.Field(2).(*array.StringBuilder).Append(row.lsn)
		builder.Field(3).(*array.BooleanBuilder).Append(row.deleted)
		builder.Field(4).(*array.TimestampBuilder).Append(arrow.Timestamp(time.Now().UTC().UnixMicro()))
		builder.Field(5).(*array.StringBuilder).Append(row.unchangedCols)
	}
	return builder.NewRecordBatch()
}

func keylessCDCRecordBatch(tableSchema *schema.TableSchema) arrow.RecordBatch {
	return postgresCDCRecordBatch(tableSchema, []postgresCDCRow{
		{id: 42, payload: "snapshot", lsn: "0/10", unchangedCols: `["payload"]`},
	})
}
