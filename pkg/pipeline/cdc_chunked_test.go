package pipeline

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

type chunkedRoutingSource struct {
	staticMultiTableSource
	validationErr error
	validated     []source.SourceTableInfo
	readOptions   source.MultiTableReadOptions
}

func (s *chunkedRoutingSource) ValidateCDCChunkedSnapshot(_ context.Context, tables []source.SourceTableInfo) error {
	s.validated = append([]source.SourceTableInfo(nil), tables...)
	return s.validationErr
}

func (*chunkedRoutingSource) CommitStream(context.Context, any) error { return nil }

func (s *chunkedRoutingSource) ReadAll(_ context.Context, options source.MultiTableReadOptions) (<-chan source.RecordBatchResult, error) {
	s.readOptions = options
	records := make(chan source.RecordBatchResult)
	close(records)
	return records, nil
}

type chunkedRoutingDestination struct {
	emptyInitialManagedDestination
	prepared []destination.PrepareOptions
	claims   int
}

func (d *chunkedRoutingDestination) PrepareTable(_ context.Context, options destination.PrepareOptions) error {
	d.prepared = append(d.prepared, options)
	return nil
}

func (d *chunkedRoutingDestination) ClaimCDCTarget(context.Context, string, destination.CDCTargetClaim) error {
	d.claims++
	return nil
}

func (*chunkedRoutingDestination) DropTable(context.Context, string) error { return nil }

func chunkedRoutingTable() source.SourceTableInfo {
	return source.SourceTableInfo{
		Name: "public.orders", DestSchema: "raw", PrimaryKeys: []string{"OrderID"},
		Schema: &schema.TableSchema{Columns: []schema.Column{{Name: "OrderID", DataType: schema.TypeInt64}}, PrimaryKeys: []string{"OrderID"}},
	}
}

func TestChunkedFiniteMultiTableUsesStreamingExecutor(t *testing.T) {
	for _, fullRefresh := range []bool{false, true} {
		name := "initial"
		if fullRefresh {
			name = "full_refresh"
		}
		t.Run(name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.SourceURI, cfg.DestURI = "postgres+cdc://source/db", "postgres://destination/db"
			cfg.CDCChunkedSnapshot, cfg.FullRefresh = true, fullRefresh
			cfg.NoLoadTimestamp, cfg.NoRunID = true, true
			cfg.SchemaNaming = "snake_case"
			cfg.Progress = config.ProgressLog
			cfg.FlushInterval = time.Hour
			dest := &chunkedRoutingDestination{}
			src := &chunkedRoutingSource{staticMultiTableSource: staticMultiTableSource{tables: []source.SourceTableInfo{chunkedRoutingTable()}}}
			pipeline := &Pipeline{config: cfg, dest: dest, cdcConnectorID: "connector"}
			require.NoError(t, pipeline.runMultiTable(t.Context(), src))
			require.True(t, src.readOptions.Streaming)
			require.NotNil(t, src.readOptions.CDCChunkedSnapshot)
			require.False(t, src.readOptions.CDCChunkedSnapshot.Continuous)
			require.Equal(t, fullRefresh, src.readOptions.FullRefresh)
			require.NotNil(t, src.readOptions.CDCChunkedSnapshot.Store)
			require.False(t, cfg.Stream)
			require.Len(t, src.validated, 1)
			require.Equal(t, []string{"OrderID"}, src.validated[0].PrimaryKeys)
			require.Equal(t, "OrderID", src.validated[0].Schema.Columns[0].Name)
			require.Equal(t, "OrderID", src.tables[0].Schema.Columns[0].Name)
			require.Positive(t, dest.claims)
		})
	}
}

func TestChunkedPreflightRejectsBeforeDestinationPreparation(t *testing.T) {
	for _, sourceFailure := range []bool{false, true} {
		name := "nullable_key"
		if sourceFailure {
			name = "source_validation"
		}
		t.Run(name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.SourceURI = "postgres+cdc://source/db"
			cfg.CDCChunkedSnapshot = true
			table := chunkedRoutingTable()
			src := &chunkedRoutingSource{staticMultiTableSource: staticMultiTableSource{tables: []source.SourceTableInfo{table}}}
			want := "non-null key"
			if sourceFailure {
				src.validationErr = errors.New("PostgreSQL version does not support chunk barriers")
				want = "does not support chunk barriers"
			} else {
				table.Schema.Columns[0].Nullable = true
			}
			dest := &chunkedRoutingDestination{}
			pipeline := &Pipeline{config: cfg, dest: dest, cdcConnectorID: "connector"}
			require.ErrorContains(t, pipeline.runMultiTable(t.Context(), src), want)
			require.Empty(t, dest.prepared)
			require.Zero(t, dest.claims)
			require.Nil(t, src.readOptions.CDCChunkedSnapshot)
		})
	}
}

func TestChunkedStrategyResolutionAndExplicitRejection(t *testing.T) {
	for _, fullRefresh := range []bool{false, true} {
		cfg := config.DefaultConfig()
		cfg.SourceURI, cfg.DestURI = "postgres+cdc://source/db", "postgres://destination/db"
		cfg.CDCChunkedSnapshot, cfg.FullRefresh = true, fullRefresh
		require.NoError(t, validateManagedChangeConfig(cfg))
		require.True(t, usesStreamingExecutor(cfg))
		require.Equal(t, config.StrategyMerge, resolveStrategy(cfg, &incrementalitySource{}, &mockSourceTable{pks: []string{"id"}}))
		require.Equal(t, config.StrategyMerge, resolveMultiTableStrategy(cfg, &staticMultiTableSource{}))
		cfg.IncrementalStrategyExplicit = true
		cfg.IncrementalStrategy = config.StrategyReplace
		require.ErrorContains(t, validateManagedChangeConfig(cfg), "require merge")
		cfg.IncrementalStrategy = config.StrategyAppend
		require.ErrorContains(t, validateManagedChangeConfig(cfg), "require merge")
	}
	cfg := &config.IngestConfig{SourceURI: "postgres+cdc://source/db", FullRefresh: true}
	require.False(t, usesStreamingExecutor(cfg))
	require.Equal(t, config.StrategyReplace, resolveMultiTableStrategy(cfg, &staticMultiTableSource{}))
}

func TestChunkedSourcePreflightIsRequired(t *testing.T) {
	pipeline := &Pipeline{config: &config.IngestConfig{CDCChunkedSnapshot: true}}
	err := pipeline.validateCDCChunkedSnapshotSource(t.Context(), &staticMultiTableSource{}, []source.SourceTableInfo{chunkedRoutingTable()})
	require.ErrorContains(t, err, "does not support chunked CDC snapshot validation")
	pipeline.config.CDCChunkedSnapshot = false
	require.NoError(t, pipeline.validateCDCChunkedSnapshotSource(t.Context(), &staticMultiTableSource{}, nil))
}
