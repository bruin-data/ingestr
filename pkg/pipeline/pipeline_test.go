package pipeline

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	internalregistry "github.com/bruin-data/ingestr/internal/registry"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/naming"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/tablename"
	"github.com/bruin-data/ingestr/pkg/transformer"
	"github.com/stretchr/testify/require"
)

type mockDestination struct {
	destination.Destination
	tableSchema *schema.TableSchema
	scheme      string
}

type mockConnectorLease struct {
	done chan struct{}
	err  error
}

type preflightTimingSource struct {
	connected bool
	closed    bool
	prepared  bool
}

func (s *preflightTimingSource) Schemes() []string { return []string{"postgres+cdc"} }
func (s *preflightTimingSource) Connect(context.Context, string) error {
	s.connected = true
	return nil
}

func (s *preflightTimingSource) Close(ctx context.Context) error {
	s.closed = true
	if ctx.Err() != nil {
		return fmt.Errorf("close received cancelled context: %w", ctx.Err())
	}
	return nil
}

func (s *preflightTimingSource) GetTable(context.Context, source.TableRequest) (source.SourceTable, error) {
	return nil, errors.New("GetTable must not be called after preflight failure")
}
func (s *preflightTimingSource) HandlesIncrementality() bool { return false }
func (s *preflightTimingSource) ConnectorIdentity(context.Context) (source.ConnectorIdentity, error) {
	return source.ConnectorIdentity{Database: "db", Connector: "connector"}, nil
}

func (s *preflightTimingSource) ValidateConnectorPreflight(context.Context, source.ConnectorPreflightOptions) error {
	return errors.New("PostgreSQL 14 or newer is required")
}

func (s *preflightTimingSource) PrepareConnector(context.Context) error {
	s.prepared = true
	return nil
}

type preflightTimingDestination struct {
	mockDestination
	connected bool
}

func (d *preflightTimingDestination) Connect(context.Context, string) error {
	d.connected = true
	return nil
}

type postgresCDCAdmissionSource struct {
	preflightTimingSource
}

func (s *postgresCDCAdmissionSource) ValidateConnectorPreflight(context.Context, source.ConnectorPreflightOptions) error {
	return nil
}

type postgresCDCAdmissionDestination struct {
	mockDestination
	connected bool
	closed    bool
}

type postgresCDCTargetValidationDestination struct {
	mockManagedCDCStateDestination
	connected bool
	closed    bool
	target    string
}

type invalidStrategyTimingSource struct {
	connectCalls   int
	prepareCalls   int
	leaseCalls     int
	getTableCalls  int
	getTablesCalls int
}

func (s *invalidStrategyTimingSource) Schemes() []string { return []string{"postgres+cdc"} }

func (s *invalidStrategyTimingSource) Connect(context.Context, string) error {
	s.connectCalls++
	return nil
}

func (s *invalidStrategyTimingSource) Close(context.Context) error { return nil }

func (s *invalidStrategyTimingSource) GetTable(context.Context, source.TableRequest) (source.SourceTable, error) {
	s.getTableCalls++
	return nil, errors.New("GetTable must not run for an invalid PostgreSQL CDC strategy")
}

func (s *invalidStrategyTimingSource) HandlesIncrementality() bool { return true }

func (s *invalidStrategyTimingSource) ConnectorIdentity(context.Context) (source.ConnectorIdentity, error) {
	return source.ConnectorIdentity{Database: "db", Connector: "connector"}, nil
}

func (s *invalidStrategyTimingSource) PrepareConnector(context.Context) error {
	s.prepareCalls++
	return nil
}

func (s *invalidStrategyTimingSource) AcquireConnectorLease(context.Context, source.ConnectorLeaseOptions) (source.ConnectorLease, error) {
	s.leaseCalls++
	return &mockConnectorLease{done: make(chan struct{})}, nil
}

func (s *invalidStrategyTimingSource) IsMultiTable() bool { return true }

func (s *invalidStrategyTimingSource) GetTables(context.Context) ([]source.SourceTableInfo, error) {
	s.getTablesCalls++
	return nil, errors.New("GetTables must not run for an invalid PostgreSQL CDC strategy")
}

func (s *invalidStrategyTimingSource) ReadAll(context.Context, source.MultiTableReadOptions) (<-chan source.RecordBatchResult, error) {
	return nil, errors.New("ReadAll must not run for an invalid PostgreSQL CDC strategy")
}

type invalidStrategyTimingDestination struct {
	mockManagedCDCStateDestination
	connectCalls     int
	prepareCalls     int
	stateWriteCalls  int
	stateLoadCalls   int
	targetClaimCalls int
}

func (d *invalidStrategyTimingDestination) Connect(context.Context, string) error {
	d.connectCalls++
	return nil
}

func (d *invalidStrategyTimingDestination) PrepareTable(context.Context, destination.PrepareOptions) error {
	d.prepareCalls++
	return nil
}

func (d *invalidStrategyTimingDestination) WriteCDCState(context.Context, <-chan source.RecordBatchResult, destination.WriteOptions) error {
	d.stateWriteCalls++
	return nil
}

func (d *invalidStrategyTimingDestination) LoadCDCState(context.Context, string, string) ([]destination.CDCStateEntry, error) {
	d.stateLoadCalls++
	return nil, nil
}

func (d *invalidStrategyTimingDestination) ClaimCDCTarget(context.Context, string, destination.CDCTargetClaim) error {
	d.targetClaimCalls++
	return nil
}

func (d *postgresCDCTargetValidationDestination) Connect(context.Context, string) error {
	d.connected = true
	return nil
}

func (d *postgresCDCTargetValidationDestination) Close(context.Context) error {
	d.closed = true
	return nil
}

func (d *postgresCDCTargetValidationDestination) ValidateManagedCDCTarget(_ context.Context, table string) error {
	d.target = table
	return errors.New("compatibility level 120 does not support OPENJSON")
}

func (d *postgresCDCAdmissionDestination) Connect(context.Context, string) error {
	d.connected = true
	return nil
}

func (d *postgresCDCAdmissionDestination) Close(context.Context) error {
	d.closed = true
	return nil
}

func TestSourcePreflightRunsBeforeDestinationAndMutatingPreparation(t *testing.T) {
	oldSource, err := internalregistry.Default.GetSourceConstructor("postgres+cdc")
	require.NoError(t, err)
	defer internalregistry.Default.RegisterSource([]string{"postgres+cdc"}, oldSource)

	src := &preflightTimingSource{}
	dest := &preflightTimingDestination{}
	internalregistry.Default.RegisterSource([]string{"postgres+cdc"}, func() interface{} { return src })
	internalregistry.Default.RegisterDestination([]string{"preflight-dest"}, func() interface{} { return dest })

	cfg := config.DefaultConfig()
	cfg.SourceURI = "postgres+cdc://source/db?mode=batch"
	cfg.DestURI = "preflight-dest://destination/db"
	cfg.SourceTable = "public.items"
	cfg.DestTable = "raw.items"

	err = New(cfg).Run(context.Background())
	require.ErrorContains(t, err, "source connector preflight failed")
	require.True(t, src.connected)
	require.True(t, src.closed)
	require.False(t, src.prepared, "publication preparation ran before preflight")
	require.False(t, dest.connected, "destination connected before source preflight")
}

func TestPostgresCDCRejectsUnsupportedStrategiesBeforeConnectorAndStateWork(t *testing.T) {
	oldSource, err := internalregistry.Default.GetSourceConstructor("postgres+cdc")
	require.NoError(t, err)
	defer internalregistry.Default.RegisterSource([]string{"postgres+cdc"}, oldSource)

	tests := []struct {
		name        string
		strategy    config.IncrementalStrategy
		sourceTable string
		explicit    bool
	}{
		{name: "single table explicit delete insert", strategy: config.StrategyDeleteInsert, sourceTable: "public.items", explicit: true},
		{name: "single table effective scd2", strategy: config.StrategySCD2, sourceTable: "public.items"},
		{name: "multi table effective delete insert", strategy: config.StrategyDeleteInsert},
		{name: "multi table explicit scd2", strategy: config.StrategySCD2, explicit: true},
		{name: "single table explicit truncate insert", strategy: config.StrategyTruncateInsert, sourceTable: "public.items", explicit: true},
		{name: "multi table effective truncate insert", strategy: config.StrategyTruncateInsert},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := &invalidStrategyTimingSource{}
			dest := &invalidStrategyTimingDestination{}
			sourceFactoryCalls := 0
			destinationFactoryCalls := 0
			internalregistry.Default.RegisterSource([]string{"postgres+cdc"}, func() interface{} {
				sourceFactoryCalls++
				return src
			})
			internalregistry.Default.RegisterDestination([]string{"invalid-strategy-timing"}, func() interface{} {
				destinationFactoryCalls++
				return dest
			})

			cfg := config.DefaultConfig()
			cfg.SourceURI = "postgres+cdc://source/db?mode=batch"
			cfg.DestURI = "invalid-strategy-timing://destination/db"
			cfg.SourceTable = tt.sourceTable
			cfg.DestTable = "raw.items"
			cfg.IncrementalStrategy = tt.strategy
			cfg.IncrementalStrategyExplicit = tt.explicit

			err := New(cfg).Run(t.Context())
			require.ErrorContains(t, err, `incremental-strategy`)
			require.ErrorContains(t, err, string(tt.strategy))
			require.Zero(t, sourceFactoryCalls)
			require.Zero(t, destinationFactoryCalls)
			require.Zero(t, src.connectCalls)
			require.Zero(t, src.prepareCalls, "connector publication preparation ran for an invalid strategy")
			require.Zero(t, src.leaseCalls, "connector lease was acquired for an invalid strategy")
			require.Zero(t, src.getTableCalls)
			require.Zero(t, src.getTablesCalls)
			require.Zero(t, dest.connectCalls)
			require.Zero(t, dest.prepareCalls, "managed state tables were prepared for an invalid strategy")
			require.Zero(t, dest.stateWriteCalls, "managed state was written for an invalid strategy")
			require.Zero(t, dest.stateLoadCalls, "predecessor state adoption was probed for an invalid strategy")
			require.Zero(t, dest.targetClaimCalls, "destination target was claimed for an invalid strategy")
		})
	}
}

func TestPostgresCDCManagedStrategyDefaultsRemainValid(t *testing.T) {
	tests := []struct {
		name        string
		strategy    config.IncrementalStrategy
		explicit    bool
		fullRefresh bool
	}{
		{name: "default replace rewrites to merge", strategy: config.StrategyReplace},
		{name: "empty strategy resolves later"},
		{name: "explicit replace rewrites to merge", strategy: config.StrategyReplace, explicit: true},
		{name: "full refresh overrides scd2 with replace", strategy: config.StrategySCD2, explicit: true, fullRefresh: true},
		{name: "full refresh permits truncate insert override", strategy: config.StrategyTruncateInsert, explicit: true, fullRefresh: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.SourceURI = "postgres+cdc://source/db?mode=batch"
			cfg.IncrementalStrategy = tt.strategy
			cfg.IncrementalStrategyExplicit = tt.explicit
			cfg.FullRefresh = tt.fullRefresh
			require.NoError(t, validateManagedChangeConfig(cfg))
		})
	}
}

func TestMySQLCDCRejectsNonMergeIncrementalStrategies(t *testing.T) {
	for _, strategy := range []config.IncrementalStrategy{
		config.StrategyAppend,
		config.StrategyDeleteInsert,
		config.StrategySCD2,
		config.StrategyTruncateInsert,
	} {
		t.Run(string(strategy), func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.SourceURI = "mysql+cdc://source/app"
			cfg.IncrementalStrategy = strategy
			err := validateManagedChangeConfig(cfg)
			require.ErrorContains(t, err, "not supported for MySQL CDC")
		})
	}

	for _, strategy := range []config.IncrementalStrategy{"", config.StrategyMerge, config.StrategyReplace} {
		cfg := config.DefaultConfig()
		cfg.SourceURI = "mysql+cdc://source/app"
		cfg.IncrementalStrategy = strategy
		require.NoError(t, validateManagedChangeConfig(cfg))
	}
}

func TestPostgresCDCFailsClosedBeforeConnectorPreparationForUnsafeDestination(t *testing.T) {
	oldSource, err := internalregistry.Default.GetSourceConstructor("postgres+cdc")
	require.NoError(t, err)
	defer internalregistry.Default.RegisterSource([]string{"postgres+cdc"}, oldSource)

	src := &postgresCDCAdmissionSource{}
	dest := &postgresCDCAdmissionDestination{mockDestination: mockDestination{scheme: "unsafe-cdc-dest"}}
	internalregistry.Default.RegisterSource([]string{"postgres+cdc"}, func() interface{} { return src })
	internalregistry.Default.RegisterDestination([]string{"unsafe-cdc-dest"}, func() interface{} { return dest })

	cfg := config.DefaultConfig()
	cfg.SourceURI = "postgres+cdc://source/db?mode=batch"
	cfg.DestURI = "unsafe-cdc-dest://destination/db"
	cfg.SourceTable = "public.items"
	cfg.DestTable = "raw.items"

	err = New(cfg).Run(context.Background())
	require.ErrorContains(t, err, "cannot safely run managed CDC")
	require.ErrorContains(t, err, "destination-managed state")
	require.True(t, src.connected)
	require.True(t, src.closed)
	require.True(t, dest.connected)
	require.True(t, dest.closed)
	require.False(t, src.prepared, "connector preparation ran before destination safety validation")
}

func TestPostgresCDCTargetValidationRunsBeforeConnectorPreparation(t *testing.T) {
	oldSource, err := internalregistry.Default.GetSourceConstructor("postgres+cdc")
	require.NoError(t, err)
	defer internalregistry.Default.RegisterSource([]string{"postgres+cdc"}, oldSource)

	src := &postgresCDCAdmissionSource{}
	dest := &postgresCDCTargetValidationDestination{}
	dest.scheme = "target-preflight-dest"
	internalregistry.Default.RegisterSource([]string{"postgres+cdc"}, func() interface{} { return src })
	internalregistry.Default.RegisterDestination([]string{"target-preflight-dest"}, func() interface{} { return dest })

	cfg := config.DefaultConfig()
	cfg.SourceURI = "postgres+cdc://source/db?mode=batch"
	cfg.DestURI = "target-preflight-dest://destination/db"
	cfg.SourceTable = "public.items"
	cfg.DestTable = "legacy.dbo.items"

	err = New(cfg).Run(context.Background())
	require.ErrorContains(t, err, "compatibility level 120")
	require.Equal(t, cfg.DestTable, dest.target)
	require.True(t, dest.connected)
	require.True(t, dest.closed)
	require.False(t, src.prepared, "connector preparation ran before target compatibility validation")
}

func (l *mockConnectorLease) Done() <-chan struct{} { return l.done }
func (l *mockConnectorLease) Err() error            { return l.err }
func (l *mockConnectorLease) Release() error        { return nil }

func TestConnectorLeaseContextCancelsWithLossCause(t *testing.T) {
	loss := errors.New("lease backend terminated")
	lease := &mockConnectorLease{done: make(chan struct{}), err: loss}
	ctx, stop := connectorLeaseContext(context.Background(), lease)
	defer stop()
	close(lease.done)
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("lease loss did not cancel pipeline context")
	}
	if !errors.Is(context.Cause(ctx), loss) {
		t.Fatalf("pipeline cancellation cause = %v, want %v", context.Cause(ctx), loss)
	}
	if !errors.Is(context.Cause(ctx), source.ErrConnectorLeaseLost) {
		t.Fatalf("pipeline cancellation cause = %v, want connector lease sentinel", context.Cause(ctx))
	}
}

func TestConnectorLeaseContextNormalStopLeavesRunContextActive(t *testing.T) {
	lease := &mockConnectorLease{done: make(chan struct{})}
	ctx, stop := connectorLeaseContext(context.Background(), lease)
	stop()
	require.NoError(t, ctx.Err())
	require.NoError(t, context.Cause(ctx))
}

func TestConnectorLeaseContextKeepsGuardAfterParentCancellation(t *testing.T) {
	loss := errors.New("lease backend terminated during shutdown")
	lease := &mockConnectorLease{done: make(chan struct{}), err: loss}
	parent, cancelParent := context.WithCancel(context.Background())
	ctx, stop := connectorLeaseContext(parent, lease)
	defer stop()

	cancelParent()
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.Nil(t, source.ConnectorLeaseLoss(ctx))

	close(lease.done)
	require.Eventually(t, func() bool {
		return errors.Is(source.ConnectorLeaseLoss(ctx), loss)
	}, time.Second, time.Millisecond)
}

func TestResourceCloseContextDetachesCancellationAndPreservesValues(t *testing.T) {
	type contextKey string
	parent, cancelParent := context.WithCancel(context.WithValue(context.Background(), contextKey("key"), "value"))
	cancelParent()
	ctx, cancel := resourceCloseContext(parent)
	defer cancel()
	require.NoError(t, ctx.Err())
	require.Equal(t, "value", ctx.Value(contextKey("key")))
	_, hasDeadline := ctx.Deadline()
	require.True(t, hasDeadline)
}

func (m *mockDestination) GetTableSchema(_ context.Context, _ string) (*schema.TableSchema, error) {
	return m.tableSchema, nil
}

func (m *mockDestination) GetScheme() string {
	if m.scheme != "" {
		return m.scheme
	}
	return "mock"
}

type mockCDCResumeDestination struct {
	mockDestination
}

func (m *mockCDCResumeDestination) GetMaxCDCLSN(_ context.Context, _ string) (string, error) {
	return "", nil
}

type mockPredicateDestination struct {
	mockDestination
}

func (m *mockPredicateDestination) SupportsIncrementalPredicate() bool {
	return true
}

type mockManagedCDCStateDestination struct {
	mockCDCResumeDestination
}

func (m *mockManagedCDCStateDestination) SupportsCDCMerge() bool { return true }

func (m *mockManagedCDCStateDestination) SupportsCDCUnchangedCols() bool { return true }

type mockUnsafeToastCDCStateDestination struct {
	mockManagedCDCStateDestination
}

func (m *mockUnsafeToastCDCStateDestination) SupportsCDCUnchangedCols() bool { return false }

func (m *mockManagedCDCStateDestination) CDCTargetIncarnation(_ context.Context, table string) (string, bool, error) {
	return "incarnation:" + table, true, nil
}

type canonicalIdentityDestination struct {
	mockManagedCDCStateDestination
	identity string
	calls    int
	targets  []string
}

func (d *canonicalIdentityDestination) CanonicalCDCTarget(_ context.Context, target string) (string, error) {
	d.calls++
	d.targets = append(d.targets, target)
	if d.identity == "" {
		return strings.ToLower(target), nil
	}
	return d.identity, nil
}

func (d *canonicalIdentityDestination) DestTableName(destSchema, sourceTable string) string {
	return destSchema + "." + strings.ReplaceAll(sourceTable, ".", "_")
}

func (m *mockManagedCDCStateDestination) ClaimCDCTarget(_ context.Context, _ string, _ destination.CDCTargetClaim) error {
	return nil
}

type claimCountingManagedDestination struct {
	mockManagedCDCStateDestination
	claimCalls int
}

func (d *claimCountingManagedDestination) ClaimCDCTarget(_ context.Context, _ string, _ destination.CDCTargetClaim) error {
	d.claimCalls++
	return nil
}

type staticMultiTableSource struct {
	tables []source.SourceTableInfo
}

type emptyInitialManagedDestination struct {
	mockManagedCDCStateDestination
}

func (d *emptyInitialManagedDestination) PrepareTable(context.Context, destination.PrepareOptions) error {
	return nil
}

func (d *emptyInitialManagedDestination) WriteCDCState(_ context.Context, records <-chan source.RecordBatchResult, _ destination.WriteOptions) error {
	for result := range records {
		if result.Batch != nil {
			result.Batch.Release()
		}
		if result.Err != nil {
			return result.Err
		}
	}
	return nil
}

type constantMultiTableNamer struct{}

func (constantMultiTableNamer) DestTableName(string, string) string {
	return "project.landing.shared"
}

type caseFoldingManagedDestination struct {
	mockManagedCDCStateDestination
	owners map[string]string
}

func (d *caseFoldingManagedDestination) DestTableName(destSchema, sourceTable string) string {
	return destSchema + "." + strings.ReplaceAll(sourceTable, ".", "_")
}

func (d *caseFoldingManagedDestination) PrepareTable(context.Context, destination.PrepareOptions) error {
	return nil
}

func (d *caseFoldingManagedDestination) ClaimCDCTarget(_ context.Context, _ string, claim destination.CDCTargetClaim) error {
	target := strings.ToLower(claim.DestinationTable)
	owner := destination.CDCTargetOwnerID(claim.ConnectorID, claim.SourceTable)
	if existing := d.owners[target]; existing != "" && existing != owner {
		return fmt.Errorf("destination table %q is already claimed", target)
	}
	d.owners[target] = owner
	return nil
}

func (s *staticMultiTableSource) Schemes() []string { return []string{"postgres+cdc"} }
func (s *staticMultiTableSource) Connect(context.Context, string) error {
	return nil
}
func (s *staticMultiTableSource) Close(context.Context) error { return nil }
func (s *staticMultiTableSource) GetTable(context.Context, source.TableRequest) (source.SourceTable, error) {
	return nil, errors.New("single-table lookup is not supported")
}
func (s *staticMultiTableSource) HandlesIncrementality() bool { return true }
func (s *staticMultiTableSource) IsMultiTable() bool          { return true }
func (s *staticMultiTableSource) GetTables(context.Context) ([]source.SourceTableInfo, error) {
	return append([]source.SourceTableInfo(nil), s.tables...), nil
}

func (s *staticMultiTableSource) ReadAll(context.Context, source.MultiTableReadOptions) (<-chan source.RecordBatchResult, error) {
	return nil, errors.New("ReadAll must not run after a destination mapping collision")
}

func TestRunMultiTableRejectsDestinationCollisionBeforeClaim(t *testing.T) {
	tables := []source.SourceTableInfo{
		{Name: "a.b_c", DestSchema: "landing", Schema: &schema.TableSchema{}},
		{Name: "a_b.c", DestSchema: "landing", Schema: &schema.TableSchema{}},
	}
	dest := &claimCountingManagedDestination{}
	cfg := config.DefaultConfig()
	cfg.SourceURI = "postgres+cdc://source/db"
	cfg.NoLoadTimestamp = true
	cfg.Progress = config.ProgressLog
	p := &Pipeline{config: cfg, dest: dest, cdcConnectorID: "connector"}

	err := p.runMultiTable(t.Context(), &staticMultiTableSource{tables: tables})
	require.ErrorContains(t, err, "multi-table destination collision")
	require.ErrorContains(t, err, "a.b_c")
	require.ErrorContains(t, err, "a_b.c")
	require.ErrorContains(t, err, "landing.a_b_c")
	require.Zero(t, dest.claimCalls, "collision was detected after a durable target claim")
}

func TestRunMultiTableStreamingPostgresCDCDoesNotExitOnEmptyInitialTableSet(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SourceURI = "postgres+cdc://source/db"
	cfg.Stream = true
	cfg.IncrementalStrategy = config.StrategyMerge
	cfg.NoLoadTimestamp = true
	cfg.FlushInterval = time.Hour
	cfg.FlushRecords = 1
	p := &Pipeline{config: cfg, dest: &emptyInitialManagedDestination{}, cdcConnectorID: "connector"}

	err := p.runMultiTable(t.Context(), &staticMultiTableSource{})
	require.ErrorContains(t, err, "ReadAll must not run", "streaming CDC must reach ReadAll so a table appearing after the first GetTables call can be announced")
}

func TestMultiTableDestinationNamesChecksDestinationOverrideCollisions(t *testing.T) {
	tables := []source.SourceTableInfo{{Name: "public.orders"}, {Name: "sales.orders"}}

	_, err := multiTableDestinationNames(tables, "", constantMultiTableNamer{})
	require.ErrorContains(t, err, "multi-table destination collision")
	require.ErrorContains(t, err, "project.landing.shared")
}

func TestMultiTableDestinationNamesShortensLongFinalComponents(t *testing.T) {
	schemaPrefix := strings.Repeat("s", 40)
	tablePrefix := strings.Repeat("t", 40)
	tables := []source.SourceTableInfo{
		{Name: schemaPrefix + "." + tablePrefix + "_one", DestSchema: "landing"},
		{Name: schemaPrefix + "." + tablePrefix + "_two", DestSchema: "landing"},
	}

	first, err := multiTableDestinationNames(tables, "postgres", nil)
	require.NoError(t, err)
	second, err := multiTableDestinationNames(tables, "postgres", nil)
	require.NoError(t, err)
	require.Equal(t, first, second, "destination mapping changed across restart")
	require.NotEqual(t, first[tables[0].Name], first[tables[1].Name])
	for _, table := range tables {
		mapped := first[table.Name]
		require.True(t, strings.HasPrefix(mapped, "landing."))
		parts := tablename.Split(mapped)
		require.Len(t, parts, 2)
		require.LessOrEqual(t, len(parts[1]), destination.MaxIdentifierLength("postgres"))
	}
}

func TestMultiTableDestinationNamesShortensMultibyteFinalComponents(t *testing.T) {
	tables := []source.SourceTableInfo{
		{Name: strings.Repeat("é", 20) + "." + strings.Repeat("界", 14) + "一", DestSchema: "landing"},
		{Name: strings.Repeat("é", 20) + "." + strings.Repeat("界", 14) + "二", DestSchema: "landing"},
	}

	first, err := multiTableDestinationNames(tables, "postgres", nil)
	require.NoError(t, err)
	second, err := multiTableDestinationNames(tables, "postgres", nil)
	require.NoError(t, err)
	require.Equal(t, first, second, "multibyte destination mapping changed across restart")
	require.NotEqual(t, first[tables[0].Name], first[tables[1].Name])
	for _, table := range tables {
		mapped := first[table.Name]
		require.True(t, utf8.ValidString(mapped))
		parts := tablename.Split(mapped)
		require.Len(t, parts, 2)
		require.LessOrEqual(t, len(parts[1]), destination.MaxIdentifierLength("postgres"))
	}
}

func TestRunMultiTableRejectsPhysicalAliasClaimsBeforeEvolution(t *testing.T) {
	tables := []source.SourceTableInfo{
		{Name: "public.Orders", DestSchema: "landing", Schema: &schema.TableSchema{}},
		{Name: "public.orders", DestSchema: "landing", Schema: &schema.TableSchema{}},
	}
	dest := &caseFoldingManagedDestination{owners: make(map[string]string)}
	cfg := config.DefaultConfig()
	cfg.SourceURI = "postgres+cdc://source/db"
	cfg.NoLoadTimestamp = true
	cfg.Progress = config.ProgressLog
	p := &Pipeline{config: cfg, dest: dest, cdcConnectorID: "connector"}

	err := p.runMultiTable(t.Context(), &staticMultiTableSource{tables: tables})
	require.ErrorContains(t, err, "already claimed")
	require.Empty(t, p.schemaComparison, "physical alias reached schema evolution")
}

type missingTableCDCSource struct {
	connected bool
	closed    bool
	lease     *mockConnectorLease
}

func (s *missingTableCDCSource) Schemes() []string { return []string{"postgres+cdc"} }
func (s *missingTableCDCSource) Connect(context.Context, string) error {
	s.connected = true
	return nil
}

func (s *missingTableCDCSource) Close(context.Context) error {
	s.closed = true
	return nil
}

func (s *missingTableCDCSource) GetTable(context.Context, source.TableRequest) (source.SourceTable, error) {
	return nil, errors.New("GetTable must not run for a confirmed missing source table")
}
func (s *missingTableCDCSource) HandlesIncrementality() bool { return true }
func (s *missingTableCDCSource) ConnectorIdentity(context.Context) (source.ConnectorIdentity, error) {
	return source.ConnectorIdentity{Database: "source_db", Connector: "source-instance"}, nil
}

func (s *missingTableCDCSource) AcquireConnectorLease(context.Context, source.ConnectorLeaseOptions) (source.ConnectorLease, error) {
	return s.lease, nil
}

func (s *missingTableCDCSource) TableExists(context.Context, string) (bool, error) {
	return false, nil
}

type missingSourceManagedDestination struct {
	mockManagedCDCStateDestination
	connected     bool
	closed        bool
	prepareTables []string
	claimCalls    int
}

func (d *missingSourceManagedDestination) Connect(context.Context, string) error {
	d.connected = true
	return nil
}

func (d *missingSourceManagedDestination) Close(context.Context) error {
	d.closed = true
	return nil
}

func (d *missingSourceManagedDestination) PrepareTable(_ context.Context, opts destination.PrepareOptions) error {
	d.prepareTables = append(d.prepareTables, opts.Table)
	return nil
}

func (d *missingSourceManagedDestination) ClaimCDCTarget(context.Context, string, destination.CDCTargetClaim) error {
	d.claimCalls++
	return nil
}

func (d *missingSourceManagedDestination) Write(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	return d.WriteParallel(ctx, records, opts)
}

func (d *missingSourceManagedDestination) WriteParallel(_ context.Context, records <-chan source.RecordBatchResult, _ destination.WriteOptions) error {
	for result := range records {
		if result.Batch != nil {
			result.Batch.Release()
		}
		if result.Err != nil {
			return result.Err
		}
	}
	return nil
}

func TestRunMissingSourceTableDoesNotClaimDestination(t *testing.T) {
	oldSource, err := internalregistry.Default.GetSourceConstructor("postgres+cdc")
	require.NoError(t, err)
	defer internalregistry.Default.RegisterSource([]string{"postgres+cdc"}, oldSource)

	src := &missingTableCDCSource{lease: &mockConnectorLease{done: make(chan struct{})}}
	dest := &missingSourceManagedDestination{}
	internalregistry.Default.RegisterSource([]string{"postgres+cdc"}, func() interface{} { return src })
	internalregistry.Default.RegisterDestination([]string{"missing-source-managed"}, func() interface{} { return dest })

	cfg := config.DefaultConfig()
	cfg.SourceURI = "postgres+cdc://source/source_db?mode=batch"
	cfg.SourceTable = "public.typo_orders"
	cfg.DestURI = "missing-source-managed://destination/database"
	cfg.DestTable = "landing.orders"

	err = New(cfg).Run(t.Context())
	require.ErrorContains(t, err, `source table "public.typo_orders" does not exist`)
	require.True(t, src.connected)
	require.True(t, src.closed)
	require.True(t, dest.connected)
	require.True(t, dest.closed)
	require.Zero(t, dest.claimCalls, "missing source table poisoned the durable target registry")
	require.Len(t, dest.prepareTables, 1, "missing source table should only invalidate resumable state")
	require.Contains(t, dest.prepareTables[0], "cdc_state")
	for _, table := range dest.prepareTables {
		require.NotContains(t, table, "cdc_targets", "missing source table prepared the durable target registry")
	}
}

type mockLegacyBootstrapDestination struct {
	mockManagedCDCStateDestination
	maxLSN  string
	entries []destination.CDCStateEntry
}

func (m *mockLegacyBootstrapDestination) GetMaxCDCLSN(_ context.Context, _ string) (string, error) {
	return m.maxLSN, nil
}

func (m *mockLegacyBootstrapDestination) PrepareTable(_ context.Context, _ destination.PrepareOptions) error {
	return nil
}

func (m *mockLegacyBootstrapDestination) WriteCDCState(_ context.Context, records <-chan source.RecordBatchResult, _ destination.WriteOptions) error {
	for result := range records {
		if result.Err != nil {
			return result.Err
		}
		if result.Batch == nil {
			continue
		}
		batch := result.Batch
		for i := 0; i < int(batch.NumRows()); i++ {
			m.entries = append(m.entries, destination.CDCStateEntry{
				EventID:          batch.Column(0).(*array.String).Value(i),
				SourceTable:      batch.Column(3).(*array.String).Value(i),
				DestinationTable: batch.Column(4).(*array.String).Value(i),
				StateKind:        batch.Column(5).(*array.String).Value(i),
				Generation:       batch.Column(6).(*array.Int64).Value(i),
				Status:           batch.Column(7).(*array.String).Value(i),
				Position:         batch.Column(8).(*array.String).Value(i),
			})
		}
		batch.Release()
	}
	return nil
}

func (m *mockLegacyBootstrapDestination) LoadCDCState(_ context.Context, _, _ string) ([]destination.CDCStateEntry, error) {
	return append([]destination.CDCStateEntry(nil), m.entries...), nil
}

func (m *mockLegacyBootstrapDestination) DeleteCDCStateEvents(_ context.Context, _, _ string, _ []string) error {
	return nil
}

type mockValidatedManagedCDCStateDestination struct {
	mockManagedCDCStateDestination
	validationErr error
}

type mockMySQLCDCFencedDestination struct {
	mockManagedCDCStateDestination
}

func (m *mockMySQLCDCFencedDestination) SupportsCDCConditionalMerge() bool {
	return true
}

func (m *mockMySQLCDCFencedDestination) TruncateCDCTableIfIncarnation(context.Context, string, string) error {
	return nil
}

func (m *mockMySQLCDCFencedDestination) AcquireManagedCDCRunLease(context.Context, string) (source.ConnectorLease, error) {
	return &mockConnectorLease{done: make(chan struct{})}, nil
}

func (m *mockMySQLCDCFencedDestination) ClaimAndPrepareEmptyCDCTarget(context.Context, string, destination.CDCTargetClaim, destination.PrepareOptions) (string, error) {
	return "incarnation", nil
}

func (m *mockMySQLCDCFencedDestination) SupportsCDCConditionalSwap() bool {
	return true
}

func (m *mockMySQLCDCFencedDestination) CDCConditionalSwapIncarnations(context.Context, string, string) (string, string, error) {
	return "staging-incarnation", "result-incarnation", nil
}

func (m *mockValidatedManagedCDCStateDestination) ValidateManagedCDCState() error {
	return m.validationErr
}

type mockUnprunableCDCStateDestination struct {
	mockCDCResumeDestination
}

type mockUnfencedCDCStateDestination struct {
	mockCDCResumeDestination
}

type mockUnclaimableCDCStateDestination struct {
	mockCDCResumeDestination
}

func (m *mockUnprunableCDCStateDestination) TruncateTable(_ context.Context, _ string) error {
	return nil
}

func (m *mockUnprunableCDCStateDestination) LoadCDCState(_ context.Context, _, _ string) ([]destination.CDCStateEntry, error) {
	return nil, nil
}

func (m *mockUnfencedCDCStateDestination) TruncateTable(_ context.Context, _ string) error {
	return nil
}

func (m *mockUnfencedCDCStateDestination) LoadCDCState(_ context.Context, _, _ string) ([]destination.CDCStateEntry, error) {
	return nil, nil
}

func (m *mockUnfencedCDCStateDestination) DeleteCDCStateEvents(_ context.Context, _, _ string, _ []string) error {
	return nil
}

func (m *mockUnclaimableCDCStateDestination) TruncateTable(_ context.Context, _ string) error {
	return nil
}

func (m *mockUnclaimableCDCStateDestination) LoadCDCState(_ context.Context, _, _ string) ([]destination.CDCStateEntry, error) {
	return nil, nil
}

func (m *mockUnclaimableCDCStateDestination) LoadCDCStateFence(_ context.Context, _, _ string) (destination.CDCStateFence, error) {
	return destination.CDCStateFence{}, nil
}

func (m *mockUnclaimableCDCStateDestination) DeleteCDCStateEvents(_ context.Context, _, _ string, _ []string) error {
	return nil
}

func (m *mockManagedCDCStateDestination) TruncateTable(_ context.Context, _ string) error {
	return nil
}

func (m *mockManagedCDCStateDestination) LoadCDCState(_ context.Context, _, _ string) ([]destination.CDCStateEntry, error) {
	return nil, nil
}

func (m *mockManagedCDCStateDestination) LoadCDCStateFence(_ context.Context, _, _ string) (destination.CDCStateFence, error) {
	return destination.CDCStateFence{}, nil
}

func (m *mockLegacyBootstrapDestination) LoadCDCStateFence(_ context.Context, _, _ string) (destination.CDCStateFence, error) {
	var fence destination.CDCStateFence
	for _, entry := range m.entries {
		if entry.StateKind != "run" {
			continue
		}
		if entry.Generation > fence.Generation {
			fence.Generation = entry.Generation
			fence.RunEventIDs = fence.RunEventIDs[:0]
		}
		if entry.Generation == fence.Generation {
			fence.RunEventIDs = append(fence.RunEventIDs, entry.EventID)
		}
	}
	return fence, nil
}

func (m *mockManagedCDCStateDestination) DeleteCDCStateEvents(_ context.Context, _, _ string, _ []string) error {
	return nil
}

type mockSchemaEvolutionDestination struct {
	mockDestination
}

type normalizingMockSchemaEvolutionDestination struct {
	*mockSchemaEvolutionDestination
}

func (m *normalizingMockSchemaEvolutionDestination) NormalizeSchemaEvolutionColumn(col schema.Column) schema.Column {
	if col.DataType == schema.TypeBoolean {
		col.DataType = schema.TypeInt64
	}
	return col
}

func (m *mockSchemaEvolutionDestination) ApplySchemaEvolution(_ context.Context, _ string, _ *schemaevolution.SchemaComparison) ([]string, error) {
	return nil, nil
}

func (m *mockSchemaEvolutionDestination) SupportsColumnTypeChanges() bool {
	return true
}

func TestRunRejectsChangeTrackingSQLLimitBeforeSourceLookup(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SourceURI = "mssql+ct://example:1433/app"
	cfg.SourceTable = "dbo.items"
	cfg.DestURI = "unsupported-destination://out"
	cfg.SQLLimit = 10

	err := New(cfg).Run(context.Background())
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "sql-limit") {
		t.Fatalf("expected sql-limit validation error, got %v", err)
	}
	if strings.Contains(err.Error(), "failed to get source") || strings.Contains(err.Error(), "failed to get destination") {
		t.Fatalf("expected validation before source/destination lookup, got %v", err)
	}
}

func TestValidateChangeTrackingDestinationAcceptsResumeProvider(t *testing.T) {
	err := validateChangeTrackingDestination(&mockCDCResumeDestination{
		mockDestination: mockDestination{scheme: "mock"},
	})
	if err != nil {
		t.Fatalf("expected resume provider to pass validation, got %v", err)
	}
}

func TestRejectUnprovenLegacyCDCTarget(t *testing.T) {
	ctx := context.Background()
	dest := &mockLegacyBootstrapDestination{
		mockManagedCDCStateDestination: mockManagedCDCStateDestination{
			mockCDCResumeDestination: mockCDCResumeDestination{mockDestination: mockDestination{
				tableSchema: &schema.TableSchema{},
			}},
		},
		maxLSN: "00000000/0000002A",
	}
	err := rejectUnprovenLegacyCDCTarget(ctx, dest, "public.orders", "raw.orders")
	if err == nil || !strings.Contains(err.Error(), "--full-refresh") {
		t.Fatalf("expected fail-closed full-refresh error, got %v", err)
	}
	if len(dest.entries) != 0 {
		t.Fatalf("raw target LSN was certified into managed state: %+v", dest.entries)
	}

	dest.maxLSN = ""
	if err := rejectUnprovenLegacyCDCTarget(ctx, dest, "public.orders", "raw.orders"); err != nil {
		t.Fatalf("empty target must allow a snapshot: %v", err)
	}
}

func TestManagedCDCStateDestinationRequiresReaderFencePrunerAndTruncate(t *testing.T) {
	if supportsDestinationManagedCDCState(&mockDestination{}) {
		t.Fatal("destination without CDC capabilities must not use managed CDC state")
	}
	if supportsDestinationManagedCDCState(&mockCDCResumeDestination{}) {
		t.Fatal("legacy resume-only destination must not use managed CDC state")
	}
	if supportsDestinationManagedCDCState(&mockUnprunableCDCStateDestination{}) {
		t.Fatal("destination without connector-scoped pruning must not use managed CDC state")
	}
	if supportsDestinationManagedCDCState(&mockUnfencedCDCStateDestination{}) {
		t.Fatal("destination without compact ownership fencing must not use managed CDC state")
	}
	if !supportsDestinationManagedCDCState(&mockManagedCDCStateDestination{}) {
		t.Fatal("destination with state-read, state-prune, and truncate capabilities must use managed CDC state")
	}
}

func TestValidateDestinationManagedCDCState(t *testing.T) {
	unsupported := &mockDestination{scheme: "unsupported"}
	if err := validateDestinationManagedCDCState(unsupported); err == nil || !strings.Contains(err.Error(), "destination-managed state") {
		t.Fatalf("destination without managed state validation error = %v", err)
	}
	legacyOnly := &mockCDCResumeDestination{mockDestination: mockDestination{scheme: "legacy-only"}}
	if err := validateDestinationManagedCDCState(legacyOnly); err == nil || !strings.Contains(err.Error(), "cannot safely run managed CDC") {
		t.Fatalf("legacy resume-only destination validation error = %v", err)
	}
	unclaimable := &mockUnclaimableCDCStateDestination{}
	if !supportsDestinationManagedCDCState(unclaimable) {
		t.Fatal("state-capable destination should reach managed CDC validation")
	}
	if err := validateDestinationManagedCDCState(unclaimable); err == nil || !strings.Contains(err.Error(), "atomic destination-table claims") {
		t.Fatalf("unclaimable destination validation error = %v", err)
	}
	if err := validateDestinationManagedCDCState(&mockManagedCDCStateDestination{}); err != nil {
		t.Fatalf("destination without validator was rejected: %v", err)
	}
	if err := validateDestinationManagedCDCState(&mockUnsafeToastCDCStateDestination{}); err == nil || !strings.Contains(err.Error(), "unchanged TOAST") {
		t.Fatalf("destination without unchanged-TOAST merge support validation error = %v", err)
	}
	if err := validateDestinationManagedCDCState(&mockValidatedManagedCDCStateDestination{}); err != nil {
		t.Fatalf("valid managed CDC destination was rejected: %v", err)
	}
	err := validateDestinationManagedCDCState(&mockValidatedManagedCDCStateDestination{validationErr: errors.New("consistency ONE is unsafe")})
	if err == nil || !strings.Contains(err.Error(), "consistency ONE is unsafe") {
		t.Fatalf("validation error = %v, want consistency rejection", err)
	}
}

func TestValidateMySQLCDCMutationFencing(t *testing.T) {
	unsupported := &mockManagedCDCStateDestination{}
	require.ErrorContains(t, validateMySQLCDCMutationFencing(unsupported), "atomic target-incarnation fencing for merge")
	require.NoError(t, validateMySQLCDCMutationFencing(&mockMySQLCDCFencedDestination{}))
}

func TestValidateChangeTrackingDestinationRequiresResumeProvider(t *testing.T) {
	err := validateChangeTrackingDestination(&mockDestination{scheme: "mock"})

	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "resume cursors") {
		t.Fatalf("expected resume cursor error, got %v", err)
	}
}

func TestValidateIncrementalPredicate(t *testing.T) {
	cfg := &config.IngestConfig{IncrementalPredicate: "t.event_date >= DATE '2026-01-01'"}
	supported := &mockPredicateDestination{mockDestination: mockDestination{scheme: "bigquery"}}

	if err := validateIncrementalPredicate(cfg, supported, config.StrategyMerge); err != nil {
		t.Fatalf("expected merge predicate to be accepted, got %v", err)
	}
	if err := validateIncrementalPredicate(cfg, supported, config.StrategyAppend); err == nil {
		t.Fatal("expected non-merge strategy to be rejected")
	}
	if err := validateIncrementalPredicate(cfg, &mockDestination{scheme: "mock"}, config.StrategyMerge); err == nil {
		t.Fatal("expected unsupported destination to be rejected")
	}
}

func TestValidateExtractPartitionSupportAllowsDifferentIncrementalKey(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.ExtractPartitionBy = "created_at"
	cfg.IncrementalKey = "updated_at"

	table := &source.DynamicSourceTable{
		TableName:                        "orders",
		TableSupportsExtractPartitioning: true,
	}

	if err := validateExtractPartitionSupport(cfg, table); err != nil {
		t.Fatalf("expected different keys to pass validation, got %v", err)
	}
}

func TestValidateExtractPartitionSupportAllowsMatchingIncrementalKey(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.ExtractPartitionBy = "created_at"
	cfg.IncrementalKey = "CREATED_AT"

	table := &source.DynamicSourceTable{
		TableName:                        "orders",
		TableSupportsExtractPartitioning: true,
	}

	if err := validateExtractPartitionSupport(cfg, table); err != nil {
		t.Fatalf("expected matching keys to pass validation, got %v", err)
	}
}

func TestValidateChangeTrackingDestinationRunsForFullRefreshRequirement(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SourceURI = "mssql+ct://example:1433/app"
	cfg.SourceTable = "dbo.items"
	cfg.DestURI = "mock://dest"
	cfg.FullRefresh = true

	requireChangeTrackingValidation := isChangeTrackingSource(cfg.SourceURI)
	if !requireChangeTrackingValidation {
		t.Fatal("expected mssql+ct URI to require destination validation")
	}
	err := validateChangeTrackingDestination(&mockCDCResumeDestination{
		mockDestination: mockDestination{scheme: "mock"},
	})
	if err != nil {
		t.Fatalf("expected full-refresh validation to accept resume provider, got %v", err)
	}
}

func TestSetupNamingConvention(t *testing.T) {
	camelCaseSource := schema.TableSchema{
		Columns: []schema.Column{
			{Name: "date", DataType: schema.TypeDate},
			{Name: "currencyCode", DataType: schema.TypeString},
			{Name: "baseCurrency", DataType: schema.TypeString},
			{Name: "rate", DataType: schema.TypeFloat64},
		},
		PrimaryKeys:    []string{"date", "currencyCode", "baseCurrency"},
		IncrementalKey: "currencyCode",
	}

	ingestrDestSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "date", DataType: schema.TypeDate},
			{Name: "currency_code", DataType: schema.TypeString},
			{Name: "base_currency", DataType: schema.TypeString},
			{Name: "rate", DataType: schema.TypeFloat64},
			{Name: "_dlt_load_id", DataType: schema.TypeString},
			{Name: "_dlt_id", DataType: schema.TypeString},
		},
	}

	tests := []struct {
		name               string
		schemaNaming       string
		destSchema         *schema.TableSchema
		wantColumns        []string
		wantPrimaryKeys    []string
		wantIncrementalKey string
		wantRenamer        bool
	}{
		// SchemaNaming="" defaults to snake_case
		{
			name:               "default naming converts to snake_case",
			schemaNaming:       "",
			destSchema:         ingestrDestSchema,
			wantColumns:        []string{"date", "currency_code", "base_currency", "rate"},
			wantPrimaryKeys:    []string{"date", "currency_code", "base_currency"},
			wantIncrementalKey: "currency_code",
			wantRenamer:        true,
		},
		// SchemaNaming="auto" with ingestr dest → detects snake_case
		{
			name:               "auto with ingestr dest detects snake_case",
			schemaNaming:       "auto",
			destSchema:         ingestrDestSchema,
			wantColumns:        []string{"date", "currency_code", "base_currency", "rate"},
			wantPrimaryKeys:    []string{"date", "currency_code", "base_currency"},
			wantIncrementalKey: "currency_code",
			wantRenamer:        true,
		},
		// Dest table doesn't exist yet → default still uses snake_case
		{
			name:               "default naming with no dest table uses snake_case",
			schemaNaming:       "",
			destSchema:         nil,
			wantColumns:        []string{"date", "currency_code", "base_currency", "rate"},
			wantPrimaryKeys:    []string{"date", "currency_code", "base_currency"},
			wantIncrementalKey: "currency_code",
			wantRenamer:        true,
		},
		// Explicit "direct" with ingestr dest → respects user choice, no renaming
		{
			name:               "explicit direct with ingestr dest stays direct",
			schemaNaming:       "direct",
			destSchema:         ingestrDestSchema,
			wantColumns:        []string{"date", "currencyCode", "baseCurrency", "rate"},
			wantPrimaryKeys:    []string{"date", "currencyCode", "baseCurrency"},
			wantIncrementalKey: "currencyCode",
			wantRenamer:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Copy source schema so each test starts fresh
			src := camelCaseSource
			src.Columns = make([]schema.Column, len(camelCaseSource.Columns))
			copy(src.Columns, camelCaseSource.Columns)
			src.PrimaryKeys = make([]string, len(camelCaseSource.PrimaryKeys))
			copy(src.PrimaryKeys, camelCaseSource.PrimaryKeys)

			p := &Pipeline{
				config: &config.IngestConfig{
					DestTable:    "exchange_rates",
					SchemaNaming: tt.schemaNaming,
				},
				dest: &mockDestination{tableSchema: tt.destSchema},
			}

			err := p.setupNamingConvention(context.Background(), &src)
			if err != nil {
				t.Fatalf("setupNamingConvention() error = %v", err)
			}

			gotColumns := src.ColumnNames()
			if len(gotColumns) != len(tt.wantColumns) {
				t.Fatalf("columns length = %d, want %d", len(gotColumns), len(tt.wantColumns))
			}
			for i, want := range tt.wantColumns {
				if gotColumns[i] != want {
					t.Errorf("column[%d] = %q, want %q", i, gotColumns[i], want)
				}
			}

			if len(src.PrimaryKeys) != len(tt.wantPrimaryKeys) {
				t.Fatalf("primary keys length = %d, want %d", len(src.PrimaryKeys), len(tt.wantPrimaryKeys))
			}
			for i, want := range tt.wantPrimaryKeys {
				if src.PrimaryKeys[i] != want {
					t.Errorf("primary key[%d] = %q, want %q", i, src.PrimaryKeys[i], want)
				}
			}

			if src.IncrementalKey != tt.wantIncrementalKey {
				t.Errorf("incremental key = %q, want %q", src.IncrementalKey, tt.wantIncrementalKey)
			}

			hasRenamer := p.columnRenamer != nil && p.columnRenamer.HasRenames()
			if hasRenamer != tt.wantRenamer {
				t.Errorf("has renamer = %v, want %v", hasRenamer, tt.wantRenamer)
			}
		})
	}

	// Test with mostly single-word columns (team_members scenario):
	// only 1 camelCase column vs 3 single-word columns, ingestr dest must still convert
	t.Run("ingestr dest with mostly single-word columns still converts to snake_case", func(t *testing.T) {
		src := schema.TableSchema{
			Columns: []schema.Column{
				{Name: "isRemoved", DataType: schema.TypeBoolean},
				{Name: "name", DataType: schema.TypeString},
				{Name: "role", DataType: schema.TypeString},
				{Name: "email", DataType: schema.TypeString},
			},
		}
		destSchema := &schema.TableSchema{
			Columns: []schema.Column{
				{Name: "is_removed", DataType: schema.TypeBoolean},
				{Name: "name", DataType: schema.TypeString},
				{Name: "role", DataType: schema.TypeString},
				{Name: "email", DataType: schema.TypeString},
				{Name: "_dlt_load_id", DataType: schema.TypeString},
				{Name: "_dlt_id", DataType: schema.TypeString},
			},
		}

		p := &Pipeline{
			config: &config.IngestConfig{
				DestTable: "team_members",
			},
			dest: &mockDestination{tableSchema: destSchema},
		}

		err := p.setupNamingConvention(context.Background(), &src)
		if err != nil {
			t.Fatalf("setupNamingConvention() error = %v", err)
		}

		wantColumns := []string{"is_removed", "name", "role", "email"}
		gotColumns := src.ColumnNames()
		if len(gotColumns) != len(wantColumns) {
			t.Fatalf("columns length = %d, want %d", len(gotColumns), len(wantColumns))
		}
		for i, want := range wantColumns {
			if gotColumns[i] != want {
				t.Errorf("column[%d] = %q, want %q", i, gotColumns[i], want)
			}
		}

		hasRenamer := p.columnRenamer != nil && p.columnRenamer.HasRenames()
		if !hasRenamer {
			t.Error("expected column renamer to be set")
		}
	})
}

func TestApplyColumnOverrides(t *testing.T) {
	tests := []struct {
		name      string
		columns   string
		schema    schema.TableSchema
		wantTypes map[string]schema.DataType
		wantErr   bool
	}{
		{
			name:    "no overrides",
			columns: "",
			schema: schema.TableSchema{
				Columns: []schema.Column{
					{Name: "id", DataType: schema.TypeString},
				},
			},
			wantTypes: map[string]schema.DataType{"id": schema.TypeString},
		},
		{
			name:    "override inferred string to timestamptz",
			columns: "LastViewedDate:timestamptz",
			schema: schema.TableSchema{
				Columns: []schema.Column{
					{Name: "Id", DataType: schema.TypeString},
					{Name: "LastViewedDate", DataType: schema.TypeString},
					{Name: "Name", DataType: schema.TypeString},
				},
			},
			wantTypes: map[string]schema.DataType{
				"Id":             schema.TypeString,
				"LastViewedDate": schema.TypeTimestampTZ,
				"Name":           schema.TypeString,
			},
		},
		{
			name:    "multiple overrides",
			columns: "score:float64,count:bigint,created_at:timestamp",
			schema: schema.TableSchema{
				Columns: []schema.Column{
					{Name: "score", DataType: schema.TypeString},
					{Name: "count", DataType: schema.TypeString},
					{Name: "created_at", DataType: schema.TypeString},
					{Name: "name", DataType: schema.TypeString},
				},
			},
			wantTypes: map[string]schema.DataType{
				"score":      schema.TypeFloat64,
				"count":      schema.TypeInt64,
				"created_at": schema.TypeTimestampTZ,
				"name":       schema.TypeString,
			},
		},
		{
			name:    "override column not in schema is ignored",
			columns: "nonexistent:bigint",
			schema: schema.TableSchema{
				Columns: []schema.Column{
					{Name: "id", DataType: schema.TypeString},
				},
			},
			wantTypes: map[string]schema.DataType{"id": schema.TypeString},
		},
		{
			name:    "invalid override format returns error",
			columns: "badformat",
			schema: schema.TableSchema{
				Columns: []schema.Column{
					{Name: "id", DataType: schema.TypeString},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := tt.schema
			src.Columns = make([]schema.Column, len(tt.schema.Columns))
			copy(src.Columns, tt.schema.Columns)

			p := &Pipeline{
				config: &config.IngestConfig{
					Columns: tt.columns,
				},
			}

			err := p.applyColumnOverrides(&src)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("applyColumnOverrides() error = %v", err)
			}

			for _, col := range src.Columns {
				wantType, ok := tt.wantTypes[col.Name]
				if !ok {
					t.Errorf("unexpected column %q", col.Name)
					continue
				}
				if col.DataType != wantType {
					t.Errorf("column %q: type = %v, want %v", col.Name, col.DataType, wantType)
				}
			}
		})
	}
}

// mutableMockDestination simulates a destination whose table schema changes between runs.
type mutableMockDestination struct {
	mockDestination
	schemas []*schema.TableSchema
	callIdx int
}

func (m *mutableMockDestination) GetTableSchema(_ context.Context, _ string) (*schema.TableSchema, error) {
	if m.callIdx >= len(m.schemas) {
		return m.schemas[len(m.schemas)-1], nil
	}
	s := m.schemas[m.callIdx]
	m.callIdx++
	return s, nil
}

func simulateRun(t *testing.T, p *Pipeline, sourceSchema *schema.TableSchema) []string {
	t.Helper()
	src := *sourceSchema
	src.Columns = make([]schema.Column, len(sourceSchema.Columns))
	copy(src.Columns, sourceSchema.Columns)
	src.PrimaryKeys = make([]string, len(sourceSchema.PrimaryKeys))
	copy(src.PrimaryKeys, sourceSchema.PrimaryKeys)

	p.columnRenamer = nil

	err := p.setupNamingConvention(context.Background(), &src)
	if err != nil {
		t.Fatalf("setupNamingConvention() error = %v", err)
	}
	return src.ColumnNames()
}

func assertColumns(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("[%s] column count = %d, want %d\n  got:  %v\n  want: %v", label, len(got), len(want), got, want)
		return
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%s] column[%d] = %q, want %q\n  got:  %v\n  want: %v", label, i, got[i], want[i], got, want)
			return
		}
	}
}

func runLabel(i int) string {
	return fmt.Sprintf("run%d", i)
}

func TestApplyExcludedColumnsNamingAware(t *testing.T) {
	source := schema.TableSchema{
		Columns: []schema.Column{
			{Name: "UserId", DataType: schema.TypeInt64, IsPrimaryKey: true},
			{Name: "FullName", DataType: schema.TypeString},
			{Name: "SecretToken", DataType: schema.TypeString},
		},
		PrimaryKeys:    []string{"UserId"},
		IncrementalKey: "FullName",
	}

	tests := []struct {
		name         string
		schemaNaming string
		exclude      []string
		wantColumns  []string
		wantPKs      []string
		wantIncKey   string
	}{
		{
			name:         "exclude by source name",
			schemaNaming: "snake_case",
			exclude:      []string{"SecretToken"},
			wantColumns:  []string{"UserId", "FullName"},
			wantPKs:      []string{"UserId"},
			wantIncKey:   "FullName",
		},
		{
			name:         "exclude by destination snake_case name",
			schemaNaming: "snake_case",
			exclude:      []string{"secret_token"},
			wantColumns:  []string{"UserId", "FullName"},
			wantPKs:      []string{"UserId"},
			wantIncKey:   "FullName",
		},
		{
			name:         "exclude incremental key by destination name",
			schemaNaming: "snake_case",
			exclude:      []string{"full_name"},
			wantColumns:  []string{"UserId", "SecretToken"},
			wantPKs:      []string{"UserId"},
			wantIncKey:   "",
		},
		{
			name:         "exclude primary key by destination name",
			schemaNaming: "snake_case",
			exclude:      []string{"user_id"},
			wantColumns:  []string{"FullName", "SecretToken"},
			wantPKs:      []string{},
			wantIncKey:   "FullName",
		},
		{
			name:         "exclude primary key by source name",
			schemaNaming: "snake_case",
			exclude:      []string{"UserId"},
			wantColumns:  []string{"FullName", "SecretToken"},
			wantPKs:      []string{},
			wantIncKey:   "FullName",
		},
		{
			name:         "direct naming does not match snake_case name",
			schemaNaming: "direct",
			exclude:      []string{"secret_token", "user_id"},
			wantColumns:  []string{"UserId", "FullName", "SecretToken"},
			wantPKs:      []string{"UserId"},
			wantIncKey:   "FullName",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := source
			src.Columns = make([]schema.Column, len(source.Columns))
			copy(src.Columns, source.Columns)
			src.PrimaryKeys = append([]string(nil), source.PrimaryKeys...)

			p := &Pipeline{
				config: &config.IngestConfig{
					DestTable:         "users_out",
					SchemaNaming:      tt.schemaNaming,
					SQLExcludeColumns: tt.exclude,
				},
				dest: &mockDestination{},
			}

			namingConv, err := p.resolveNamingConvention(context.Background(), &src)
			if err != nil {
				t.Fatalf("resolveNamingConvention() error = %v", err)
			}

			got := p.applyExcludedColumnsToSchema(&src, namingConv)
			gotColumns := got.ColumnNames()
			if len(gotColumns) != len(tt.wantColumns) {
				t.Fatalf("columns = %v, want %v", gotColumns, tt.wantColumns)
			}
			for i, want := range tt.wantColumns {
				if gotColumns[i] != want {
					t.Errorf("column[%d] = %q, want %q", i, gotColumns[i], want)
				}
			}
			if len(got.PrimaryKeys) != len(tt.wantPKs) {
				t.Fatalf("primary keys = %v, want %v", got.PrimaryKeys, tt.wantPKs)
			}
			for i, want := range tt.wantPKs {
				if got.PrimaryKeys[i] != want {
					t.Errorf("primary key[%d] = %q, want %q", i, got.PrimaryKeys[i], want)
				}
			}
			if got.IncrementalKey != tt.wantIncKey {
				t.Errorf("incremental key = %q, want %q", got.IncrementalKey, tt.wantIncKey)
			}
		})
	}
}

func TestApplyDestinationSchemaConstraints_OracleStringIncrementalKey(t *testing.T) {
	tableSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64},
			{Name: "cursor", DataType: schema.TypeString},
			{Name: "payload", DataType: schema.TypeString},
		},
		IncrementalKey: "cursor",
	}
	p := &Pipeline{dest: &mockDestination{scheme: "oracle"}}

	p.applyDestinationSchemaConstraints(tableSchema)

	if tableSchema.Columns[1].MaxLength != oracleComparableStringLen {
		t.Fatalf("incremental key MaxLength = %d, want %d", tableSchema.Columns[1].MaxLength, oracleComparableStringLen)
	}
	if tableSchema.Columns[2].MaxLength != 0 {
		t.Fatalf("non-incremental string MaxLength = %d, want 0", tableSchema.Columns[2].MaxLength)
	}
}

func TestEvolveSchemaIfNeededBuildsAbstractPlanForSchemaEvolver(t *testing.T) {
	destSchema := tschema(
		"events",
		tcol("id", schema.TypeInt64),
	)
	sourceSchema := tschema(
		"events",
		tcol("id", schema.TypeInt64),
		tcol("age", schema.TypeInt64),
	)
	p := &Pipeline{
		config: &config.IngestConfig{
			DestTable: "events",
		},
		dest: &mockSchemaEvolutionDestination{
			mockDestination: mockDestination{
				tableSchema: destSchema,
				scheme:      "schema_evolver_without_dialect",
			},
		},
	}

	plan, err := p.evolveSchemaIfNeeded(context.Background(), "events", sourceSchema, config.StrategyAppend)
	if err != nil {
		t.Fatalf("evolveSchemaIfNeeded() error = %v", err)
	}
	if plan == nil || plan.FinalSchema == nil {
		t.Fatal("expected final schema plan")
	}
	if plan.Table != "events" {
		t.Fatalf("plan table = %q, want events", plan.Table)
	}
	if !plan.HasChanges() {
		t.Fatal("expected abstract comparison changes")
	}
	if len(plan.Comparison.Changes) != 1 || plan.Comparison.Changes[0].Type != schemaevolution.ChangeAddColumn {
		t.Fatalf("unexpected plan comparison: %#v", plan.Comparison)
	}

	gotColumns := plan.FinalSchema.ColumnNames()
	assertColumns(t, "columns", gotColumns, []string{"id", "age"})
}

func TestEvolveSchemaIfNeededDoesNotRelaxPrimaryKeyNullability(t *testing.T) {
	destSchema := &schema.TableSchema{Columns: []schema.Column{{
		Name: "ID", DataType: schema.TypeInt64, Nullable: false,
	}}}
	sourceSchema := &schema.TableSchema{
		Columns:     []schema.Column{{Name: "id", DataType: schema.TypeInt64, Nullable: true}},
		PrimaryKeys: []string{"id"},
	}
	p := &Pipeline{
		config: &config.IngestConfig{DestTable: "events"},
		dest: &mockSchemaEvolutionDestination{mockDestination: mockDestination{
			tableSchema: destSchema,
			scheme:      "schema_evolver_without_dialect",
		}},
	}

	plan, err := p.evolveSchemaIfNeeded(context.Background(), "events", sourceSchema, config.StrategyMerge)
	require.NoError(t, err)
	require.NotNil(t, plan)
	require.False(t, plan.HasChanges())
}

func TestEvolveSchemaIfNeededUsesDestinationTypeNormalization(t *testing.T) {
	destSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "active", DataType: schema.TypeInt64}}}
	sourceSchema := &schema.TableSchema{Columns: []schema.Column{{Name: "active", DataType: schema.TypeBoolean}}}
	dest := &normalizingMockSchemaEvolutionDestination{mockSchemaEvolutionDestination: &mockSchemaEvolutionDestination{
		mockDestination: mockDestination{tableSchema: destSchema, scheme: "normalizing"},
	}}
	p := &Pipeline{config: &config.IngestConfig{DestTable: "events"}, dest: dest}

	plan, err := p.evolveSchemaIfNeeded(context.Background(), "events", sourceSchema, config.StrategyAppend)
	require.NoError(t, err)
	require.NotNil(t, plan)
	require.False(t, plan.HasChanges())
}

func TestNamingConsistency(t *testing.T) {
	camelCaseSourceSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "Id", DataType: schema.TypeString},
			{Name: "CreatedDate", DataType: schema.TypeTimestampTZ},
			{Name: "LastModifiedDate", DataType: schema.TypeTimestampTZ},
			{Name: "OpportunityId", DataType: schema.TypeString},
		},
		PrimaryKeys: []string{"Id"},
	}

	snakeCaseSourceSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeString},
			{Name: "created_date", DataType: schema.TypeTimestampTZ},
			{Name: "last_modified_date", DataType: schema.TypeTimestampTZ},
			{Name: "opportunity_id", DataType: schema.TypeString},
		},
		PrimaryKeys: []string{"id"},
	}

	ingestrSnakeCaseDest := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeString},
			{Name: "created_date", DataType: schema.TypeTimestampTZ},
			{Name: "last_modified_date", DataType: schema.TypeTimestampTZ},
			{Name: "opportunity_id", DataType: schema.TypeString},
			{Name: "_dlt_load_id", DataType: schema.TypeString},
			{Name: "_dlt_id", DataType: schema.TypeString},
		},
	}

	snakeCaseDest := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeString},
			{Name: "created_date", DataType: schema.TypeTimestampTZ},
			{Name: "last_modified_date", DataType: schema.TypeTimestampTZ},
			{Name: "opportunity_id", DataType: schema.TypeString},
		},
	}

	camelCaseDest := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "Id", DataType: schema.TypeString},
			{Name: "CreatedDate", DataType: schema.TypeTimestampTZ},
			{Name: "LastModifiedDate", DataType: schema.TypeTimestampTZ},
			{Name: "OpportunityId", DataType: schema.TypeString},
		},
	}

	snakeCaseExpected := []string{"id", "created_date", "last_modified_date", "opportunity_id"}
	camelCaseExpected := []string{"Id", "CreatedDate", "LastModifiedDate", "OpportunityId"}

	// Replace: ingestr table exists, then replaced — should keep snake_case across runs
	t.Run("replace/ingestr then default naming", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{ingestrSnakeCaseDest, snakeCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: ""},
			dest:   dest,
		}
		assertColumns(t, "run1", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
		assertColumns(t, "run2", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
	})

	// Replace: ingestr table exists, auto naming — should detect snake_case both runs
	t.Run("replace/ingestr then auto naming", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{ingestrSnakeCaseDest, snakeCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: "auto"},
			dest:   dest,
		}
		assertColumns(t, "run1", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
		assertColumns(t, "run2", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
	})

	// Replace: explicit direct should ignore ingestr dest and keep original column names
	t.Run("replace/explicit direct ignores ingestr dest", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{ingestrSnakeCaseDest, camelCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: "direct"},
			dest:   dest,
		}
		assertColumns(t, "run1", simulateRun(t, p, camelCaseSourceSchema), camelCaseExpected)
		assertColumns(t, "run2", simulateRun(t, p, camelCaseSourceSchema), camelCaseExpected)
	})

	// Replace: no table exists on first run, default naming uses snake_case
	t.Run("replace/no existing table default naming", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{nil, snakeCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: ""},
			dest:   dest,
		}
		assertColumns(t, "run1", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
		assertColumns(t, "run2", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
	})

	// Replace: no table exists on first run with auto, falls back to snake_case
	t.Run("replace/no existing table auto naming", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{nil, snakeCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: "auto"},
			dest:   dest,
		}
		assertColumns(t, "run1", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
		assertColumns(t, "run2", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
	})

	// Replace: explicit snake_case always converts regardless of dest
	t.Run("replace/explicit snake_case", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{nil, snakeCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: "snake_case"},
			dest:   dest,
		}
		assertColumns(t, "run1", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
		assertColumns(t, "run2", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
	})

	// Replace: 3 consecutive runs after ingestr — naming must stay consistent
	t.Run("replace/three consecutive runs after ingestr", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{ingestrSnakeCaseDest, snakeCaseDest, snakeCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: ""},
			dest:   dest,
		}
		for i := 1; i <= 3; i++ {
			assertColumns(t, runLabel(i), simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
		}
	})

	// Merge: ingestr table, then metadata columns removed — should still detect snake_case
	t.Run("merge/ingestr then default naming", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{ingestrSnakeCaseDest, ingestrSnakeCaseDest, snakeCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: ""},
			dest:   dest,
		}
		assertColumns(t, "run1", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
		assertColumns(t, "run2", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
		assertColumns(t, "run3", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
	})

	// Merge: ingestr table with auto naming — detects snake_case consistently
	t.Run("merge/ingestr then auto naming", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{ingestrSnakeCaseDest, snakeCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: "auto"},
			dest:   dest,
		}
		assertColumns(t, "run1", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
		assertColumns(t, "run2", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
	})

	t.Run("merge/default naming honors destination convention", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{camelCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: ""},
			dest:   dest,
		}
		assertColumns(t, "run1", simulateRun(t, p, camelCaseSourceSchema), camelCaseExpected)
		assertColumns(t, "run2", simulateRun(t, p, camelCaseSourceSchema), camelCaseExpected)
	})

	// Merge: 3 consecutive runs with auto naming after ingestr
	t.Run("merge/three consecutive runs auto naming", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{ingestrSnakeCaseDest, ingestrSnakeCaseDest, snakeCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: "auto"},
			dest:   dest,
		}
		for i := 1; i <= 3; i++ {
			assertColumns(t, runLabel(i), simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
		}
	})

	// Append: ingestr table, then metadata columns removed — should still detect snake_case
	t.Run("append/ingestr then default naming", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{ingestrSnakeCaseDest, ingestrSnakeCaseDest, snakeCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: ""},
			dest:   dest,
		}
		assertColumns(t, "run1", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
		assertColumns(t, "run2", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
		assertColumns(t, "run3", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
	})

	// Append: no table on first run, default naming uses snake_case
	t.Run("append/no existing table default naming", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{nil, snakeCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: ""},
			dest:   dest,
		}
		assertColumns(t, "run1", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
		assertColumns(t, "run2", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
	})

	// Source already snake_case with ingestr dest — no renaming needed
	t.Run("snake_case source/ingestr dest default naming", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{ingestrSnakeCaseDest, snakeCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: ""},
			dest:   dest,
		}
		assertColumns(t, "run1", simulateRun(t, p, snakeCaseSourceSchema), snakeCaseExpected)
		assertColumns(t, "run2", simulateRun(t, p, snakeCaseSourceSchema), snakeCaseExpected)
	})

	// Source already snake_case, no dest, auto naming — stays snake_case
	t.Run("snake_case source/no dest auto naming", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{nil, snakeCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: "auto"},
			dest:   dest,
		}
		assertColumns(t, "run1", simulateRun(t, p, snakeCaseSourceSchema), snakeCaseExpected)
		assertColumns(t, "run2", simulateRun(t, p, snakeCaseSourceSchema), snakeCaseExpected)
	})

	// camelCase dest without ingestr metadata, default naming — converts to snake_case
	t.Run("camelCase dest no ingestr columns/default naming", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{snakeCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: ""},
			dest:   dest,
		}
		assertColumns(t, "run1", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
		assertColumns(t, "run2", simulateRun(t, p, camelCaseSourceSchema), snakeCaseExpected)
	})

	// camelCase dest without ingestr metadata, auto naming — should detect direct
	t.Run("camelCase dest no ingestr columns/auto naming", func(t *testing.T) {
		dest := &mutableMockDestination{
			schemas: []*schema.TableSchema{camelCaseDest, camelCaseDest},
		}
		p := &Pipeline{
			config: &config.IngestConfig{DestTable: "t", SchemaNaming: "auto"},
			dest:   dest,
		}
		assertColumns(t, "run1", simulateRun(t, p, camelCaseSourceSchema), camelCaseExpected)
		assertColumns(t, "run2", simulateRun(t, p, camelCaseSourceSchema), camelCaseExpected)
	})
}

// TestSourceSchemaPreservesOriginalColumnNames verifies that SourceSchema
// (used by strategies to read from the source) retains the original source
// column names even when a naming convention renames columns for the
// destination. The ColumnRenamer handles the rename on Arrow batches after
// reading; the source SELECT query must use the original names.
func TestSourceSchemaPreservesOriginalColumnNames(t *testing.T) {
	sourceSchema := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "Id", DataType: schema.TypeString},
			{Name: "FirstName", DataType: schema.TypeString},
			{Name: "LastName", DataType: schema.TypeString},
		},
		PrimaryKeys: []string{"Id"},
	}

	// Deep copy, same as Run() gets from source.GetTable
	tableSchema := *sourceSchema
	tableSchema.Columns = make([]schema.Column, len(sourceSchema.Columns))
	copy(tableSchema.Columns, sourceSchema.Columns)
	tableSchema.PrimaryKeys = make([]string, len(sourceSchema.PrimaryKeys))
	copy(tableSchema.PrimaryKeys, sourceSchema.PrimaryKeys)

	p := &Pipeline{
		config: &config.IngestConfig{
			DestTable:    "users",
			SchemaNaming: "", // defaults to snake_case
		},
		dest: &mockDestination{tableSchema: nil}, // no existing dest table
	}

	// Snapshot original column names before naming convention renames them.
	// This is what Run() must do to preserve original names for SourceSchema.
	originalSourceSchema := schema.TableSchema{
		Name:           tableSchema.Name,
		Schema:         tableSchema.Schema,
		Columns:        make([]schema.Column, len(tableSchema.Columns)),
		PrimaryKeys:    make([]string, len(tableSchema.PrimaryKeys)),
		IncrementalKey: tableSchema.IncrementalKey,
	}
	copy(originalSourceSchema.Columns, tableSchema.Columns)
	copy(originalSourceSchema.PrimaryKeys, tableSchema.PrimaryKeys)

	// Run() applies naming convention which renames tableSchema columns in-place.
	err := p.setupNamingConvention(context.Background(), &tableSchema)
	if err != nil {
		t.Fatalf("setupNamingConvention() error = %v", err)
	}

	// Verify the destination schema was renamed (sanity check)
	destNames := tableSchema.ColumnNames()
	wantDestNames := []string{"id", "first_name", "last_name"}
	for i, want := range wantDestNames {
		if destNames[i] != want {
			t.Errorf("dest column[%d] = %q, want %q", i, destNames[i], want)
		}
	}

	// SourceSchema must have the ORIGINAL column names because the source
	// table has those names, not the renamed ones. Using renamed names causes:
	//   ERROR: column "first_name" does not exist (SQLSTATE 42703)
	originalNames := []string{"Id", "FirstName", "LastName"}
	sourceSchemaNames := originalSourceSchema.ColumnNames()

	for i, want := range originalNames {
		if sourceSchemaNames[i] != want {
			t.Errorf("SourceSchema column[%d] = %q, want original name %q (source uses these for SELECT queries)",
				i, sourceSchemaNames[i], want)
		}
	}
}

func resolveIncrementality(
	handlesIncrementality bool,
	cfg *config.IngestConfig,
	table *mockSourceTable,
	tableSchema *schema.TableSchema,
) config.IncrementalStrategy {
	// Resolve PKs: user always wins, then table, then schema
	if len(cfg.PrimaryKeys) > 0 {
		tableSchema.PrimaryKeys = cfg.PrimaryKeys
	} else if len(tableSchema.PrimaryKeys) == 0 {
		tableSchema.PrimaryKeys = table.pks
	}

	// Track 1 vs Track 2
	var resolvedStrategy config.IncrementalStrategy
	if handlesIncrementality {
		tableSchema.IncrementalKey = table.incrementalKey
		resolvedStrategy = table.strategy
	} else {
		if cfg.IncrementalKey != "" {
			tableSchema.IncrementalKey = cfg.IncrementalKey
		} else if tableSchema.IncrementalKey == "" {
			tableSchema.IncrementalKey = table.incrementalKey
		}
		resolvedStrategy = cfg.IncrementalStrategy
	}

	if cfg.FullRefresh {
		resolvedStrategy = config.StrategyReplace
	}

	return resolvedStrategy
}

type mockSourceTable struct {
	pks            []string
	incrementalKey string
	strategy       config.IncrementalStrategy
}

func TestResolveIncrementality(t *testing.T) {
	tests := []struct {
		name                  string
		handlesIncrementality bool
		cfg                   *config.IngestConfig
		table                 *mockSourceTable
		schemaIncrementalKey  string
		schemaPKs             []string
		wantStrategy          config.IncrementalStrategy
		wantPKs               []string
		wantIncrementalKey    string
	}{
		// === Track 1: source handles incrementality ===
		{
			name:                  "track1: source strategy and incremental key win",
			handlesIncrementality: true,
			cfg: &config.IngestConfig{
				IncrementalStrategy: config.StrategyReplace,
				IncrementalKey:      "user_key",
			},
			table: &mockSourceTable{
				pks:            []string{"source_pk"},
				incrementalKey: "updated_at",
				strategy:       config.StrategyMerge,
			},
			wantStrategy:       config.StrategyMerge,
			wantPKs:            []string{"source_pk"},
			wantIncrementalKey: "updated_at",
		},
		{
			name:                  "track1: user PKs override source PKs",
			handlesIncrementality: true,
			cfg: &config.IngestConfig{
				IncrementalStrategy: config.StrategyReplace,
				PrimaryKeys:         []string{"user_pk"},
			},
			table: &mockSourceTable{
				pks:            []string{"source_pk"},
				incrementalKey: "updated_at",
				strategy:       config.StrategyMerge,
			},
			wantStrategy:       config.StrategyMerge,
			wantPKs:            []string{"user_pk"},
			wantIncrementalKey: "updated_at",
		},
		{
			name:                  "track1: full refresh overrides source strategy",
			handlesIncrementality: true,
			cfg: &config.IngestConfig{
				IncrementalStrategy: config.StrategyReplace,
				FullRefresh:         true,
			},
			table: &mockSourceTable{
				pks:            []string{"id"},
				incrementalKey: "updated_at",
				strategy:       config.StrategyMerge,
			},
			wantStrategy:       config.StrategyReplace,
			wantPKs:            []string{"id"},
			wantIncrementalKey: "updated_at",
		},

		// === Track 2: framework handles incrementality ===
		{
			name:                  "track2: user strategy wins over table",
			handlesIncrementality: false,
			cfg: &config.IngestConfig{
				IncrementalStrategy: config.StrategyMerge,
				IncrementalKey:      "created_at",
				PrimaryKeys:         []string{"user_pk"},
			},
			table: &mockSourceTable{
				pks:            []string{"table_pk"},
				incrementalKey: "table_key",
				strategy:       config.StrategyAppend,
			},
			wantStrategy:       config.StrategyMerge,
			wantPKs:            []string{"user_pk"},
			wantIncrementalKey: "created_at",
		},
		{
			name:                  "track2: falls back to table PKs when user provides none",
			handlesIncrementality: false,
			cfg: &config.IngestConfig{
				IncrementalStrategy: config.StrategyReplace,
			},
			table: &mockSourceTable{
				pks:      []string{"auto_pk"},
				strategy: config.StrategyReplace,
			},
			wantStrategy:       config.StrategyReplace,
			wantPKs:            []string{"auto_pk"},
			wantIncrementalKey: "",
		},
		{
			name:                  "track2: falls back to table incremental key when user provides none",
			handlesIncrementality: false,
			cfg: &config.IngestConfig{
				IncrementalStrategy: config.StrategyDeleteInsert,
			},
			table: &mockSourceTable{
				pks:            []string{"id"},
				incrementalKey: "modified_at",
				strategy:       config.StrategyReplace,
			},
			wantStrategy:       config.StrategyDeleteInsert,
			wantPKs:            []string{"id"},
			wantIncrementalKey: "modified_at",
		},
		{
			name:                  "track2: schema PKs used when table has none",
			handlesIncrementality: false,
			cfg: &config.IngestConfig{
				IncrementalStrategy: config.StrategyReplace,
			},
			table: &mockSourceTable{
				strategy: config.StrategyReplace,
			},
			schemaPKs:          []string{"schema_pk"},
			wantStrategy:       config.StrategyReplace,
			wantPKs:            []string{"schema_pk"},
			wantIncrementalKey: "",
		},
		{
			name:                  "track2: user PKs override schema PKs",
			handlesIncrementality: false,
			cfg: &config.IngestConfig{
				IncrementalStrategy: config.StrategyMerge,
				PrimaryKeys:         []string{"user_pk"},
			},
			table: &mockSourceTable{
				pks:      []string{"table_pk"},
				strategy: config.StrategyReplace,
			},
			schemaPKs:          []string{"schema_pk"},
			wantStrategy:       config.StrategyMerge,
			wantPKs:            []string{"user_pk"},
			wantIncrementalKey: "",
		},
		{
			name:                  "track2: schema incremental key used when table has none",
			handlesIncrementality: false,
			cfg: &config.IngestConfig{
				IncrementalStrategy: config.StrategyReplace,
			},
			table: &mockSourceTable{
				strategy: config.StrategyReplace,
			},
			schemaIncrementalKey: "schema_inc_key",
			wantStrategy:         config.StrategyReplace,
			wantPKs:              nil,
			wantIncrementalKey:   "schema_inc_key",
		},
		{
			name:                  "track2: full refresh overrides user strategy",
			handlesIncrementality: false,
			cfg: &config.IngestConfig{
				IncrementalStrategy: config.StrategyMerge,
				PrimaryKeys:         []string{"id"},
				FullRefresh:         true,
			},
			table: &mockSourceTable{
				pks:      []string{"table_pk"},
				strategy: config.StrategyAppend,
			},
			wantStrategy:       config.StrategyReplace,
			wantPKs:            []string{"id"},
			wantIncrementalKey: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tableSchema := &schema.TableSchema{
				PrimaryKeys:    tt.schemaPKs,
				IncrementalKey: tt.schemaIncrementalKey,
			}

			gotStrategy := resolveIncrementality(tt.handlesIncrementality, tt.cfg, tt.table, tableSchema)

			if gotStrategy != tt.wantStrategy {
				t.Errorf("strategy = %q, want %q", gotStrategy, tt.wantStrategy)
			}

			if len(tableSchema.PrimaryKeys) != len(tt.wantPKs) {
				t.Errorf("PKs = %v, want %v", tableSchema.PrimaryKeys, tt.wantPKs)
			} else {
				for i, want := range tt.wantPKs {
					if tableSchema.PrimaryKeys[i] != want {
						t.Errorf("PK[%d] = %q, want %q", i, tableSchema.PrimaryKeys[i], want)
					}
				}
			}

			if tableSchema.IncrementalKey != tt.wantIncrementalKey {
				t.Errorf("incrementalKey = %q, want %q", tableSchema.IncrementalKey, tt.wantIncrementalKey)
			}
		})
	}
}

func TestCDCSlotSuffix(t *testing.T) {
	// Deterministic: same input always produces same output
	s1 := cdcSlotSuffix("sqlite:///tmp/a.db")
	s2 := cdcSlotSuffix("sqlite:///tmp/a.db")
	if s1 != s2 {
		t.Errorf("cdcSlotSuffix not deterministic: %q != %q", s1, s2)
	}

	// 20 hex characters
	if len(s1) != 20 {
		t.Errorf("cdcSlotSuffix length = %d, want 20", len(s1))
	}

	// Different URIs produce different suffixes
	s3 := cdcSlotSuffix("sqlite:///tmp/b.db")
	if s1 == s3 {
		t.Errorf("cdcSlotSuffix(%q) == cdcSlotSuffix(%q), want different", "sqlite:///tmp/a.db", "sqlite:///tmp/b.db")
	}
	if cdcSlotSuffix("postgres://warehouse/db?application_name=cdc3463") == cdcSlotSuffix("postgres://warehouse/db?application_name=cdc9341") {
		t.Fatal("80-bit slot suffix retained a known 24-bit collision")
	}
	legacy := legacyCDCSlotSuffix("sqlite:///tmp/a.db")
	if len(legacy) != 6 {
		t.Fatalf("legacy slot suffix length = %d, want 6", len(legacy))
	}
	wantLegacy := sha256.Sum256([]byte("sqlite:///tmp/a.db"))
	if legacy != fmt.Sprintf("%x", wantLegacy[:3]) {
		t.Fatalf("legacy slot suffix = %q, want raw-destination 24-bit hash", legacy)
	}
}

func TestCDCStateConnectorID(t *testing.T) {
	base := &config.IngestConfig{
		SourceURI:   "postgres+cdc://user:old@db.example/app?publication=pub&mode=batch",
		DestURI:     "postgres://loader:old@warehouse.example/analytics",
		SourceTable: "public.orders",
		DestTable:   "raw.orders",
	}
	resolved := source.ConnectorIdentity{
		Database:  "postgres\x00db.example:5432\x00app",
		Connector: "postgres\x00db.example:5432\x00app\x00pub\x00",
	}

	rotated := *base
	rotated.SourceURI = "postgres+cdc://user:new@db.example/app?mode=stream&publication=pub&binary=true"
	rotated.DestURI = "postgres://loader:new@warehouse.example/analytics"
	if got, want := resolvedCDCStateConnectorID(&rotated, resolved, ""), resolvedCDCStateConnectorID(base, resolved, ""); got != want {
		t.Fatalf("credential/runtime option changes changed connector ID: got %s want %s", got, want)
	}

	aliased := *base
	aliased.SourceURI = "postgresql+cdc://other@DB.EXAMPLE:5432/app?publication=pub&mode=stream"
	aliased.DestURI = "postgresql://different@WAREHOUSE.EXAMPLE:5432/analytics"
	if got, want := resolvedCDCStateConnectorID(&aliased, resolved, ""), resolvedCDCStateConnectorID(base, resolved, ""); got != want {
		t.Fatalf("scheme/default-port aliases changed connector ID: got %s want %s", got, want)
	}

	otherSlotIdentity := resolved
	otherSlotIdentity.Connector += "another_slot"
	if resolvedCDCStateConnectorID(base, otherSlotIdentity, "") == resolvedCDCStateConnectorID(base, resolved, "") {
		t.Fatal("different replication slots must not share CDC state")
	}

	explicitA := *base
	explicitA.SourceURI += "&state_id=connector-a"
	explicitRotated := explicitA
	explicitRotated.SourceURI = "postgres+cdc://other:new@db.example/app?state_id=connector-a&mode=stream&binary=true&discover_interval=10s"
	if got, want := resolvedCDCStateConnectorID(&explicitRotated, resolved, ""), resolvedCDCStateConnectorID(&explicitA, resolved, ""); got != want {
		t.Fatalf("explicit state identity changed within one source database: got %s want %s", got, want)
	}
	explicitPort := explicitA
	explicitPort.SourceURI = "postgresql+cdc://other@DB.EXAMPLE:5432/app?state_id=connector-a"
	if got, want := resolvedCDCStateConnectorID(&explicitPort, resolved, ""), resolvedCDCStateConnectorID(&explicitA, resolved, ""); got != want {
		t.Fatalf("explicit state identity changed for default port alias: got %s want %s", got, want)
	}

	otherDatabaseIdentity := resolved
	otherDatabaseIdentity.Database = "postgres\x00db.example:5432\x00other_db"
	otherDatabaseIdentity.Connector = otherDatabaseIdentity.Database + "\x00pub\x00"
	if resolvedCDCStateConnectorID(&explicitA, otherDatabaseIdentity, "") == resolvedCDCStateConnectorID(&explicitA, resolved, "") {
		t.Fatal("matching explicit state_id values on different source databases must not share identity")
	}
	explicitB := explicitA
	explicitB.SourceURI = strings.Replace(explicitA.SourceURI, "connector-a", "connector-b", 1)
	suffixA := cdcSlotSuffix(canonicalCDCStateURI(explicitA.DestURI) + "\x00" + resolvedCDCStateConnectorID(&explicitA, resolved, ""))
	suffixB := cdcSlotSuffix(canonicalCDCStateURI(explicitB.DestURI) + "\x00" + resolvedCDCStateConnectorID(&explicitB, resolved, ""))
	if suffixA == suffixB {
		t.Fatal("distinct explicit state identities share an automatic slot suffix")
	}
	rotatedSuffix := cdcSlotSuffix(canonicalCDCStateURI(rotated.DestURI) + "\x00" + resolvedCDCStateConnectorID(&rotated, resolved, ""))
	baseSuffix := cdcSlotSuffix(canonicalCDCStateURI(base.DestURI) + "\x00" + resolvedCDCStateConnectorID(base, resolved, ""))
	if rotatedSuffix != baseSuffix {
		t.Fatal("credential rotation changed the automatic slot suffix")
	}

	implicitDestAlice := *base
	implicitDestAlice.DestURI = "postgres://alice@warehouse.example"
	implicitDestBob := implicitDestAlice
	implicitDestBob.DestURI = "postgres://bob@warehouse.example"
	if resolvedCDCStateConnectorID(&implicitDestAlice, resolved, "") == resolvedCDCStateConnectorID(&implicitDestBob, resolved, "") {
		t.Fatal("destination URIs with different implicit PostgreSQL databases share a connector ID")
	}
}

func TestCDCStateConnectorIDDistinguishesEffectiveUnqualifiedDestination(t *testing.T) {
	cfg := &config.IngestConfig{
		SourceURI:   "postgres+cdc://reader@source/app",
		DestURI:     "postgres://loader@warehouse/analytics",
		SourceTable: "public.orders",
		DestTable:   "orders",
	}
	identity := source.ConnectorIdentity{Database: "source-db", Connector: "source-connector"}
	first := resolvedCDCStateConnectorID(cfg, identity, "analytics\x00alice\x00orders")
	second := resolvedCDCStateConnectorID(cfg, identity, "analytics\x00bob\x00orders")
	if first == second {
		t.Fatal("different effective destination schemas share a connector ID")
	}
	if first == resolvedCDCStateConnectorID(cfg, identity, "") {
		t.Fatal("effective destination namespace was not added to the connector ID")
	}

	dest := &canonicalIdentityDestination{identity: "canonical-target"}
	got, err := managedCDCDestinationIdentity(t.Context(), dest, cfg.DestTable)
	require.NoError(t, err)
	require.Equal(t, "canonical-target", got)
	require.Equal(t, 1, dest.calls)

	cfg.DestTable = "raw.orders"
	got, err = managedCDCDestinationIdentity(t.Context(), dest, cfg.DestTable)
	require.NoError(t, err)
	require.Equal(t, "canonical-target", got)
	require.Equal(t, 2, dest.calls)

	qualifiedA := *cfg
	qualifiedA.DestTable = "raw.orders"
	qualifiedB := *cfg
	qualifiedB.DestTable = `"raw"."orders"`
	first = resolvedCDCStateConnectorID(&qualifiedA, identity, "canonical-target")
	second = resolvedCDCStateConnectorID(&qualifiedB, identity, "canonical-target")
	require.Equal(t, first, second, "equivalent qualified targets must share a connector ID")
}

func TestMultiTableCDCConnectorIDIncludesNormalizedDestinationNamespace(t *testing.T) {
	base := &config.IngestConfig{
		SourceURI: "postgres+cdc://reader@source/app?publication=pub&dest_schema=Landing_A",
		DestURI:   "postgres://loader@warehouse/analytics",
	}
	identity := source.ConnectorIdentity{Database: "source-db", Connector: "source-connector"}
	dest := &canonicalIdentityDestination{}

	targetA := managedCDCDestinationTarget(base, dest)
	canonicalA, err := managedCDCDestinationIdentity(t.Context(), dest, targetA)
	require.NoError(t, err)
	idA := resolvedCDCStateConnectorID(base, identity, canonicalA)
	require.Equal(t, "Landing_A.__ingestr_cdc_namespace__", targetA)
	require.Equal(t, "landing_a.__ingestr_cdc_namespace__", canonicalA)

	equivalent := *base
	equivalent.SourceURI = strings.Replace(base.SourceURI, "Landing_A", "landing_a", 1)
	targetEquivalent := managedCDCDestinationTarget(&equivalent, dest)
	canonicalEquivalent, err := managedCDCDestinationIdentity(t.Context(), dest, targetEquivalent)
	require.NoError(t, err)
	require.Equal(t, idA, resolvedCDCStateConnectorID(&equivalent, identity, canonicalEquivalent),
		"equivalent destination namespaces must share connector state and slot identity")

	other := *base
	other.SourceURI = strings.Replace(base.SourceURI, "Landing_A", "landing_b", 1)
	targetB := managedCDCDestinationTarget(&other, dest)
	canonicalB, err := managedCDCDestinationIdentity(t.Context(), dest, targetB)
	require.NoError(t, err)
	idB := resolvedCDCStateConnectorID(&other, identity, canonicalB)
	require.NotEqual(t, idA, idB, "different dest_schema mappings must not share connector state or slots")
	require.NotEqual(t,
		cdcSlotSuffix(canonicalCDCStateURI(base.DestURI)+"\x00"+idA),
		cdcSlotSuffix(canonicalCDCStateURI(other.DestURI)+"\x00"+idB),
		"different dest_schema mappings must not share automatic PostgreSQL slots")
}

func TestMySQLCDCConnectorIDIgnoresOperationalXALimits(t *testing.T) {
	base := &config.IngestConfig{
		SourceURI:   "mysql+cdc://reader@source/app?server_id=11&xa_buffer_limit=100&xa_buffer_bytes_limit=10000&xa_pending_limit=2",
		SourceTable: "orders",
		DestURI:     "mysql://loader@warehouse/analytics",
		DestTable:   "orders",
	}
	changed := *base
	changed.SourceURI = "mysql+cdc://reader@source/app?server_id=22&xa_buffer_limit=1000&xa_buffer_bytes_limit=20000&xa_pending_limit=20"

	require.Equal(t, genericCDCConnectorID(base), genericCDCConnectorID(&changed))
}

func TestDroppedColumnsPKFiltering(t *testing.T) {
	tests := []struct {
		name           string
		primaryKeys    []string
		droppedColumns map[string]bool
		wantPKs        []string
	}{
		{
			name:           "no dropped columns",
			primaryKeys:    []string{"id", "name"},
			droppedColumns: nil,
			wantPKs:        []string{"id", "name"},
		},
		{
			name:           "PK references dropped column",
			primaryKeys:    []string{"campaign_id", "adsquad_id", "start_time"},
			droppedColumns: map[string]bool{"adsquad_id": true},
			wantPKs:        []string{"campaign_id", "start_time"},
		},
		{
			name:           "all PKs dropped",
			primaryKeys:    []string{"a", "b"},
			droppedColumns: map[string]bool{"a": true, "b": true},
			wantPKs:        nil,
		},
		{
			name:           "no PKs defined",
			primaryKeys:    nil,
			droppedColumns: map[string]bool{"a": true},
			wantPKs:        nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Pipeline{
				droppedColumns: tt.droppedColumns,
			}

			pks := p.filterDroppedPKs(tt.primaryKeys)

			if len(pks) != len(tt.wantPKs) {
				t.Fatalf("PKs = %v, want %v", pks, tt.wantPKs)
			}
			for i, want := range tt.wantPKs {
				if pks[i] != want {
					t.Errorf("PK[%d] = %q, want %q", i, pks[i], want)
				}
			}
		})
	}
}

func tcol(name string, dt schema.DataType) schema.Column {
	return schema.Column{Name: name, DataType: dt, Nullable: true}
}

func tschema(name string, cols ...schema.Column) *schema.TableSchema {
	return &schema.TableSchema{Name: name, Columns: cols}
}

func arrowFieldNames(s *arrow.Schema) []string {
	out := make([]string, s.NumFields())
	for i, f := range s.Fields() {
		out[i] = f.Name
	}
	return out
}

// Case 1: identical source and dest schemas, target equals dest's order,
// types come from dest.
func TestBuildBufferReaderTarget_NoDriftIdenticalOrder(t *testing.T) {
	p := &Pipeline{}
	src := tschema(
		"users",
		tcol("id", schema.TypeInt64),
		tcol("name", schema.TypeString),
		tcol("age", schema.TypeInt32),
	)
	dest := tschema(
		"users",
		tcol("id", schema.TypeInt64),
		tcol("name", schema.TypeString),
		tcol("age", schema.TypeInt32),
	)

	got := p.buildBufferReaderTarget(src, dest)

	assertColumns(t, "fields", arrowFieldNames(got), []string{"id", "name", "age"})
	if got.Field(0).Type.ID() != arrow.PrimitiveTypes.Int64.ID() {
		t.Errorf("field 0 type = %s, want int64", got.Field(0).Type)
	}
	if got.Field(1).Type.ID() != arrow.BinaryTypes.String.ID() {
		t.Errorf("field 1 type = %s, want string", got.Field(1).Type)
	}
	if got.Field(2).Type.ID() != arrow.PrimitiveTypes.Int32.ID() {
		t.Errorf("field 2 type = %s, want int32", got.Field(2).Type)
	}
}

// source-only columns reach destSchema via the evolve phase (ChangeAddColumn);
// when destSchema doesn't carry them, this function drops them.
func TestBuildBufferReaderTarget_SourceOnlyColumnIsDropped(t *testing.T) {
	p := &Pipeline{}
	src := tschema(
		"users",
		tcol("id", schema.TypeInt64),
		tcol("name", schema.TypeString),
		tcol("email", schema.TypeString),
	)
	dest := tschema(
		"users",
		tcol("id", schema.TypeInt64),
		tcol("name", schema.TypeString),
	)

	got := p.buildBufferReaderTarget(src, dest)

	assertColumns(t, "fields", arrowFieldNames(got), []string{"id", "name"})
}

func TestRemoveSCD2MetadataColumns_CaseInsensitive(t *testing.T) {
	got := removeSCD2MetadataColumns(tschema(
		"users",
		tcol("id", schema.TypeInt64),
		tcol("_SCD_VALID_FROM", schema.TypeTimestampTZ),
		tcol("_scd_valid_to", schema.TypeTimestampTZ),
		tcol("_Scd_Is_Current", schema.TypeBoolean),
		tcol("name", schema.TypeString),
	))

	assertColumns(t, "columns", got.ColumnNames(), []string{"id", "name"})
}

func TestAddLoadTimestampColumnCreatesNullableColumn(t *testing.T) {
	got := addLoadTimestampColumn(tschema("users", tcol("id", schema.TypeInt64)))

	if len(got.Columns) != 2 {
		t.Fatalf("len(columns) = %d, want 2", len(got.Columns))
	}
	col := got.Columns[1]
	if col.Name != "_ingestr_loaded_at" {
		t.Fatalf("load timestamp column name = %q", col.Name)
	}
	if col.DataType != schema.TypeTimestampTZ {
		t.Fatalf("load timestamp type = %v, want %v", col.DataType, schema.TypeTimestampTZ)
	}
	if !col.Nullable {
		t.Fatal("load timestamp column should be nullable")
	}
}

func TestAddLoadTimestampColumnKeepsExistingNameButMakesNullable(t *testing.T) {
	existing := schema.Column{
		Name:     "_INGESTR_LOADED_AT",
		DataType: schema.TypeString,
		Nullable: false,
	}
	got := addLoadTimestampColumn(tschema("users", tcol("id", schema.TypeInt64), existing))

	col := got.Columns[1]
	if col.Name != existing.Name {
		t.Fatalf("load timestamp column name = %q, want %q", col.Name, existing.Name)
	}
	if col.DataType != schema.TypeTimestampTZ {
		t.Fatalf("load timestamp type = %v, want %v", col.DataType, schema.TypeTimestampTZ)
	}
	if !col.Nullable {
		t.Fatal("existing load timestamp column should be treated as nullable")
	}
}

func TestPreserveSourceCDCColumnTypes(t *testing.T) {
	ingest := tschema(
		"items",
		tcol("id", schema.TypeInt64),
		tcol("_cdc_deleted", schema.TypeInt64),
		tcol("_cdc_lsn", schema.TypeString),
		tcol("_cdc_unchanged_cols", schema.TypeString),
		tcol("value", schema.TypeString),
	)
	source := tschema(
		"items",
		tcol("id", schema.TypeInt64),
		tcol("_cdc_deleted", schema.TypeBoolean),
		tcol("_cdc_lsn", schema.TypeString),
		tcol("_cdc_unchanged_cols", schema.TypeString),
		tcol("value", schema.TypeInt64),
	)

	got := destination.PreserveSourceCDCColumnTypes(ingest, source)

	types := map[string]schema.DataType{}
	for _, col := range got.Columns {
		types[col.Name] = col.DataType
	}
	if types["_cdc_deleted"] != schema.TypeBoolean {
		t.Fatalf("_cdc_deleted type = %v, want %v", types["_cdc_deleted"], schema.TypeBoolean)
	}
	if types["value"] != schema.TypeString {
		t.Fatalf("value type = %v, want destination-aligned string", types["value"])
	}
}

func TestPreserveLogicalPrimaryKeys(t *testing.T) {
	ingest := tschema(
		"items",
		tcol("id", schema.TypeString),
		tcol("value", schema.TypeString),
	)
	source := tschema(
		"items",
		tcol("id", schema.TypeString),
		tcol("value", schema.TypeString),
	)
	source.PrimaryKeys = []string{"id"}

	got := preserveLogicalPrimaryKeys(ingest, source)

	if len(got.PrimaryKeys) != 1 || got.PrimaryKeys[0] != "id" {
		t.Fatalf("primary keys = %v, want [id]", got.PrimaryKeys)
	}
	if len(ingest.PrimaryKeys) != 0 {
		t.Fatalf("input schema was mutated: %v", ingest.PrimaryKeys)
	}
}

func TestMarkPrimaryKeyColumns(t *testing.T) {
	tableSchema := tschema(
		"items",
		tcol("id", schema.TypeString),
		tcol("value", schema.TypeString),
	)
	tableSchema.PrimaryKeys = []string{"id"}

	markPrimaryKeyColumns(tableSchema)

	if !tableSchema.Columns[0].IsPrimaryKey {
		t.Fatal("id should be marked as primary key")
	}
	if tableSchema.Columns[1].IsPrimaryKey {
		t.Fatal("non-key column should not be marked as primary key")
	}
}

func TestStrategyUsesLogicalPrimaryKeys(t *testing.T) {
	if strategyUsesLogicalPrimaryKeys(config.StrategyAppend) {
		t.Fatal("append should not preserve logical primary keys for destination prepare")
	}
	for _, strategy := range []config.IncrementalStrategy{
		config.StrategyReplace,
		config.StrategyMerge,
		config.StrategyDeleteInsert,
		config.StrategySCD2,
	} {
		if !strategyUsesLogicalPrimaryKeys(strategy) {
			t.Fatalf("%s should preserve logical primary keys", strategy)
		}
	}
}

func TestBuildBufferReaderTarget_OrderFollowsDest(t *testing.T) {
	p := &Pipeline{}
	src := tschema(
		"users",
		tcol("email", schema.TypeString),
		tcol("age", schema.TypeInt32),
		tcol("id", schema.TypeInt64),
		tcol("name", schema.TypeString),
	)
	dest := tschema(
		"users",
		tcol("id", schema.TypeInt64),
		tcol("name", schema.TypeString),
		tcol("age", schema.TypeInt32),
	)

	got := p.buildBufferReaderTarget(src, dest)

	assertColumns(t, "fields", arrowFieldNames(got), []string{"id", "name", "age"})
}

// Case 4a: dest has an ingestr metadata column NOT in source. It must be
// SKIPPED in the target. IngestrColumnFiller adds it downstream; including
// it here would cause a duplicate.
func TestBuildBufferReaderTarget_SkipsIngestrMetadataColumn(t *testing.T) {
	p := &Pipeline{}
	src := tschema(
		"users",
		tcol("id", schema.TypeInt64),
		tcol("name", schema.TypeString),
	)
	dest := tschema(
		"users",
		tcol("id", schema.TypeInt64),
		tcol("name", schema.TypeString),
		tcol("_dlt_load_id", schema.TypeString),
	)

	got := p.buildBufferReaderTarget(src, dest)

	assertColumns(t, "fields", arrowFieldNames(got), []string{"id", "name"})
}

func TestBuildBufferReaderTarget_SkipsLoadTimestampColumn(t *testing.T) {
	p := &Pipeline{}
	src := tschema(
		"users",
		tcol("id", schema.TypeInt64),
		tcol("name", schema.TypeString),
	)
	dest := tschema(
		"users",
		tcol("id", schema.TypeInt64),
		tcol("name", schema.TypeString),
		tcol("_ingestr_loaded_at", schema.TypeTimestampTZ),
	)

	got := p.buildBufferReaderTarget(src, dest)

	assertColumns(t, "fields", arrowFieldNames(got), []string{"id", "name"})
}

func TestSetupIngestrColumnsDoesNotFillLoadTimestamp(t *testing.T) {
	p := &Pipeline{
		config: &config.IngestConfig{DestTable: "users"},
		dest: &mockDestination{tableSchema: tschema(
			"users",
			tcol("id", schema.TypeInt64),
			tcol("_ingestr_loaded_at", schema.TypeTimestampTZ),
		)},
	}
	src := tschema("users", tcol("id", schema.TypeInt64))

	got, err := p.setupIngestrColumns(context.Background(), src)
	if err != nil {
		t.Fatalf("setupIngestrColumns() error = %v", err)
	}
	if got != nil {
		t.Fatalf("setupIngestrColumns() returned schema with columns %v, want nil", got.ColumnNames())
	}
	if p.ingestrColumnFiller != nil {
		t.Fatal("load timestamp column must not use IngestrColumnFiller")
	}
}

// Case 4b: dest has a NON-ingestr column NOT in source (soft-removed under
// evolve mode). It MUST be included in the target so the buffer reader
// null-fills it; staging then gets the column with NULLs, MERGE inserts NULL
// into the dest column for new rows.
func TestBuildBufferReaderTarget_IncludesSoftRemovedColumn(t *testing.T) {
	p := &Pipeline{}
	src := tschema(
		"users",
		tcol("id", schema.TypeInt64),
		tcol("name", schema.TypeString),
	)
	dest := tschema(
		"users",
		tcol("id", schema.TypeInt64),
		tcol("name", schema.TypeString),
		tcol("email", schema.TypeString), // soft-removed from source
	)

	got := p.buildBufferReaderTarget(src, dest)

	assertColumns(t, "fields", arrowFieldNames(got), []string{"id", "name", "email"})
	if !got.Field(2).Nullable {
		t.Errorf("soft-removed column must be nullable")
	}
}

// Case 5: dest type differs from source type. Target uses dest's type so the
// buffer reader casts batches to the staging-table column type.
func TestBuildBufferReaderTarget_UsesDestTypes(t *testing.T) {
	p := &Pipeline{}
	src := tschema(
		"events",
		tcol("id", schema.TypeInt64),
		tcol("created_at", schema.TypeTimestamp), // source: TIMESTAMP
	)
	dest := tschema(
		"events",
		tcol("id", schema.TypeInt64),
		tcol("created_at", schema.TypeString), // dest: STRING
	)

	got := p.buildBufferReaderTarget(src, dest)

	if got.Field(1).Name != "created_at" {
		t.Errorf("field 1 name = %q, want created_at", got.Field(1).Name)
	}
	if got.Field(1).Type.ID() != arrow.BinaryTypes.String.ID() {
		t.Errorf("field 1 type = %s, want string", got.Field(1).Type)
	}
}

// Case 6: a ColumnRenamer bridges source camelCase names to dest snake_case.
// Field names in the target stay as SOURCE names (to match buffer files), but
// type lookup goes through the rename map to find the dest column.
func TestBuildBufferReaderTarget_HonorsRenamer(t *testing.T) {
	p := &Pipeline{
		columnRenamer: transformer.NewColumnRenamer(map[string]string{
			"userId":    "user_id",
			"createdAt": "created_at",
		}),
	}
	src := tschema(
		"users",
		tcol("userId", schema.TypeInt64),
		tcol("createdAt", schema.TypeTimestamp),
	)
	dest := tschema(
		"users",
		tcol("user_id", schema.TypeInt64),
		tcol("created_at", schema.TypeString), // wider dest type
	)

	got := p.buildBufferReaderTarget(src, dest)

	assertColumns(t, "fields", arrowFieldNames(got), []string{"userId", "createdAt"})
	if got.Field(1).Type.ID() != arrow.BinaryTypes.String.ID() {
		t.Errorf("field 1 type = %s, want string", got.Field(1).Type)
	}
}

func TestBuildBufferReaderTarget_KeepsAliasesForCanonicalDuplicate(t *testing.T) {
	p := &Pipeline{
		columnRenamer: transformer.NewColumnRenamer(map[string]string{
			"userId": "user_id",
			"UserID": "user_id",
		}),
	}
	src := tschema(
		"users",
		tcol("_id", schema.TypeInt64),
		tcol("userId", schema.TypeInt64),
		tcol("user_id", schema.TypeInt64),
		tcol("UserID", schema.TypeInt64),
	)
	dest := tschema(
		"users",
		tcol("_id", schema.TypeInt64),
		tcol("user_id", schema.TypeInt64),
	)

	got := p.buildBufferReaderTarget(src, dest)

	assertColumns(t, "fields", arrowFieldNames(got), []string{"_id", "userId", "user_id", "UserID"})
}

func TestBuildSourceSchemaCaster_ProjectsAndCastsToSourceSchema(t *testing.T) {
	p := &Pipeline{}
	sourceSchema := tschema(
		"events",
		tcol("id", schema.TypeInt64),
		tcol("count", schema.TypeInt64),
	)

	caster := p.buildSourceSchemaCaster(sourceSchema)
	if caster == nil {
		t.Fatal("expected source schema caster")
	}

	mem := memory.NewGoAllocator()
	idBuilder := array.NewInt64Builder(mem)
	idBuilder.Append(7)
	idArr := idBuilder.NewArray()
	idBuilder.Release()
	defer idArr.Release()

	extraBuilder := array.NewStringBuilder(mem)
	extraBuilder.Append("drop me")
	extraArr := extraBuilder.NewArray()
	extraBuilder.Release()
	defer extraArr.Release()

	countBuilder := array.NewStringBuilder(mem)
	countBuilder.Append("42")
	countArr := countBuilder.NewArray()
	countBuilder.Release()
	defer countArr.Release()

	input := array.NewRecordBatch(
		arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
			{Name: "extra", Type: arrow.BinaryTypes.String, Nullable: true},
			{Name: "count", Type: arrow.BinaryTypes.String, Nullable: true},
		}, nil),
		[]arrow.Array{idArr, extraArr, countArr},
		1,
	)
	defer input.Release()

	got, err := caster.Transform(input)
	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}
	defer got.Release()

	assertColumns(t, "fields", arrowFieldNames(got.Schema()), []string{"id", "count"})
	if got.Column(1).DataType().ID() != arrow.INT64 {
		t.Fatalf("count type = %s, want int64", got.Column(1).DataType())
	}
	countCol, ok := got.Column(1).(*array.Int64)
	if !ok {
		t.Fatalf("count column type = %T, want *array.Int64", got.Column(1))
	}
	if got := countCol.Value(0); got != 42 {
		t.Fatalf("count = %d, want 42", got)
	}
}

func TestApplyColumnMapping_DedupesCanonicalColumns(t *testing.T) {
	p := &Pipeline{}
	src := tschema(
		"users",
		tcol("_id", schema.TypeInt64),
		tcol("userId", schema.TypeInt32),
		tcol("user_id", schema.TypeInt64),
		tcol("UserID", schema.TypeInt64),
	)
	src.PrimaryKeys = []string{"userId", "UserID"}

	p.applyColumnMapping(src, map[string]string{
		"userId": "user_id",
		"UserID": "user_id",
	})

	assertColumns(t, "columns", src.ColumnNames(), []string{"_id", "user_id"})
	assertColumns(t, "primary keys", src.PrimaryKeys, []string{"user_id"})
	if got := src.Columns[1].DataType; got != schema.TypeInt64 {
		t.Fatalf("user_id type = %v, want %v", got, schema.TypeInt64)
	}
}

func TestNormalizeMultiTableInfoComposesNamingAndShortening(t *testing.T) {
	const longSource = "configurationDataWithANameThatExceedsThePostgresDestinationIdentifierLimitByFar"
	p := &Pipeline{
		config: &config.IngestConfig{SchemaNaming: "snake_case"},
		dest:   &mockDestination{scheme: "postgres"},
	}
	original := &schema.TableSchema{
		Columns: []schema.Column{
			{Name: "id", DataType: schema.TypeInt64, IsPrimaryKey: true},
			{Name: "configData", DataType: schema.TypeJSON, Nullable: true},
			{Name: longSource, DataType: schema.TypeString, Nullable: true},
			{Name: destination.CDCLSNColumn, DataType: schema.TypeString},
			{Name: destination.CDCDeletedColumn, DataType: schema.TypeBoolean},
			{Name: destination.CDCSyncedAtColumn, DataType: schema.TypeTimestampTZ},
			{Name: destination.CDCUnchangedColsColumn, DataType: schema.TypeString},
		},
		PrimaryKeys: []string{"id"},
	}

	normalized, renamer, err := p.normalizeMultiTableInfo(context.Background(), source.SourceTableInfo{
		Name: "public.items", Schema: original, PrimaryKeys: []string{"id"},
	}, "public.items")
	require.NoError(t, err)
	require.NotNil(t, renamer)
	require.Equal(t, "configData", original.Columns[1].Name, "normalization must not mutate the source-owned schema")
	require.Equal(t, "config_data", normalized.Schema.Columns[1].Name)
	require.Equal(t, destination.CDCUnchangedColsColumn, normalized.Schema.Columns[len(normalized.Schema.Columns)-1].Name)

	finalLong := renamer.Mapping()[longSource]
	require.NotEmpty(t, finalLong)
	require.LessOrEqual(t, len(finalLong), destination.MaxIdentifierLength("postgres"))
	require.Equal(t, finalLong, normalized.Schema.Columns[2].Name)

	dataBuilder := array.NewStringBuilder(memory.DefaultAllocator)
	dataBuilder.AppendNull()
	longBuilder := array.NewStringBuilder(memory.DefaultAllocator)
	longBuilder.AppendNull()
	markerBuilder := array.NewStringBuilder(memory.DefaultAllocator)
	markerBuilder.Append(fmt.Sprintf(`["configData","%s"]`, longSource))
	data := dataBuilder.NewArray()
	longData := longBuilder.NewArray()
	markers := markerBuilder.NewArray()
	dataBuilder.Release()
	longBuilder.Release()
	markerBuilder.Release()
	batch := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{
		{Name: "configData", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: longSource, Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: destination.CDCUnchangedColsColumn, Type: arrow.BinaryTypes.String},
	}, nil), []arrow.Array{data, longData, markers}, 1)
	data.Release()
	longData.Release()
	markers.Release()
	defer batch.Release()

	transformed, err := renamer.Transform(batch)
	require.NoError(t, err)
	defer transformed.Release()
	require.Equal(t, "config_data", transformed.Schema().Field(0).Name)
	require.Equal(t, finalLong, transformed.Schema().Field(1).Name)
	require.Equal(t, fmt.Sprintf(`["config_data","%s"]`, finalLong), transformed.Column(2).(*array.String).Value(0))
}

func TestNormalizeMultiTableInfoPropagatesRenamedPrimaryKeysToSchema(t *testing.T) {
	p := &Pipeline{
		config: &config.IngestConfig{SchemaNaming: "snake_case"},
		dest:   &mockDestination{scheme: "postgres"},
	}
	original := &schema.TableSchema{Columns: []schema.Column{
		{Name: "eventId", DataType: schema.TypeInt64},
		{Name: "payload", DataType: schema.TypeString},
	}}

	normalized, _, err := p.normalizeMultiTableInfo(context.Background(), source.SourceTableInfo{
		Name: "public.events", Schema: original, PrimaryKeys: []string{"eventId"},
	}, "public.events")
	require.NoError(t, err)
	require.Equal(t, []string{"event_id"}, normalized.PrimaryKeys)
	require.Equal(t, []string{"event_id"}, normalized.Schema.PrimaryKeys)
	require.True(t, normalized.Schema.Columns[0].IsPrimaryKey)
	require.Empty(t, original.PrimaryKeys)
	require.False(t, original.Columns[0].IsPrimaryKey)
}

func TestNormalizeMultiTableInfoRejectsCDCMetadataCollision(t *testing.T) {
	p := &Pipeline{
		config: &config.IngestConfig{SchemaNaming: "snake_case"},
		dest:   &mockDestination{scheme: "postgres"},
	}
	table := source.SourceTableInfo{Name: "public.items", Schema: &schema.TableSchema{Columns: []schema.Column{
		{Name: "_cdcLSN", DataType: schema.TypeString},
		{Name: destination.CDCLSNColumn, DataType: schema.TypeString},
	}}}

	_, _, err := p.normalizeMultiTableInfo(context.Background(), table, "public.items")
	require.ErrorContains(t, err, "reserved CDC metadata column")
}

// Case 7: realistic evolve scenario.
func TestBuildBufferReaderTarget_EvolveScenario(t *testing.T) {
	p := &Pipeline{}
	// Source order can be anything — we only care about names.
	src := tschema(
		"orders",
		tcol("age", schema.TypeInt64),
		tcol("email", schema.TypeString), // new
		tcol("id", schema.TypeInt64),
		tcol("name", schema.TypeString),
		tcol("score", schema.TypeInt64),
	)

	dest := tschema(
		"orders",
		tcol("age", schema.TypeInt64),
		tcol("id", schema.TypeInt64),
		tcol("name", schema.TypeString),
		tcol("score", schema.TypeInt64),
		tcol("email", schema.TypeString), // added by Compare.ChangeAddColumn
	)

	got := p.buildBufferReaderTarget(src, dest)

	assertColumns(t, "fields", arrowFieldNames(got), []string{"age", "id", "name", "score", "email"})
}

func TestBuildBufferReaderTarget_CaseInsensitiveMatch(t *testing.T) {
	p := &Pipeline{}
	src := tschema(
		"orders",
		tcol("id", schema.TypeInt64),
		tcol("name", schema.TypeString),
	)
	dest := tschema(
		"orders",
		tcol("ID", schema.TypeInt64),
		tcol("NAME", schema.TypeString),
	)

	got := p.buildBufferReaderTarget(src, dest)

	assertColumns(t, "fields", arrowFieldNames(got), []string{"id", "name"})
}

func TestApplyPartitionNaming(t *testing.T) {
	tests := []struct {
		name            string
		convention      naming.Convention
		partitionBy     string
		clusterBy       []string
		schemaPartition string
		wantPartitionBy string
		wantClusterBy   []string
	}{
		{
			name:            "configured partition_by normalized to snake_case",
			convention:      naming.SnakeCase,
			partitionBy:     "updatedAt",
			wantPartitionBy: "updated_at",
		},
		{
			name:            "configured partition_by untouched under direct naming",
			convention:      naming.Direct,
			partitionBy:     "updatedAt",
			wantPartitionBy: "updatedAt",
		},
		{
			name:            "source-provided partition column normalized as fallback",
			convention:      naming.SnakeCase,
			schemaPartition: "eventDate",
			wantPartitionBy: "event_date",
		},
		{
			name:            "configured partition_by wins over source-provided",
			convention:      naming.SnakeCase,
			partitionBy:     "createdAt",
			schemaPartition: "eventDate",
			wantPartitionBy: "created_at",
		},
		{
			name:          "cluster_by columns normalized to snake_case",
			convention:    naming.SnakeCase,
			clusterBy:     []string{"countryCode", "region"},
			wantClusterBy: []string{"country_code", "region"},
		},
		{
			name:          "cluster_by untouched under direct naming",
			convention:    naming.Direct,
			clusterBy:     []string{"countryCode"},
			wantClusterBy: []string{"countryCode"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.PartitionBy = tt.partitionBy
			cfg.ClusterBy = tt.clusterBy
			tableSchema := &schema.TableSchema{PartitionBy: tt.schemaPartition}

			applyPartitionNaming(cfg, tableSchema, naming.Get(tt.convention))

			if cfg.PartitionBy != tt.wantPartitionBy {
				t.Fatalf("PartitionBy = %q, want %q", cfg.PartitionBy, tt.wantPartitionBy)
			}
			if fmt.Sprint(cfg.ClusterBy) != fmt.Sprint(tt.wantClusterBy) {
				t.Fatalf("ClusterBy = %v, want %v", cfg.ClusterBy, tt.wantClusterBy)
			}
		})
	}
}
