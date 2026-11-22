package pipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	internalregistry "github.com/bruin-data/ingestr/internal/registry"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	_ "github.com/bruin-data/ingestr/pkg/source/mssql"
	"github.com/bruin-data/ingestr/pkg/strategy"
	"github.com/stretchr/testify/require"
)

type ctResumeAdmissionSource struct{}

func (*ctResumeAdmissionSource) Schemes() []string                     { return []string{"mssql+ct"} }
func (*ctResumeAdmissionSource) Connect(context.Context, string) error { return nil }
func (*ctResumeAdmissionSource) Close(context.Context) error           { return nil }
func (*ctResumeAdmissionSource) HandlesIncrementality() bool           { return true }
func (*ctResumeAdmissionSource) SupportsStreaming() bool               { return true }
func (*ctResumeAdmissionSource) DefaultStreamingStrategy() config.IncrementalStrategy {
	return config.StrategyMerge
}

func (*ctResumeAdmissionSource) GetTable(context.Context, source.TableRequest) (source.SourceTable, error) {
	return nil, errors.New("stopped after CT resume selection")
}

type ctResumeAdmissionDestination struct {
	mockLegacyBootstrapDestination
	connectorID string
	maxCalls    int
	loadErr     error
}

func (*ctResumeAdmissionDestination) Connect(context.Context, string) error { return nil }
func (*ctResumeAdmissionDestination) Close(context.Context) error           { return nil }
func (d *ctResumeAdmissionDestination) GetMaxCDCLSN(context.Context, string) (string, error) {
	d.maxCalls++
	return d.maxLSN, nil
}

func (d *ctResumeAdmissionDestination) LoadCDCState(_ context.Context, _, connectorID string) ([]destination.CDCStateEntry, error) {
	if d.loadErr != nil {
		return nil, d.loadErr
	}
	if connectorID != d.connectorID {
		return nil, nil
	}
	return append([]destination.CDCStateEntry(nil), d.entries...), nil
}

func TestChangeTrackingRestartUsesManagedState(t *testing.T) {
	previous, err := internalregistry.Default.GetSourceConstructor("mssql+ct")
	require.NoError(t, err)
	t.Cleanup(func() { internalregistry.Default.RegisterSource([]string{"mssql+ct"}, previous) })
	internalregistry.Default.RegisterSource([]string{"mssql+ct"}, func() interface{} { return &ctResumeAdmissionSource{} })
	for _, streaming := range []bool{true, false} {
		mode := "batch"
		if streaming {
			mode = "stream"
		}
		for _, state := range []string{"complete", "interrupted", "legacy", "read-error", "other-database"} {
			t.Run(mode+"/"+state, func(t *testing.T) {
				cfg := config.DefaultConfig()
				cfg.SourceURI = "mssql+ct://source?database=app"
				cfg.SourceTable = "dbo.items"
				cfg.DestURI = "ct-resume-test://destination/db"
				cfg.DestTable = "raw.items"
				cfg.Stream = streaming
				dest := &ctResumeAdmissionDestination{connectorID: genericCDCConnectorID(cfg)}
				dest.maxLSN = "00000000000000000010"
				dest.tableSchema = &schema.TableSchema{Columns: []schema.Column{{Name: "id", DataType: schema.TypeInt64}}, PrimaryKeys: []string{"id"}}
				internalregistry.Default.RegisterDestination([]string{"ct-resume-test"}, func() interface{} { return dest })
				if state == "complete" || state == "interrupted" || state == "other-database" {
					manager, err := strategy.NewCDCStateManager(dest, dest.connectorID, cfg.DestTable, "")
					require.NoError(t, err)
					require.NoError(t, manager.RegisterTable(t.Context(), cfg.SourceTable, cfg.DestTable))
					require.NoError(t, manager.BeginRun(t.Context(), true))
					if state != "interrupted" {
						require.NoError(t, manager.BindDestinationIncarnation(t.Context(), cfg.SourceTable, cfg.DestTable))
						require.NoError(t, manager.Persist(t.Context(), source.CDCStateCommitToken{SnapshotPositions: map[string]string{cfg.SourceTable: dest.maxLSN}}))
						require.NoError(t, manager.Persist(t.Context(), source.CDCStateCommitToken{Position: "00000000000000000040"}))
					}
				}
				if state == "read-error" {
					dest.loadErr = errors.New("state unavailable")
				}
				if state == "other-database" {
					cfg.SourceURI = "mssql+ct://source?database=other"
				}
				err = New(cfg).Run(t.Context())
				if state == "read-error" {
					require.ErrorContains(t, err, "state unavailable")
					require.Zero(t, dest.maxCalls, "metadata failure cannot fall back to a stale row cursor")
					return
				}
				require.ErrorContains(t, err, "stopped after CT resume selection")
				switch state {
				case "complete":
					require.Equal(t, "00000000000000000040", cfg.CDCResumeLSN)
					require.Zero(t, dest.maxCalls, "managed checkpoints must outrank older row cursors")
				case "interrupted":
					require.Empty(t, cfg.CDCResumeLSN, "an incomplete managed snapshot must restart")
					require.Zero(t, dest.maxCalls)
				default:
					require.Equal(t, dest.maxLSN, cfg.CDCResumeLSN)
					require.Equal(t, 1, dest.maxCalls, "connectors without managed history retain legacy resume")
				}
			})
		}
	}
}

func TestChangeTrackingConnectorIdentityUsesEffectiveDatabase(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SourceURI = "mssql+ct://user:password@source?database=app"
	cfg.SourceTable = "dbo.items"
	cfg.DestURI = "bigquery://project/dataset?credentials_base64=first"
	cfg.DestTable = "raw.items"
	identity := genericCDCConnectorID(cfg)
	cfg.SourceURI = "mssql+ct://other:rotated@source/app?database=ignored"
	cfg.DestURI = "bigquery://project/dataset?credentials_path=/tmp/key.json"
	require.Equal(t, identity, genericCDCConnectorID(cfg))
	cfg.SourceURI = "mssql+ct://source?database=other"
	require.NotEqual(t, identity, genericCDCConnectorID(cfg))
}
