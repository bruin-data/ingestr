package pipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/config"
	internalregistry "github.com/bruin-data/ingestr/internal/registry"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/strategy"
	"github.com/stretchr/testify/require"
)

type refreshSlotDestination struct {
	mockManagedCDCStateDestination
	entries map[string][]destination.CDCStateEntry
}

func newRefreshSlotDestination() *refreshSlotDestination {
	dest := &refreshSlotDestination{entries: make(map[string][]destination.CDCStateEntry)}
	dest.tableSchema = &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}}
	return dest
}

func (*refreshSlotDestination) Connect(context.Context, string) error { return nil }
func (*refreshSlotDestination) Close(context.Context) error           { return nil }
func (*refreshSlotDestination) PrepareTable(context.Context, destination.PrepareOptions) error {
	return nil
}

func (d *refreshSlotDestination) LoadCDCState(_ context.Context, _, connectorID string) ([]destination.CDCStateEntry, error) {
	return append([]destination.CDCStateEntry(nil), d.entries[connectorID]...), nil
}

func (d *refreshSlotDestination) LoadCDCStateFence(_ context.Context, _, connectorID string) (destination.CDCStateFence, error) {
	var fence destination.CDCStateFence
	for _, entry := range d.entries[connectorID] {
		if entry.StateKind != "run" || entry.Generation < fence.Generation {
			continue
		}
		if entry.Generation > fence.Generation {
			fence.Generation = entry.Generation
			fence.RunEventIDs = nil
		}
		fence.RunEventIDs = append(fence.RunEventIDs, entry.EventID)
	}
	return fence, nil
}

func (d *refreshSlotDestination) WriteCDCState(_ context.Context, records <-chan source.RecordBatchResult, _ destination.WriteOptions) error {
	for result := range records {
		if result.Err != nil {
			return result.Err
		}
		if result.Batch == nil {
			continue
		}
		batch := result.Batch
		for row := 0; row < int(batch.NumRows()); row++ {
			connectorID := batch.Column(2).(*array.String).Value(row)
			d.entries[connectorID] = append(d.entries[connectorID], destination.CDCStateEntry{
				EventID:          batch.Column(0).(*array.String).Value(row),
				SourceTable:      batch.Column(3).(*array.String).Value(row),
				DestinationTable: batch.Column(4).(*array.String).Value(row),
				StateKind:        batch.Column(5).(*array.String).Value(row),
				Generation:       batch.Column(6).(*array.Int64).Value(row),
				Status:           batch.Column(7).(*array.String).Value(row),
				Position:         batch.Column(8).(*array.String).Value(row),
			})
		}
		batch.Release()
	}
	return nil
}

func refreshSlotConfig(multi bool) *config.IngestConfig {
	cfg := config.DefaultConfig()
	cfg.SourceURI = "postgres+cdc://source/app?dest_schema=raw"
	cfg.DestURI = "bigquery://project/dataset?credentials_base64=YWFhYWFh"
	cfg.SourceTable = "public.orders"
	cfg.DestTable = "raw.orders"
	cfg.Stream, cfg.FullRefresh, cfg.RefreshID = true, true, "opaque-refresh"
	if multi {
		cfg.SourceTable = ""
		cfg.SourceTables = []string{"public.orders", "public.users"}
		cfg.DestTable = "cdc.stream"
	}
	return cfg
}

func refreshSlotSourceIdentity() source.ConnectorIdentity {
	return source.ConnectorIdentity{Database: "source-db", Connector: "source-connector"}
}

func refreshSlotPipeline(cfg *config.IngestConfig, dest *refreshSlotDestination) *Pipeline {
	identity := resolvedCDCStateConnectorID(cfg, refreshSlotSourceIdentity(), managedCDCDestinationTarget(cfg, dest))
	return &Pipeline{config: cfg, dest: dest, cdcConnectorID: identity}
}

func seedRefreshSlot(t *testing.T, p *Pipeline, complete bool) *strategy.CDCStateManager {
	t.Helper()
	manager, err := strategy.NewCDCStateManager(p.dest, p.cdcConnectorID, p.config.DestTable, p.config.StagingDataset)
	require.NoError(t, err)
	tables := p.config.SourceTables
	if p.config.SourceTable != "" {
		tables = []string{p.config.SourceTable}
	}
	for _, table := range tables {
		require.NoError(t, manager.RegisterTable(t.Context(), table, "raw."+table))
	}
	require.NoError(t, p.prepareStreamingRefresh(t.Context(), manager))
	require.NoError(t, manager.BeginRun(t.Context(), true))
	if complete {
		positions := make(map[string]string, len(tables))
		for _, table := range tables {
			require.NoError(t, manager.BindDestinationIncarnation(t.Context(), table, "raw."+table))
			positions[table] = "00000000/00000020"
		}
		require.NoError(t, manager.Persist(t.Context(), source.CDCStateCommitToken{Position: "00000000/00000020", SnapshotPositions: positions}))
	}
	return manager
}

func TestStreamingRefreshPreservesOrdinarySlotNaming(t *testing.T) {
	for _, multi := range []bool{false, true} {
		t.Run(map[bool]string{false: "single", true: "multi"}[multi], func(t *testing.T) {
			cfg := refreshSlotConfig(multi)
			p := refreshSlotPipeline(cfg, newRefreshSlotDestination())
			expected := cdcSlotSuffix(canonicalCDCStateURI(cfg.DestURI) + "\x00" + p.cdcConnectorID)
			expectedLegacy := legacyCDCSlotSuffix(cfg.DestURI)
			require.NoError(t, p.prepareStreamingRefreshIdentity(t.Context()))
			require.Equal(t, expected, cfg.CDCSlotSuffix, "starting a refresh must preserve the ordinary auto-slot name")
			require.Equal(t, expectedLegacy, cfg.CDCLegacySlotSuffix)
			seedRefreshSlot(t, p, true)
			cfg.FullRefresh, cfg.RefreshID = false, ""
			require.NoError(t, p.prepareStreamingRefreshIdentity(t.Context()))
			require.Equal(t, expected, cfg.CDCSlotSuffix, "ordinary restart after refresh must keep the same slot")
			require.Equal(t, expectedLegacy, cfg.CDCLegacySlotSuffix)
		})
	}
}

func TestStreamingRefreshRestoresPersistedSlotAcrossCredentialChanges(t *testing.T) {
	for _, multi := range []bool{false, true} {
		for _, complete := range []bool{false, true} {
			name := map[bool]string{false: "single", true: "multi"}[multi] + "/" + map[bool]string{false: "pending", true: "complete"}[complete]
			t.Run(name, func(t *testing.T) {
				dest := newRefreshSlotDestination()
				cfg := refreshSlotConfig(multi)
				p := refreshSlotPipeline(cfg, dest)
				require.NoError(t, p.prepareStreamingRefreshIdentity(t.Context()))
				originalConnector, originalSuffix, originalSource := p.cdcConnectorID, cfg.CDCSlotSuffix, cfg.SourceURI
				originalLegacy := cfg.CDCLegacySlotSuffix
				seedRefreshSlot(t, p, complete)
				saved, err := strategy.LookupRefresh(t.Context(), dest, cfg.RefreshID, p.streamingRefreshScope(), cfg.StagingDataset)
				require.NoError(t, err)
				require.Equal(t, &strategy.RefreshIdentity{ConnectorID: originalConnector, SlotSuffix: originalSuffix, LegacySlotSuffix: originalLegacy}, saved)
				for _, destURI := range []string{
					"bigquery://project/dataset?credentials_base64=YmJiYmJi",
					"bigquery://project/dataset?credentials_path=/tmp/rotated.json",
					"bigquery://project/dataset",
				} {
					restartedConfig := *cfg
					restartedConfig.DestURI = destURI
					restartedConfig.CDCSlotSuffix = ""
					restartedConfig.CDCLegacySlotSuffix = ""
					restarted := refreshSlotPipeline(&restartedConfig, dest)
					require.NotEqual(t, originalConnector, restarted.cdcConnectorID, "fixture must exercise connector identity restoration")
					recomputed := cdcSlotSuffix(canonicalCDCStateURI(destURI) + "\x00" + originalConnector)
					require.NotEqual(t, originalSuffix, recomputed, "fixture must reproduce the slot-change bug even after connector restoration")
					require.NotEqual(t, originalLegacy, legacyCDCSlotSuffix(destURI), "fixture must exercise legacy slot identity restoration")
					require.NoError(t, restarted.prepareStreamingRefreshIdentity(t.Context()))
					require.Equal(t, originalConnector, restarted.cdcConnectorID)
					require.Equal(t, originalSuffix, restartedConfig.CDCSlotSuffix)
					require.Equal(t, originalLegacy, restartedConfig.CDCLegacySlotSuffix)
					require.Equal(t, originalSource, restartedConfig.SourceURI)
				}
			})
		}
	}
}

func TestStreamingRefreshKeepsExplicitPostgresSlot(t *testing.T) {
	cfg := refreshSlotConfig(false)
	cfg.SourceURI += "&slot=explicit_stream_slot"
	dest := newRefreshSlotDestination()
	p := refreshSlotPipeline(cfg, dest)
	require.NoError(t, p.prepareStreamingRefreshIdentity(t.Context()))
	seedRefreshSlot(t, p, true)
	originalSource := cfg.SourceURI
	cfg.DestURI = "bigquery://project/dataset"
	restarted := refreshSlotPipeline(cfg, dest)
	require.NoError(t, restarted.prepareStreamingRefreshIdentity(t.Context()))
	require.Equal(t, originalSource, cfg.SourceURI, "explicit slot selection must remain intact")
}

type refreshSlotLeaseSource struct {
	leaseCalls   int
	leaseOptions source.ConnectorLeaseOptions
}

func (*refreshSlotLeaseSource) Schemes() []string                     { return []string{"postgres+cdc"} }
func (*refreshSlotLeaseSource) Connect(context.Context, string) error { return nil }
func (*refreshSlotLeaseSource) Close(context.Context) error           { return nil }
func (*refreshSlotLeaseSource) HandlesIncrementality() bool           { return true }
func (*refreshSlotLeaseSource) SupportsStreaming() bool               { return true }
func (*refreshSlotLeaseSource) DefaultStreamingStrategy() config.IncrementalStrategy {
	return config.StrategyMerge
}
func (*refreshSlotLeaseSource) SelectTables([]string) error { return nil }
func (*refreshSlotLeaseSource) ConnectorIdentity(context.Context) (source.ConnectorIdentity, error) {
	return refreshSlotSourceIdentity(), nil
}

func (*refreshSlotLeaseSource) GetTable(context.Context, source.TableRequest) (source.SourceTable, error) {
	return nil, errors.New("table lookup must not run after the lease test stops")
}

func (s *refreshSlotLeaseSource) AcquireConnectorLease(_ context.Context, opts source.ConnectorLeaseOptions) (source.ConnectorLease, error) {
	s.leaseCalls++
	s.leaseOptions = opts
	return nil, errors.New("stopped after refresh slot lease selection")
}

func TestStreamingRefreshSlotRestoredBeforeSourceLease(t *testing.T) {
	originalSource, err := internalregistry.Default.GetSourceConstructor("postgres+cdc")
	require.NoError(t, err)
	originalDestination, err := internalregistry.Default.GetDestinationConstructor("bigquery")
	require.NoError(t, err)
	t.Cleanup(func() {
		internalregistry.Default.RegisterSource([]string{"postgres+cdc"}, originalSource)
		internalregistry.Default.RegisterDestination([]string{"bigquery"}, originalDestination)
	})
	for _, multi := range []bool{false, true} {
		for _, state := range []string{"complete", "wrong-scope", "missing-slot", "missing-legacy-slot"} {
			name := map[bool]string{false: "single", true: "multi"}[multi] + "/" + state
			t.Run(name, func(t *testing.T) {
				cfg := refreshSlotConfig(multi)
				dest := newRefreshSlotDestination()
				p := refreshSlotPipeline(cfg, dest)
				require.NoError(t, p.prepareStreamingRefreshIdentity(t.Context()))
				originalConnector, originalSuffix := p.cdcConnectorID, cfg.CDCSlotSuffix
				originalLegacy := cfg.CDCLegacySlotSuffix
				if state == "missing-slot" {
					cfg.CDCSlotSuffix = ""
				}
				if state == "missing-legacy-slot" {
					cfg.CDCLegacySlotSuffix = ""
				}
				seedRefreshSlot(t, p, true)
				cfg.DestURI = "bigquery://project/dataset?credentials_path=/tmp/rotated.json"
				if state == "wrong-scope" {
					cfg.DestURI = "bigquery://other-project/dataset"
				}
				src := &refreshSlotLeaseSource{}
				internalregistry.Default.RegisterSource([]string{"postgres+cdc"}, func() interface{} { return src })
				internalregistry.Default.RegisterDestination([]string{"bigquery"}, func() interface{} { return dest })
				err := New(cfg).Run(t.Context())
				if state != "complete" {
					expectedError := "missing its replication slot identity"
					if state == "wrong-scope" {
						expectedError = "already assigned to a different source, destination, or table selection"
					}
					require.ErrorContains(t, err, expectedError)
					require.Zero(t, src.leaseCalls, "invalid refresh identity must fail before source lease acquisition")
					return
				}
				require.ErrorContains(t, err, "stopped after refresh slot lease selection")
				require.Equal(t, 1, src.leaseCalls)
				require.Equal(t, originalConnector, src.leaseOptions.ConnectorID)
				require.Equal(t, originalSuffix, src.leaseOptions.SlotSuffix)
				require.Equal(t, originalLegacy, src.leaseOptions.LegacySlotSuffix)
			})
		}
	}
}
