package strategy

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
)

const (
	cdcStateVersion           = "2"
	cdcStateTableName         = "cdc_state"
	cdcTargetTableName        = "cdc_targets"
	cdcStateKindCheckpoint    = "checkpoint"
	cdcStateKindSnapshot      = "snapshot"
	cdcStateKindDestination   = "destination"
	cdcStateKindAtomicAttempt = "atomic_snapshot"
	cdcStateKindRun           = "run"
	cdcStateStatusInProgress  = "in_progress"
	cdcStateStatusReady       = "ready"
	zeroCDCPosition           = "00000000/00000000"
	cdcStatePruneThreshold    = 100
	cdcStatePruneBatchSize    = 900
	cdcStateMaxPruneBatchSize = 10000
	cdcStateWriteBatchSize    = 1000
	cdcStateInvalidationMax   = 8
	cdcManagementConnectorID  = "__ingestr_cdc_management__"
	cdcManagementSourceTable  = "__ingestr_cdc_management__"
)

var cdcStateSchema = &schema.TableSchema{Columns: []schema.Column{
	{Name: "event_id", DataType: schema.TypeString, MaxLength: 128, Nullable: false},
	{Name: "state_version", DataType: schema.TypeString, MaxLength: 16, Nullable: false},
	{Name: "connector_id", DataType: schema.TypeString, MaxLength: 64, Nullable: false},
	{Name: "source_table", DataType: schema.TypeString, MaxLength: 1000, Nullable: false},
	{Name: "destination_table", DataType: schema.TypeString, MaxLength: 1000, Nullable: false},
	{Name: "state_kind", DataType: schema.TypeString, MaxLength: 32, Nullable: false},
	{Name: "state_generation", DataType: schema.TypeInt64, Nullable: false},
	{Name: "state_status", DataType: schema.TypeString, MaxLength: 32, Nullable: false},
	{Name: "_cdc_lsn", DataType: schema.TypeString, MaxLength: 1024, Nullable: false},
	{Name: "recorded_at", DataType: schema.TypeTimestampTZ, Nullable: false},
}}

var cdcTargetSchema = &schema.TableSchema{Columns: []schema.Column{
	{Name: "destination_table", DataType: schema.TypeString, MaxLength: 2048, Nullable: false},
	{Name: "connector_id", DataType: schema.TypeString, MaxLength: 64, Nullable: false},
	{Name: "claimed_at", DataType: schema.TypeTimestampTZ, Nullable: false},
}}

type cdcStateKey struct {
	runID       string
	sourceTable string
	kind        string
}

type reducedCDCState struct {
	generation        int64
	position          string
	destTable         string
	complete          bool
	incarnation       string
	schemaFingerprint string
	snapshotEpoch     uint64
}

type cdcStateWriteEvent struct {
	entry      destination.CDCStateEntry
	recordedAt time.Time
}

// CDCStateManager stores every connector and source-table marker in one shared
// destination table. Generations invalidate older completions, while run IDs
// prevent concurrent processes from completing each other's generation.
type CDCStateManager struct {
	dest           destination.Destination
	reader         destination.CDCStateReader
	fenceReader    destination.CDCStateFenceReader
	pruner         destination.CDCStatePruner
	targetClaimer  destination.CDCTargetClaimer
	incarnation    destination.CDCTargetIncarnationProvider
	initializer    destination.CDCTargetIncarnationInitializer
	pruneBatchSize int
	connectorID    string
	stateTable     string
	targetTable    string

	mu                  sync.Mutex
	prepared            bool
	targetPrepared      bool
	loaded              bool
	started             bool
	fullRefresh         bool
	generation          int64
	runID               string
	runEventID          string
	runs                map[string]struct{}
	states              map[cdcStateKey]reducedCDCState
	destTables          map[string]string
	knownComplete       map[string]string
	currentIncarnations map[string]string
	currentSchemas      map[string]string
	knownIncarnations   map[string]string
	knownSchemas        map[string]string
	knownDestinations   map[string]string
	boundDestinations   map[string]string
	boundDestinationRaw map[string]string
	batchSnapshots      map[string]string
	atomicAttempts      map[string]atomicSnapshotJournalEntry
	atomicSequences     map[string]uint64
	lateTargetModes     map[string]lateTargetMode
	lateTargetRaw       map[string]string
	snapshotEpochs      map[string]uint64
	entries             []destination.CDCStateEntry
	cleanupDue          bool
}

type lateTargetMode uint8

type atomicSnapshotJournalEntry struct {
	attemptID               string
	sequence                uint64
	snapshotEpoch           uint64
	ready                   bool
	resumeLSN               string
	expectedIncarnation     string
	sourceMetadataKnown     bool
	sourceIncarnation       string
	sourceSchemaFingerprint string
}

const (
	lateTargetNone lateTargetMode = iota
	lateTargetCreatedEmpty
	lateTargetConditionalReplace
)

func NewCDCStateManager(dest destination.Destination, connectorID, _ string, stagingDataset string) (*CDCStateManager, error) {
	manager, err := newCDCStateManager(dest, connectorID, managedCDCStateTableName(dest, cdcStateTableName, stagingDataset))
	if err != nil {
		return nil, err
	}
	claimer, ok := dest.(destination.CDCTargetClaimer)
	if !ok {
		return nil, fmt.Errorf("destination scheme %q cannot atomically claim managed CDC targets", dest.GetScheme())
	}
	manager.targetClaimer = claimer
	manager.targetTable = managedCDCStateTableName(dest, cdcTargetTableName, stagingDataset)
	return manager, nil
}

func newCDCStateManager(dest destination.Destination, connectorID, stateTable string) (*CDCStateManager, error) {
	reader, ok := dest.(destination.CDCStateReader)
	if !ok {
		return nil, fmt.Errorf("destination scheme %q does not support destination-managed CDC state", dest.GetScheme())
	}
	fenceReader, ok := dest.(destination.CDCStateFenceReader)
	if !ok {
		return nil, fmt.Errorf("destination scheme %q does not support destination-managed CDC state fencing", dest.GetScheme())
	}
	pruner, ok := dest.(destination.CDCStatePruner)
	if !ok {
		return nil, fmt.Errorf("destination scheme %q does not support destination-managed CDC state pruning", dest.GetScheme())
	}
	if connectorID == "" {
		return nil, fmt.Errorf("CDC state connector ID is empty")
	}
	pruneBatchSize := cdcStatePruneBatchSize
	if sizer, ok := dest.(destination.CDCStatePruneBatchSizer); ok {
		if advertised := sizer.CDCStatePruneBatchSize(); advertised > 0 {
			pruneBatchSize = min(advertised, cdcStateMaxPruneBatchSize)
		}
	}
	return &CDCStateManager{
		dest:                dest,
		reader:              reader,
		fenceReader:         fenceReader,
		pruner:              pruner,
		incarnation:         destinationIncarnationProvider(dest),
		initializer:         destinationIncarnationInitializer(dest),
		pruneBatchSize:      pruneBatchSize,
		connectorID:         connectorID,
		stateTable:          stateTable,
		runs:                make(map[string]struct{}),
		states:              make(map[cdcStateKey]reducedCDCState),
		destTables:          make(map[string]string),
		knownComplete:       make(map[string]string),
		currentIncarnations: make(map[string]string),
		currentSchemas:      make(map[string]string),
		knownIncarnations:   make(map[string]string),
		knownSchemas:        make(map[string]string),
		knownDestinations:   make(map[string]string),
		boundDestinations:   make(map[string]string),
		boundDestinationRaw: make(map[string]string),
		batchSnapshots:      make(map[string]string),
		atomicAttempts:      make(map[string]atomicSnapshotJournalEntry),
		atomicSequences:     make(map[string]uint64),
		lateTargetModes:     make(map[string]lateTargetMode),
		lateTargetRaw:       make(map[string]string),
		snapshotEpochs:      make(map[string]uint64),
	}, nil
}

func destinationIncarnationProvider(dest destination.Destination) destination.CDCTargetIncarnationProvider {
	provider, _ := dest.(destination.CDCTargetIncarnationProvider)
	return provider
}

func destinationIncarnationInitializer(dest destination.Destination) destination.CDCTargetIncarnationInitializer {
	initializer, _ := dest.(destination.CDCTargetIncarnationInitializer)
	return initializer
}

func (m *CDCStateManager) RegisterTable(ctx context.Context, sourceTable, destTable string) error {
	return m.RegisterTableIncarnation(ctx, sourceTable, destTable, "")
}

func (m *CDCStateManager) RegisterTableIncarnation(ctx context.Context, sourceTable, destTable, incarnation string) error {
	return m.RegisterTableState(ctx, sourceTable, destTable, incarnation, "")
}

func (m *CDCStateManager) RegisterTableState(ctx context.Context, sourceTable, destTable, incarnation, schemaFingerprint string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.registerTable(ctx, sourceTable, destTable, incarnation, schemaFingerprint); err != nil {
		return err
	}
	return nil
}

// InvalidateSnapshot durably starts a new replacement snapshot epoch. Once
// this returns, an older completed epoch can no longer make the table
// resumable, even if the process crashes during truncate or backfill.
func (m *CDCStateManager) InvalidateSnapshot(ctx context.Context, sourceTable, destTable, incarnation string) error {
	return m.InvalidateSnapshotState(ctx, sourceTable, destTable, incarnation, "")
}

func (m *CDCStateManager) InvalidateSnapshotState(ctx context.Context, sourceTable, destTable, incarnation, schemaFingerprint string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.claimTarget(ctx, sourceTable, destTable); err != nil {
		return err
	}
	if err := m.registerTable(ctx, sourceTable, destTable, incarnation, schemaFingerprint); err != nil {
		return err
	}
	if incarnation == "" {
		incarnation = m.currentIncarnations[sourceTable]
	}
	if !m.started || m.generation == 0 {
		return fmt.Errorf("CDC state run has not started")
	}
	epoch := m.snapshotEpochs[sourceTable] + 1
	position := encodeCDCStatePositionWithSchema(zeroCDCPosition, incarnation, m.currentSchemas[sourceTable], epoch)
	if err := m.writeState(ctx, sourceTable, destTable, cdcStateKindSnapshot, m.generation, cdcStateStatusInProgress, position); err != nil {
		return fmt.Errorf("failed to invalidate CDC snapshot for %s: %w", sourceTable, err)
	}
	m.snapshotEpochs[sourceTable] = epoch
	delete(m.knownComplete, sourceTable)
	delete(m.knownIncarnations, sourceTable)
	delete(m.knownSchemas, sourceTable)
	delete(m.knownDestinations, sourceTable)
	delete(m.boundDestinations, sourceTable)
	delete(m.boundDestinationRaw, sourceTable)
	return nil
}

// BindDestinationIncarnation pins a snapshot to the physical target that will
// receive it. Persist revalidates the pin before certifying the snapshot.
func (m *CDCStateManager) BindDestinationIncarnation(ctx context.Context, sourceTable, destTable string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.registerTable(ctx, sourceTable, destTable, "", ""); err != nil {
		return err
	}
	if m.initializer != nil {
		if _, exists, err := m.initializer.EnsureCDCTargetIncarnation(ctx, destTable); err != nil {
			return fmt.Errorf("failed to initialize CDC destination table %q incarnation: %w", destTable, err)
		} else if !exists {
			return fmt.Errorf("CDC destination table %q disappeared before snapshot write", destTable)
		}
	}
	raw, current, exists, err := m.destinationIncarnationForTable(ctx, destTable)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("CDC destination table %q disappeared before snapshot write", destTable)
	}
	if bound := m.boundDestinations[sourceTable]; bound != "" && current != bound {
		return fmt.Errorf("CDC destination table %q was replaced during its snapshot", destTable)
	}
	if known := m.knownDestinations[sourceTable]; known != "" && current != known {
		return fmt.Errorf("CDC destination table %q was replaced after its resume position was selected", destTable)
	}
	m.boundDestinations[sourceTable] = current
	m.boundDestinationRaw[sourceTable] = raw
	return nil
}

func (m *CDCStateManager) BoundDestinationIncarnation(sourceTable string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.boundDestinationRaw[sourceTable]
}

// AtomicSnapshotAttempt returns the durable identity of the current snapshot
// publication. An incomplete attempt is carried into the next run so an
// ambiguous successful publish is retried with the same idempotency key.
func (m *CDCStateManager) AtomicSnapshotAttempt(ctx context.Context, sourceTable, destTable string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started || m.generation == 0 {
		return "", fmt.Errorf("CDC state run has not started")
	}
	registered, ok := m.destTables[sourceTable]
	if !ok || registered != destTable {
		return "", fmt.Errorf("CDC state is not registered for source table %q and destination %q", sourceTable, destTable)
	}
	if attempt, exists := m.atomicAttempts[sourceTable]; exists {
		if attempt.ready {
			return "", fmt.Errorf("sealed atomic snapshot attempt for %s must be recovered before source read", sourceTable)
		}
		return attempt.attemptID, nil
	}
	attemptID, err := newCDCStateRunID()
	if err != nil {
		return "", fmt.Errorf("failed to generate atomic snapshot attempt for %s: %w", sourceTable, err)
	}
	sequence := m.atomicSequences[sourceTable] + 1
	attempt := atomicSnapshotJournalEntry{
		attemptID:     attemptID,
		sequence:      sequence,
		snapshotEpoch: m.snapshotEpochs[sourceTable],
	}
	if err := m.writeState(
		ctx,
		sourceTable,
		destTable,
		cdcStateKindAtomicAttempt,
		m.generation,
		cdcStateStatusInProgress,
		encodeAtomicSnapshotAttempt(attempt),
	); err != nil {
		return "", fmt.Errorf("failed to persist atomic snapshot attempt for %s: %w", sourceTable, err)
	}
	m.atomicAttempts[sourceTable] = attempt
	m.atomicSequences[sourceTable] = sequence
	return attemptID, nil
}

func (m *CDCStateManager) SealAtomicSnapshotAttempt(
	ctx context.Context,
	sourceTable, destTable, attemptID, resumeLSN, expectedIncarnation string,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	attempt, exists := m.atomicAttempts[sourceTable]
	if !m.started || !exists || attemptID == "" || attempt.attemptID != attemptID {
		return fmt.Errorf("atomic snapshot attempt for %s does not match the active durable attempt", sourceTable)
	}
	if m.destTables[sourceTable] != destTable {
		return fmt.Errorf("CDC state destination for %s is %q, not %q", sourceTable, m.destTables[sourceTable], destTable)
	}
	if !cdcStatePositionValid(resumeLSN) || expectedIncarnation == "" {
		return fmt.Errorf("atomic snapshot attempt for %s has an invalid durable boundary or publication fence", sourceTable)
	}
	attempt.ready = true
	attempt.resumeLSN = resumeLSN
	attempt.expectedIncarnation = expectedIncarnation
	attempt.sourceMetadataKnown = true
	attempt.sourceIncarnation = m.currentIncarnations[sourceTable]
	attempt.sourceSchemaFingerprint = m.currentSchemas[sourceTable]
	if err := m.writeState(
		ctx,
		sourceTable,
		destTable,
		cdcStateKindAtomicAttempt,
		m.generation,
		cdcStateStatusReady,
		encodeAtomicSnapshotAttempt(attempt),
	); err != nil {
		return fmt.Errorf("failed to seal atomic snapshot attempt for %s: %w", sourceTable, err)
	}
	m.atomicAttempts[sourceTable] = attempt
	return nil
}

func (m *CDCStateManager) PendingAtomicSnapshotAttempt(sourceTable, destTable string) (atomicSnapshotJournalEntry, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	attempt, exists := m.atomicAttempts[sourceTable]
	if !exists {
		return atomicSnapshotJournalEntry{}, false, nil
	}
	if m.destTables[sourceTable] != destTable {
		return atomicSnapshotJournalEntry{}, false, fmt.Errorf(
			"CDC state destination for %s is %q, not %q",
			sourceTable,
			m.destTables[sourceTable],
			destTable,
		)
	}
	return attempt, true, nil
}

func (m *CDCStateManager) FullRefresh() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.fullRefresh
}

func (m *CDCStateManager) CompleteAtomicSnapshotAttempt(ctx context.Context, sourceTable, destTable, attemptID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started || m.generation == 0 {
		return fmt.Errorf("CDC state run has not started")
	}
	attempt, exists := m.atomicAttempts[sourceTable]
	if !exists || attemptID == "" || attempt.attemptID != attemptID {
		return fmt.Errorf("atomic snapshot attempt for %s does not match the active durable attempt", sourceTable)
	}
	if registered := m.destTables[sourceTable]; registered != destTable {
		return fmt.Errorf("CDC state destination for %s is %q, not %q", sourceTable, registered, destTable)
	}
	if err := m.writeState(
		ctx,
		sourceTable,
		destTable,
		cdcStateKindAtomicAttempt,
		m.generation,
		destination.CDCStateStatusComplete,
		encodeAtomicSnapshotAttempt(attempt),
	); err != nil {
		return fmt.Errorf("failed to complete atomic snapshot attempt for %s: %w", sourceTable, err)
	}
	delete(m.atomicAttempts, sourceTable)
	return nil
}

func (m *CDCStateManager) AbandonAtomicSnapshotAttempt(ctx context.Context, sourceTable, destTable, attemptID string) error {
	return m.CompleteAtomicSnapshotAttempt(ctx, sourceTable, destTable, attemptID)
}

func (m *CDCStateManager) DestinationIncarnationForPublication(ctx context.Context, destTable string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, _, exists, err := m.destinationIncarnationForTable(ctx, destTable)
	if err != nil {
		return "", err
	}
	if !exists || raw == "" {
		return "", fmt.Errorf("CDC replacement table %q has no stable physical incarnation", destTable)
	}
	return raw, nil
}

// BindPublishedDestinationIncarnation accepts the physical incarnation created
// by a destination-fenced replacement. The caller must provide the incarnation
// that guarded publication so an unrelated or stale run cannot rebind state.
func (m *CDCStateManager) BindPublishedDestinationIncarnation(
	ctx context.Context,
	sourceTable, destTable, expectedPrevious, expectedPublished string,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if expectedPrevious == "" || expectedPublished == "" || m.boundDestinationRaw[sourceTable] != expectedPrevious {
		return fmt.Errorf("CDC destination table %q replacement was not fenced by its bound incarnation", destTable)
	}
	if err := m.registerTable(ctx, sourceTable, destTable, "", ""); err != nil {
		return err
	}
	raw, current, exists, err := m.destinationIncarnationForTable(ctx, destTable)
	if err != nil {
		return err
	}
	if !exists || raw != expectedPublished {
		return fmt.Errorf("CDC destination table %q changed after its fenced replacement was published", destTable)
	}
	m.boundDestinations[sourceTable] = current
	m.boundDestinationRaw[sourceTable] = raw
	return nil
}

func (m *CDCStateManager) BindRecoveredPublishedDestinationIncarnation(
	ctx context.Context,
	sourceTable, destTable, expectedPublished string,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if expectedPublished == "" {
		return fmt.Errorf("CDC destination table %q recovered publication has no physical incarnation", destTable)
	}
	if err := m.registerTable(ctx, sourceTable, destTable, "", ""); err != nil {
		return err
	}
	raw, current, exists, err := m.destinationIncarnationForTable(ctx, destTable)
	if err != nil {
		return err
	}
	if !exists || raw != expectedPublished {
		return fmt.Errorf("CDC destination table %q changed after its recovered fenced replacement was published", destTable)
	}
	m.boundDestinations[sourceTable] = current
	m.boundDestinationRaw[sourceTable] = raw
	return nil
}

func (m *CDCStateManager) CurrentSourceMetadata(sourceTable string) (incarnation, schemaFingerprint string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.currentIncarnations[sourceTable], m.currentSchemas[sourceTable]
}

func (m *CDCStateManager) InvalidateSnapshotPreservingDestination(ctx context.Context, sourceTable, destTable, incarnation string) error {
	return m.InvalidateSnapshotStatePreservingDestination(ctx, sourceTable, destTable, incarnation, "")
}

func (m *CDCStateManager) InvalidateSnapshotStatePreservingDestination(ctx context.Context, sourceTable, destTable, incarnation, schemaFingerprint string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.claimTarget(ctx, sourceTable, destTable); err != nil {
		return err
	}
	if err := m.registerTable(ctx, sourceTable, destTable, incarnation, schemaFingerprint); err != nil {
		return err
	}
	if incarnation == "" {
		incarnation = m.currentIncarnations[sourceTable]
	}
	if !m.started || m.generation == 0 {
		return fmt.Errorf("CDC state run has not started")
	}
	expected := m.boundDestinations[sourceTable]
	if expected == "" {
		expected = m.knownDestinations[sourceTable]
	}
	if expected == "" {
		return fmt.Errorf("CDC destination table %q has no previously verified physical incarnation", destTable)
	}
	raw, current, exists, err := m.destinationIncarnationForTable(ctx, destTable)
	if err != nil {
		return err
	}
	if !exists || current != expected {
		return fmt.Errorf("CDC destination table %q was replaced after its prior snapshot boundary", destTable)
	}
	epoch := m.snapshotEpochs[sourceTable] + 1
	position := encodeCDCStatePositionWithSchema(zeroCDCPosition, incarnation, m.currentSchemas[sourceTable], epoch)
	if err := m.writeState(ctx, sourceTable, destTable, cdcStateKindSnapshot, m.generation, cdcStateStatusInProgress, position); err != nil {
		return fmt.Errorf("failed to invalidate CDC snapshot for %s: %w", sourceTable, err)
	}
	_, afterWrite, exists, err := m.destinationIncarnationForTable(ctx, destTable)
	if err != nil {
		return err
	}
	if !exists || afterWrite != expected {
		return fmt.Errorf("CDC destination table %q changed while invalidating its prior snapshot boundary", destTable)
	}
	m.snapshotEpochs[sourceTable] = epoch
	delete(m.knownComplete, sourceTable)
	delete(m.knownIncarnations, sourceTable)
	delete(m.knownSchemas, sourceTable)
	delete(m.knownDestinations, sourceTable)
	m.boundDestinations[sourceTable] = expected
	m.boundDestinationRaw[sourceTable] = raw
	return nil
}

// ClaimTarget reserves a destination table for one logical source table before
// any target or staging table is prepared.
func (m *CDCStateManager) ClaimTarget(ctx context.Context, sourceTable, destTable string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.claimTarget(ctx, sourceTable, destTable)
}

// ClaimLateDiscoveredTarget validates a target that was not part of the
// startup table set before permanently reserving it. An existing target may
// only be reused when this connector has previously completed a snapshot that
// still matches both the source metadata and the physical destination table.
func (m *CDCStateManager) ClaimLateDiscoveredTarget(
	ctx context.Context,
	sourceTable, destTable, incarnation, schemaFingerprint string,
	allowReplacement bool,
	targetOptions destination.PrepareOptions,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.load(ctx); err != nil {
		return err
	}
	if !m.started || !m.targetPrepared {
		return fmt.Errorf("CDC state run has not started")
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}

	rawDestinationIncarnation, destinationIncarnation, exists, err := m.destinationIncarnationForTable(ctx, destTable)
	if err != nil {
		return err
	}
	if err := m.validateTarget(ctx, destTable); err != nil {
		return err
	}
	if !exists {
		preparer, ok := m.dest.(destination.CDCLateTargetClaimPreparer)
		if !ok {
			return fmt.Errorf("destination scheme %q cannot atomically claim and create an empty late-discovered CDC target", m.dest.GetScheme())
		}
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		targetOptions.Table = destTable
		createdIncarnation, err := preparer.ClaimAndPrepareEmptyCDCTarget(ctx, m.targetTable, destination.CDCTargetClaim{
			DestinationTable: destTable,
			ConnectorID:      m.connectorID,
			SourceTable:      sourceTable,
		}, targetOptions)
		if err != nil {
			return fmt.Errorf("failed to atomically claim and create CDC destination table %q: %w", destTable, err)
		}
		if createdIncarnation == "" {
			return fmt.Errorf("destination scheme %q created CDC destination table %q without a provable physical incarnation", m.dest.GetScheme(), destTable)
		}
		m.lateTargetModes[sourceTable] = lateTargetCreatedEmpty
		m.lateTargetRaw[sourceTable] = createdIncarnation
		m.boundDestinations[sourceTable] = cdcDestinationIncarnationDigest(createdIncarnation)
		m.boundDestinationRaw[sourceTable] = createdIncarnation
		return nil
	}
	if !allowReplacement {
		if incarnation == "" || compactSchemaFingerprint(schemaFingerprint) == "" {
			return fmt.Errorf("destination table %q already exists, but newly discovered source table %q has no complete incarnation and schema proof", destTable, sourceTable)
		}
		if rawDestinationIncarnation == "" || !m.hasCompletedTargetAuthorization(
			sourceTable,
			destTable,
			incarnation,
			schemaFingerprint,
			destinationIncarnation,
		) {
			return fmt.Errorf("destination table %q already exists, but no matching completed CDC state authorizes replacing it for newly discovered source table %q; rerun with --full-refresh or restore the completed CDC state", destTable, sourceTable)
		}
		if _, ok := m.dest.(destination.CDCConditionalTruncater); !ok {
			return fmt.Errorf("destination scheme %q cannot conditionally truncate an authorized late-discovered CDC target", m.dest.GetScheme())
		}
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if err := m.claimTarget(ctx, sourceTable, destTable); err != nil {
		return err
	}
	m.lateTargetModes[sourceTable] = lateTargetConditionalReplace
	m.lateTargetRaw[sourceTable] = rawDestinationIncarnation
	m.boundDestinations[sourceTable] = destinationIncarnation
	m.boundDestinationRaw[sourceTable] = rawDestinationIncarnation
	return nil
}

func (m *CDCStateManager) destinationIncarnationForTable(ctx context.Context, destTable string) (raw, digest string, exists bool, err error) {
	if m.incarnation != nil {
		incarnation, exists, err := m.incarnation.CDCTargetIncarnation(ctx, destTable)
		if err != nil {
			return "", "", false, fmt.Errorf("failed to verify CDC destination table %q incarnation: %w", destTable, err)
		}
		if !exists {
			return "", "", false, nil
		}
		if incarnation == "" {
			return "", "", true, nil
		}
		return incarnation, cdcDestinationIncarnationDigest(incarnation), true, nil
	}

	tableSchema, err := m.dest.GetTableSchema(ctx, destTable)
	if err != nil {
		return "", "", false, fmt.Errorf("failed to verify CDC destination table %q: %w", destTable, err)
	}
	if tableSchema == nil {
		return "", "", false, nil
	}
	return "", "", true, nil
}

func (m *CDCStateManager) hasCompletedTargetAuthorization(
	sourceTable, destTable, incarnation, schemaFingerprint, destinationIncarnation string,
) bool {
	type runState struct {
		snapshot    reducedCDCState
		destination reducedCDCState
	}

	runsByGeneration := make(map[int64]map[string]*runState)
	for _, entry := range m.entries {
		runID, ok := cdcStateRunID(entry.EventID, m.connectorID)
		if !ok {
			continue
		}
		runs := runsByGeneration[entry.Generation]
		if runs == nil {
			runs = make(map[string]*runState)
			runsByGeneration[entry.Generation] = runs
		}
		state := runs[runID]
		if state == nil {
			state = &runState{}
			runs[runID] = state
		}
		if entry.SourceTable != sourceTable || entry.DestinationTable != destTable {
			continue
		}
		position, stateIncarnation, epoch, valid := decodeCDCStateEntry(entry)
		if !valid {
			continue
		}
		switch entry.StateKind {
		case cdcStateKindSnapshot:
			state.snapshot = reduceCDCStateEntry(state.snapshot, entry, position, stateIncarnation, epoch)
		case cdcStateKindDestination:
			state.destination = reduceCDCStateEntry(state.destination, entry, position, stateIncarnation, epoch)
		}
	}

	for _, runs := range runsByGeneration {
		if len(runs) != 1 {
			continue
		}
		var state *runState
		for _, candidate := range runs {
			state = candidate
		}
		if !state.snapshot.complete || !state.destination.complete {
			continue
		}
		if state.snapshot.destTable != destTable || state.destination.destTable != destTable ||
			state.snapshot.snapshotEpoch != state.destination.snapshotEpoch ||
			compareCDCPositions(state.snapshot.position, state.destination.position) != 0 {
			continue
		}
		if incarnation != "" && state.snapshot.incarnation != incarnation {
			continue
		}
		if currentSchema := compactSchemaFingerprint(schemaFingerprint); currentSchema != "" && state.snapshot.schemaFingerprint != currentSchema {
			continue
		}
		if destinationIncarnation == "" || state.destination.incarnation != destinationIncarnation {
			continue
		}
		return true
	}
	return false
}

func (m *CDCStateManager) claimTarget(ctx context.Context, sourceTable, destTable string) error {
	if err := m.validateTarget(ctx, destTable); err != nil {
		return err
	}
	if m.targetClaimer != nil {
		if err := m.prepareTargetTable(ctx); err != nil {
			return err
		}
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		if err := m.targetClaimer.ClaimCDCTarget(ctx, m.targetTable, destination.CDCTargetClaim{
			DestinationTable: destTable,
			ConnectorID:      m.connectorID,
			SourceTable:      sourceTable,
		}); err != nil {
			return fmt.Errorf("failed to claim CDC destination table %q: %w", destTable, err)
		}
		if m.initializer != nil {
			if _, _, err := m.initializer.EnsureCDCTargetIncarnation(ctx, destTable); err != nil {
				return fmt.Errorf("failed to initialize CDC destination table %q incarnation: %w", destTable, err)
			}
		}
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (m *CDCStateManager) validateTarget(ctx context.Context, destTable string) error {
	if validator, ok := m.dest.(destination.ManagedCDCTargetValidator); ok {
		if err := validator.ValidateManagedCDCTarget(ctx, destTable); err != nil {
			return fmt.Errorf("CDC destination table %q failed managed-state validation: %w", destTable, err)
		}
	}
	return nil
}

// ApplyLateSnapshotBoundary handles the first replacement boundary for a
// late-discovered table. A target created empty by the atomic claim protocol
// needs no truncate; an existing target is truncated only by an incarnation
// compare-and-swap operation supplied by the destination.
func (m *CDCStateManager) ApplyLateSnapshotBoundary(ctx context.Context, sourceTable, destTable string) (bool, error) {
	return m.applyLateSnapshotBoundary(ctx, sourceTable, destTable, true)
}

func (m *CDCStateManager) BindLateAtomicSnapshotBoundary(ctx context.Context, sourceTable, destTable string) (bool, error) {
	return m.applyLateSnapshotBoundary(ctx, sourceTable, destTable, false)
}

func (m *CDCStateManager) applyLateSnapshotBoundary(ctx context.Context, sourceTable, destTable string, truncateVisibleTarget bool) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	mode := m.lateTargetModes[sourceTable]
	if mode == lateTargetNone {
		return false, nil
	}
	if err := m.registerTable(ctx, sourceTable, destTable, "", ""); err != nil {
		return true, err
	}
	expectedRaw := m.lateTargetRaw[sourceTable]
	current, exists, err := m.currentDestinationIncarnation(ctx, sourceTable)
	if err != nil {
		return true, err
	}
	expectedDigest := cdcDestinationIncarnationDigest(expectedRaw)
	if !exists || current != expectedDigest {
		return true, fmt.Errorf("CDC destination table %q was replaced after its late-target claim", destTable)
	}

	if mode == lateTargetConditionalReplace && truncateVisibleTarget {
		truncater, ok := m.dest.(destination.CDCConditionalTruncater)
		if !ok {
			return true, fmt.Errorf("destination scheme %q cannot conditionally truncate late-discovered CDC targets", m.dest.GetScheme())
		}
		if err := truncater.TruncateCDCTableIfIncarnation(ctx, destTable, expectedRaw); err != nil {
			return true, fmt.Errorf("failed to conditionally truncate CDC destination table %q: %w", destTable, err)
		}
	}
	m.boundDestinations[sourceTable] = expectedDigest
	m.boundDestinationRaw[sourceTable] = expectedRaw
	delete(m.lateTargetModes, sourceTable)
	delete(m.lateTargetRaw, sourceTable)
	return true, nil
}

func (m *CDCStateManager) HasPendingLateSnapshotBoundary(sourceTable string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lateTargetModes[sourceTable] != lateTargetNone
}

func (m *CDCStateManager) registerTable(ctx context.Context, sourceTable, destTable, incarnation, schemaFingerprint string) error {
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if _, exists := m.destTables[sourceTable]; exists {
		m.destTables[sourceTable] = destTable
		if incarnation != "" || m.currentIncarnations[sourceTable] == "" {
			m.currentIncarnations[sourceTable] = incarnation
		}
		if schemaFingerprint != "" || m.currentSchemas[sourceTable] == "" {
			m.currentSchemas[sourceTable] = schemaFingerprint
		}
		return nil
	}
	m.destTables[sourceTable] = destTable
	m.currentIncarnations[sourceTable] = incarnation
	m.currentSchemas[sourceTable] = schemaFingerprint
	if m.started {
		if err := m.prepareTable(ctx); err != nil {
			return err
		}
		return m.writeState(ctx, sourceTable, destTable, cdcStateKindSnapshot, m.generation, cdcStateStatusInProgress, zeroCDCPosition)
	}
	return nil
}

func (m *CDCStateManager) prepareTargetTable(ctx context.Context) error {
	if m.targetPrepared {
		return nil
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if err := m.dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: m.targetTable, Schema: cdcTargetSchema, DropFirst: false, PrimaryKeys: []string{"destination_table"},
	}); err != nil {
		return fmt.Errorf("failed to prepare shared CDC target registry %s: %w", m.targetTable, err)
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	managementClaim := destination.CDCTargetClaim{
		ConnectorID: cdcManagementConnectorID,
		SourceTable: cdcManagementSourceTable,
	}
	for _, table := range []string{m.stateTable, m.targetTable} {
		managementClaim.DestinationTable = table
		if err := m.targetClaimer.ClaimCDCTarget(ctx, m.targetTable, managementClaim); err != nil {
			return fmt.Errorf("failed to reserve managed CDC table %q: %w", table, err)
		}
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
	}
	m.targetPrepared = true
	return nil
}

// RegisterTableForRead records the source-to-destination mapping without
// creating the state table, for paths that only need to consult state (such
// as invalidating a missing source table) without starting a run.
func (m *CDCStateManager) RegisterTableForRead(sourceTable, destTable string) {
	m.RegisterTableForReadIncarnation(sourceTable, destTable, "")
}

func (m *CDCStateManager) RegisterTableForReadIncarnation(sourceTable, destTable, incarnation string) {
	m.RegisterTableForReadState(sourceTable, destTable, incarnation, "")
}

func (m *CDCStateManager) RegisterTableForReadState(sourceTable, destTable, incarnation, schemaFingerprint string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.destTables[sourceTable] = destTable
	m.currentIncarnations[sourceTable] = incarnation
	m.currentSchemas[sourceTable] = schemaFingerprint
}

// ResumePosition requires a complete marker for the latest generation of the
// source table. The later complete connector checkpoint is then safe to use.
func (m *CDCStateManager) ResumePosition(ctx context.Context, sourceTable string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.destTables[sourceTable]; !ok {
		return "", fmt.Errorf("CDC state is not registered for source table %q", sourceTable)
	}
	if err := m.load(ctx); err != nil {
		return "", err
	}

	if len(m.runs) != 1 {
		return "", nil
	}
	runID := onlyCDCStateRun(m.runs)
	snapshot := m.states[cdcStateKey{runID: runID, sourceTable: sourceTable, kind: cdcStateKindSnapshot}]
	if !snapshot.complete {
		return "", nil
	}
	if snapshot.destTable != m.destTables[sourceTable] {
		return "", fmt.Errorf("CDC state ID %q maps source table %q to destination %q, not %q", m.connectorID, sourceTable, snapshot.destTable, m.destTables[sourceTable])
	}
	if current := m.currentIncarnations[sourceTable]; current != "" && snapshot.incarnation != current {
		return "", nil
	}
	if current := compactSchemaFingerprint(m.currentSchemas[sourceTable]); current != "" && snapshot.schemaFingerprint != current {
		return "", nil
	}
	if m.incarnation == nil {
		return "", nil
	}
	destinationState := m.states[cdcStateKey{runID: runID, sourceTable: sourceTable, kind: cdcStateKindDestination}]
	if !destinationState.complete || destinationState.destTable != m.destTables[sourceTable] ||
		destinationState.snapshotEpoch != snapshot.snapshotEpoch || compareCDCPositions(destinationState.position, snapshot.position) != 0 {
		return "", nil
	}
	currentDestination, exists, err := m.currentDestinationIncarnation(ctx, sourceTable)
	if err != nil {
		return "", err
	}
	if !exists || currentDestination != destinationState.incarnation {
		return "", nil
	}
	destSchema, err := m.dest.GetTableSchema(ctx, m.destTables[sourceTable])
	if err != nil {
		return "", fmt.Errorf("failed to verify CDC destination table %q: %w", m.destTables[sourceTable], err)
	}
	if destSchema == nil {
		return "", nil
	}
	m.knownComplete[sourceTable] = snapshot.position
	m.knownIncarnations[sourceTable] = snapshot.incarnation
	m.knownSchemas[sourceTable] = snapshot.schemaFingerprint
	m.knownDestinations[sourceTable] = destinationState.incarnation

	checkpoint := m.states[cdcStateKey{runID: runID, kind: cdcStateKindCheckpoint}]
	if checkpoint.complete && compareCDCPositions(checkpoint.position, snapshot.position) > 0 {
		return checkpoint.position, nil
	}
	return snapshot.position, nil
}

func (m *CDCStateManager) currentDestinationIncarnation(ctx context.Context, sourceTable string) (string, bool, error) {
	if m.incarnation == nil {
		return "", false, nil
	}
	incarnation, exists, err := m.incarnation.CDCTargetIncarnation(ctx, m.destTables[sourceTable])
	if err != nil {
		return "", false, fmt.Errorf("failed to verify CDC destination table %q incarnation: %w", m.destTables[sourceTable], err)
	}
	if !exists {
		return "", false, nil
	}
	return cdcDestinationIncarnationDigest(incarnation), true, nil
}

// StateEmpty reports whether this connector has no state events at all. Once
// state exists, an empty resume position can mean a crashed or conflicting run
// and must force a snapshot instead of trusting a potentially partial
// destination table.
func (m *CDCStateManager) StateEmpty(ctx context.Context) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.load(ctx); err != nil {
		return false, err
	}
	return len(m.entries) == 0, nil
}

type atomicSnapshotAttemptCandidate struct {
	entry     atomicSnapshotJournalEntry
	destTable string
	terminal  bool
}

func (m *CDCStateManager) recoverAtomicSnapshotAttempts() (
	map[string]atomicSnapshotJournalEntry,
	map[string]uint64,
	error,
) {
	candidates := make(map[string]*atomicSnapshotAttemptCandidate)
	for _, entry := range m.entries {
		if entry.StateKind != cdcStateKindAtomicAttempt {
			continue
		}
		journal, valid := decodeAtomicSnapshotAttempt(entry.Position)
		if !valid {
			continue
		}
		candidate := candidates[entry.SourceTable]
		if candidate == nil || journal.sequence > candidate.entry.sequence {
			candidate = &atomicSnapshotAttemptCandidate{entry: journal, destTable: entry.DestinationTable}
			candidates[entry.SourceTable] = candidate
		}
		if journal.sequence != candidate.entry.sequence {
			continue
		}
		if journal.attemptID != candidate.entry.attemptID ||
			journal.snapshotEpoch != candidate.entry.snapshotEpoch ||
			entry.DestinationTable != candidate.destTable {
			return nil, nil, fmt.Errorf(
				"CDC source table %q has conflicting atomic snapshot attempt sequence %d",
				entry.SourceTable,
				journal.sequence,
			)
		}
		if journal.ready {
			if candidate.entry.ready &&
				(journal.resumeLSN != candidate.entry.resumeLSN ||
					journal.expectedIncarnation != candidate.entry.expectedIncarnation ||
					journal.sourceMetadataKnown != candidate.entry.sourceMetadataKnown ||
					journal.sourceIncarnation != candidate.entry.sourceIncarnation ||
					journal.sourceSchemaFingerprint != candidate.entry.sourceSchemaFingerprint) {
				return nil, nil, fmt.Errorf(
					"CDC source table %q has conflicting sealed atomic snapshot attempt %q",
					entry.SourceTable,
					journal.attemptID,
				)
			}
			candidate.entry = journal
		}
		candidate.terminal = candidate.terminal || entry.Status == destination.CDCStateStatusComplete
	}

	recovered := make(map[string]atomicSnapshotJournalEntry)
	sequences := make(map[string]uint64)
	for sourceTable, candidate := range candidates {
		sequences[sourceTable] = candidate.entry.sequence
		if candidate.terminal {
			continue
		}
		destTable, registered := m.destTables[sourceTable]
		if !registered || candidate.destTable != destTable {
			continue
		}
		recovered[sourceTable] = candidate.entry
	}
	return recovered, sequences, nil
}

// BeginRun appends an in-progress marker for every registered source table at
// a new connector generation. A crash leaves that generation incomplete, so
// older completed rows cannot make a partial target resumable.
func (m *CDCStateManager) BeginRun(ctx context.Context, fullRefresh bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.prepareTable(ctx); err != nil {
		return err
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if err := m.load(ctx); err != nil {
		return err
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	recoveredAttempts, recoveredSequences, err := m.recoverAtomicSnapshotAttempts()
	if err != nil {
		return err
	}
	if fullRefresh {
		m.knownComplete = make(map[string]string)
		m.knownIncarnations = make(map[string]string)
		m.knownDestinations = make(map[string]string)
	}
	m.boundDestinations = make(map[string]string)
	m.boundDestinationRaw = make(map[string]string)
	m.batchSnapshots = make(map[string]string)
	m.atomicAttempts = make(map[string]atomicSnapshotJournalEntry, len(recoveredAttempts))
	m.atomicSequences = recoveredSequences
	m.snapshotEpochs = make(map[string]uint64)
	m.fullRefresh = fullRefresh
	for sourceTable, recovered := range recoveredAttempts {
		m.atomicAttempts[sourceTable] = recovered
		m.snapshotEpochs[sourceTable] = recovered.snapshotEpoch
	}
	m.generation++
	runID, err := newCDCStateRunID()
	if err != nil {
		return err
	}
	m.runID = runID
	m.runs = map[string]struct{}{runID: {}}
	m.states = make(map[cdcStateKey]reducedCDCState)
	runEvent := m.newStateWriteEvent("", "", cdcStateKindRun, m.generation, cdcStateStatusInProgress, zeroCDCPosition)
	m.runEventID = runEvent.entry.EventID
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if err := m.writeStateEvents(ctx, []cdcStateWriteEvent{runEvent}); err != nil {
		return err
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	m.started = true

	tables := make([]string, 0, len(m.destTables))
	for table := range m.destTables {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	events := make([]cdcStateWriteEvent, 0, len(tables)+len(recoveredAttempts))
	for _, sourceTable := range tables {
		events = append(events, m.newStateWriteEvent(sourceTable, m.destTables[sourceTable], cdcStateKindSnapshot, m.generation, cdcStateStatusInProgress, zeroCDCPosition))
		if attempt, exists := m.atomicAttempts[sourceTable]; exists {
			status := cdcStateStatusInProgress
			if attempt.ready {
				status = cdcStateStatusReady
			}
			events = append(events, m.newStateWriteEvent(
				sourceTable,
				m.destTables[sourceTable],
				cdcStateKindAtomicAttempt,
				m.generation,
				status,
				encodeAtomicSnapshotAttempt(attempt),
			))
		}
	}
	if err := m.writeStateEvents(ctx, events); err != nil {
		return err
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	m.pruneSuperseded(ctx)
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	return nil
}

func (m *CDCStateManager) Persist(ctx context.Context, token source.CDCStateCommitToken) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started || m.generation == 0 {
		return fmt.Errorf("CDC state run has not started")
	}
	token.SnapshotPositions = cloneStringMap(token.SnapshotPositions)
	token.SnapshotIncarnations = cloneStringMap(token.SnapshotIncarnations)
	token.SnapshotSchemas = cloneStringMap(token.SnapshotSchemas)
	for table, position := range m.batchSnapshots {
		if position == "" {
			position = token.Position
		}
		if position == "" {
			return fmt.Errorf("completed batch snapshot for %s has no durable source position", table)
		}
		token.SnapshotPositions[table] = position
	}
	if token.Position != "" && !cdcStatePositionValid(token.Position) {
		return fmt.Errorf("invalid CDC checkpoint position %q", token.Position)
	}
	for table, position := range token.SnapshotPositions {
		if !cdcStatePositionValid(position) {
			return fmt.Errorf("invalid CDC snapshot position %q for %s", position, table)
		}
	}
	for table := range token.SnapshotIncarnations {
		if _, ok := token.SnapshotPositions[table]; !ok {
			return fmt.Errorf("CDC snapshot incarnation provided without a snapshot position for %s", table)
		}
	}
	for table := range token.SnapshotSchemas {
		if _, ok := token.SnapshotPositions[table]; !ok {
			return fmt.Errorf("CDC snapshot schema provided without a snapshot position for %s", table)
		}
	}
	fence, err := m.fenceReader.LoadCDCStateFence(ctx, m.stateTable, m.connectorID)
	if err != nil {
		return fmt.Errorf("failed to load CDC state ownership fence: %w", err)
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	logicalRunEventIDs := uniqueCDCStateEventIDs(fence.RunEventIDs)
	if fence.Generation != m.generation || len(logicalRunEventIDs) != 1 || logicalRunEventIDs[0] != m.runEventID {
		ownershipErr := fmt.Errorf("CDC state run %s lost exclusive ownership of generation %d", m.runID, m.generation)
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		return m.invalidateAfterFenceLoss(ctx, fence, ownershipErr)
	}

	positions := make(map[string]string, len(token.SnapshotPositions)+len(m.knownComplete))
	for table, position := range token.SnapshotPositions {
		positions[table] = position
	}
	for table, position := range m.knownComplete {
		if _, snapshotted := positions[table]; snapshotted {
			continue
		}
		if token.Position != "" {
			position = token.Position
		}
		positions[table] = position
	}

	tables := make([]string, 0, len(positions))
	for table := range positions {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	capturedDestinations := make(map[string]string, len(tables))
	if m.incarnation != nil {
		for sourceTable, known := range m.knownDestinations {
			if _, freshSnapshot := token.SnapshotPositions[sourceTable]; freshSnapshot {
				continue
			}
			current, exists, err := m.currentDestinationIncarnation(ctx, sourceTable)
			if err != nil {
				return err
			}
			if !exists || current != known {
				return fmt.Errorf("CDC destination table %q was replaced after its completed snapshot", m.destTables[sourceTable])
			}
			capturedDestinations[sourceTable] = current
		}
		for _, sourceTable := range tables {
			current, exists, err := m.currentDestinationIncarnation(ctx, sourceTable)
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("CDC destination table %q disappeared before state persistence", m.destTables[sourceTable])
			}
			_, freshSnapshot := token.SnapshotPositions[sourceTable]
			if bound := m.boundDestinations[sourceTable]; freshSnapshot && bound != "" && current != bound {
				return fmt.Errorf("CDC destination table %q was replaced during its snapshot", m.destTables[sourceTable])
			}
			if known := m.knownDestinations[sourceTable]; known != "" && !freshSnapshot && current != known {
				return fmt.Errorf("CDC destination table %q was replaced after its completed snapshot", m.destTables[sourceTable])
			}
			capturedDestinations[sourceTable] = current
		}
	}

	events := make([]cdcStateWriteEvent, 0, len(tables)*2+1)
	if token.Position != "" {
		events = append(events, m.newStateWriteEvent("", "", cdcStateKindCheckpoint, m.generation, destination.CDCStateStatusComplete, token.Position))
	}
	for _, sourceTable := range tables {
		destTable, ok := m.destTables[sourceTable]
		if !ok {
			return fmt.Errorf("CDC state is not registered for source table %q", sourceTable)
		}
		position := positions[sourceTable]
		incarnation := token.SnapshotIncarnations[sourceTable]
		if incarnation == "" {
			incarnation = m.knownIncarnations[sourceTable]
		}
		if incarnation == "" {
			incarnation = m.currentIncarnations[sourceTable]
		}
		if current := m.currentIncarnations[sourceTable]; current != "" && incarnation != current {
			return fmt.Errorf("CDC snapshot for %s has source incarnation %q, want %q", sourceTable, incarnation, current)
		}
		schemaFingerprint := token.SnapshotSchemas[sourceTable]
		if schemaFingerprint == "" {
			schemaFingerprint = m.knownSchemas[sourceTable]
		}
		if schemaFingerprint == "" {
			schemaFingerprint = m.currentSchemas[sourceTable]
		}
		schemaFingerprint = compactSchemaFingerprint(schemaFingerprint)
		if current := compactSchemaFingerprint(m.currentSchemas[sourceTable]); current != "" && schemaFingerprint != current {
			return fmt.Errorf("CDC snapshot for %s has source schema fingerprint %q, want %q", sourceTable, schemaFingerprint, current)
		}
		position = encodeCDCStatePositionWithSchema(position, incarnation, schemaFingerprint, m.snapshotEpochs[sourceTable])
		events = append(events, m.newStateWriteEvent(sourceTable, destTable, cdcStateKindSnapshot, m.generation, destination.CDCStateStatusComplete, position))
		if destinationIncarnation := capturedDestinations[sourceTable]; destinationIncarnation != "" {
			destinationPosition := encodeCDCDestinationState(positions[sourceTable], destinationIncarnation, m.snapshotEpochs[sourceTable])
			events = append(events, m.newStateWriteEvent(sourceTable, destTable, cdcStateKindDestination, m.generation, destination.CDCStateStatusComplete, destinationPosition))
		}
	}
	if err := m.writeStateEvents(ctx, events); err != nil {
		return err
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	for sourceTable, expected := range capturedDestinations {
		current, exists, err := m.currentDestinationIncarnation(ctx, sourceTable)
		if err != nil {
			return err
		}
		if !exists || current != expected {
			return fmt.Errorf("CDC destination table %q changed while its state was being persisted", m.destTables[sourceTable])
		}
		m.knownDestinations[sourceTable] = expected
	}
	m.pruneSuperseded(ctx)
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	m.batchSnapshots = make(map[string]string)
	return nil
}

func (m *CDCStateManager) RecordBatchSnapshotCompletion(sourceTable, position string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.batchSnapshots == nil {
		m.batchSnapshots = make(map[string]string)
	}
	m.batchSnapshots[sourceTable] = position
}

func applyMultiTableSnapshotInvalidations(
	ctx context.Context,
	job *MultiTableIngestionJob,
	records <-chan source.RecordBatchResult,
) <-chan source.RecordBatchResult {
	out := make(chan source.RecordBatchResult)
	drainTimeout := canceledSourceDrainTimeout
	go func() {
		defer close(out)
		knownTables := make(map[string]struct{}, len(job.Tables))
		for _, table := range job.Tables {
			knownTables[table.Name] = struct{}{}
		}
		pending := make(map[string]source.CDCSnapshotInvalidation)
		fail := func(result source.RecordBatchResult, err error) {
			if result.Batch != nil {
				result.Batch.Release()
			}
			select {
			case out <- source.RecordBatchResult{Err: err}:
			case <-ctx.Done():
			}
			<-startBoundedRecordDrain(records, drainTimeout)
		}
		for {
			select {
			case <-ctx.Done():
				<-startBoundedRecordDrain(records, drainTimeout)
				return
			case result, ok := <-records:
				if !ok {
					if len(pending) > 0 {
						tables := make([]string, 0, len(pending))
						for table := range pending {
							tables = append(tables, table)
						}
						sort.Strings(tables)
						fail(source.RecordBatchResult{}, fmt.Errorf("source snapshot invalidation for %s was not followed by a replacement boundary", tables[0]))
					}
					return
				}
				if result.Err != nil {
					fail(result, result.Err)
					return
				}
				if result.SnapshotInvalidation != nil {
					if result.Batch != nil {
						result.Batch.Release()
					}
					invalidation := *result.SnapshotInvalidation
					if _, known := knownTables[invalidation.TableName]; !known {
						fail(source.RecordBatchResult{}, fmt.Errorf("source requested snapshot invalidation for unknown table %q", invalidation.TableName))
						return
					}
					if _, exists := pending[invalidation.TableName]; exists {
						fail(source.RecordBatchResult{}, fmt.Errorf("source repeated snapshot invalidation for %s before its replacement boundary", invalidation.TableName))
						return
					}
					pending[invalidation.TableName] = invalidation
					continue
				}
				if invalidation, waiting := pending[result.TableName]; waiting {
					if !result.Truncate || result.CDCWALTruncate {
						if result.Batch == nil && result.TableInfo != nil {
							merged, err := mergeSnapshotInvalidationMetadata(invalidation, result.TableInfo)
							if err != nil {
								fail(result, err)
								return
							}
							pending[result.TableName] = merged
							select {
							case out <- result:
							case <-ctx.Done():
								<-startBoundedRecordDrain(records, drainTimeout)
								return
							}
							continue
						}
						fail(result, fmt.Errorf("source snapshot invalidation for %s was not followed by a replacement boundary", result.TableName))
						return
					}
					if job.CDCStateManager == nil {
						fail(result, fmt.Errorf("source requested snapshot invalidation without destination-managed CDC state"))
						return
					}
					if err := job.CDCStateManager.InvalidateSnapshotStatePreservingDestination(
						ctx, invalidation.TableName, job.GetDestTableName(invalidation.TableName),
						invalidation.Incarnation, invalidation.SchemaFingerprint,
					); err != nil {
						fail(result, err)
						return
					}
					delete(pending, result.TableName)
				}
				select {
				case out <- result:
				case <-ctx.Done():
					if result.Batch != nil {
						result.Batch.Release()
					}
					<-startBoundedRecordDrain(records, drainTimeout)
					return
				}
			}
		}
	}()
	return out
}

func mergeSnapshotInvalidationMetadata(
	invalidation source.CDCSnapshotInvalidation,
	table *source.SourceTableInfo,
) (source.CDCSnapshotInvalidation, error) {
	if table == nil {
		return invalidation, nil
	}
	if table.Name != "" && table.Name != invalidation.TableName {
		return invalidation, fmt.Errorf(
			"source snapshot invalidation for %s was followed by metadata for %s",
			invalidation.TableName, table.Name,
		)
	}
	if invalidation.Incarnation != "" && table.Incarnation != "" && invalidation.Incarnation != table.Incarnation {
		return invalidation, fmt.Errorf("source snapshot invalidation for %s has conflicting incarnation metadata", invalidation.TableName)
	}
	if invalidation.SchemaFingerprint != "" && table.SchemaFingerprint != "" && invalidation.SchemaFingerprint != table.SchemaFingerprint {
		return invalidation, fmt.Errorf("source snapshot invalidation for %s has conflicting schema metadata", invalidation.TableName)
	}
	if invalidation.Incarnation == "" {
		invalidation.Incarnation = table.Incarnation
	}
	if invalidation.SchemaFingerprint == "" {
		invalidation.SchemaFingerprint = table.SchemaFingerprint
	}
	return invalidation, nil
}

func cloneStringMap(values map[string]string) map[string]string {
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func (m *CDCStateManager) prepareTable(ctx context.Context) error {
	if m.prepared {
		return nil
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if err := m.dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: m.stateTable, Schema: cdcStateSchema, DropFirst: false, PrimaryKeys: []string{"connector_id", "event_id"},
	}); err != nil {
		return fmt.Errorf("failed to prepare shared CDC state table %s: %w", m.stateTable, err)
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	m.prepared = true
	return nil
}

func (m *CDCStateManager) load(ctx context.Context) error {
	if m.loaded {
		return nil
	}
	entries, err := m.reader.LoadCDCState(ctx, m.stateTable, m.connectorID)
	if err != nil {
		return fmt.Errorf("failed to load shared CDC state: %w", err)
	}
	m.entries = append(m.entries[:0], entries...)
	for _, entry := range entries {
		if entry.Generation > m.generation {
			m.generation = entry.Generation
		}
	}
	for _, entry := range entries {
		if entry.Generation != m.generation {
			continue
		}
		runID, ok := cdcStateRunID(entry.EventID, m.connectorID)
		if !ok {
			continue
		}
		m.runs[runID] = struct{}{}
		key := cdcStateKey{runID: runID, sourceTable: entry.SourceTable, kind: entry.StateKind}
		state := m.states[key]
		if entry.Generation > state.generation {
			state = reducedCDCState{generation: entry.Generation}
		}
		if entry.Generation == state.generation {
			position, incarnation, epoch, valid := decodeCDCStateEntry(entry)
			if !valid {
				m.states[key] = state
				continue
			}
			state = reduceCDCStateEntry(state, entry, position, incarnation, epoch)
		}
		m.states[key] = state
	}
	m.loaded = true
	return nil
}

func newCDCStateRunID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("failed to generate CDC state run ID: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

func onlyCDCStateRun(runs map[string]struct{}) string {
	for runID := range runs {
		return runID
	}
	return ""
}

func cdcStateRunID(eventID, connectorID string) (string, bool) {
	remainder := strings.TrimPrefix(eventID, connectorID+"-")
	separator := strings.IndexByte(remainder, '-')
	if remainder == eventID || separator != 32 {
		return "", false
	}
	runID := remainder[:separator]
	if _, err := hex.DecodeString(runID); err != nil {
		return "", false
	}
	return runID, true
}

func (m *CDCStateManager) newStateWriteEvent(sourceTable, destTable, kind string, generation int64, status, position string) cdcStateWriteEvent {
	return m.newStateWriteEventForRun(m.runID, sourceTable, destTable, kind, generation, status, position)
}

func (m *CDCStateManager) newStateWriteEventForRun(runID, sourceTable, destTable, kind string, generation int64, status, position string) cdcStateWriteEvent {
	now := time.Now().UTC()
	eventSeed := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%d\x00%s\x00%s\x00%d", m.connectorID, runID, sourceTable, kind, generation, status, position, now.UnixNano())
	eventID := fmt.Sprintf("%s-%s-%x", m.connectorID, runID, sha256.Sum256([]byte(eventSeed)))
	return cdcStateWriteEvent{
		entry: destination.CDCStateEntry{
			EventID: eventID, SourceTable: sourceTable, DestinationTable: destTable,
			StateKind: kind, Generation: generation, Status: status, Position: position,
		},
		recordedAt: now,
	}
}

func (m *CDCStateManager) writeState(ctx context.Context, sourceTable, destTable, kind string, generation int64, status, position string) error {
	return m.writeStateEvents(ctx, []cdcStateWriteEvent{m.newStateWriteEvent(sourceTable, destTable, kind, generation, status, position)})
}

func (m *CDCStateManager) writeStateEvents(ctx context.Context, events []cdcStateWriteEvent) error {
	for start := 0; start < len(events); start += cdcStateWriteBatchSize {
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		end := min(start+cdcStateWriteBatchSize, len(events))
		batchEvents := events[start:end]
		if err := m.writeStateBatch(ctx, batchEvents); err != nil {
			return err
		}
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		for _, event := range batchEvents {
			m.applyWrittenState(event.entry)
		}
	}
	return nil
}

func (m *CDCStateManager) writeStateBatch(ctx context.Context, events []cdcStateWriteEvent) error {
	if len(events) == 0 {
		return nil
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}

	builder := array.NewRecordBuilder(memory.DefaultAllocator, cdcStateSchema.ToArrowSchema())
	defer builder.Release()
	for _, event := range events {
		entry := event.entry
		builder.Field(0).(*array.StringBuilder).Append(entry.EventID)
		builder.Field(1).(*array.StringBuilder).Append(cdcStateVersion)
		builder.Field(2).(*array.StringBuilder).Append(m.connectorID)
		builder.Field(3).(*array.StringBuilder).Append(entry.SourceTable)
		builder.Field(4).(*array.StringBuilder).Append(entry.DestinationTable)
		builder.Field(5).(*array.StringBuilder).Append(entry.StateKind)
		builder.Field(6).(*array.Int64Builder).Append(entry.Generation)
		builder.Field(7).(*array.StringBuilder).Append(entry.Status)
		builder.Field(8).(*array.StringBuilder).Append(entry.Position)
		builder.Field(9).(*array.TimestampBuilder).Append(arrow.Timestamp(event.recordedAt.UnixMicro()))
	}

	record := builder.NewRecordBatch()
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: record}
	close(records)
	writeOpts := destination.WriteOptions{Table: m.stateTable, Schema: cdcStateSchema, Parallelism: 1}
	var err error
	if writer, ok := m.dest.(destination.CDCStateWriter); ok {
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			record.Release()
			return err
		}
		err = writer.WriteCDCState(ctx, records, writeOpts)
	} else {
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			record.Release()
			return err
		}
		err = m.dest.WriteParallel(ctx, records, writeOpts)
	}
	if err != nil {
		return fmt.Errorf("failed to persist CDC state batch of %d events: %w", len(events), err)
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	return nil
}

func (m *CDCStateManager) applyWrittenState(entry destination.CDCStateEntry) {
	runID, ok := cdcStateRunID(entry.EventID, m.connectorID)
	if !ok {
		runID = m.runID
	}
	m.runs[runID] = struct{}{}
	key := cdcStateKey{runID: runID, sourceTable: entry.SourceTable, kind: entry.StateKind}
	state := m.states[key]
	if entry.Generation > state.generation {
		state = reducedCDCState{generation: entry.Generation}
	}
	if entry.Generation == state.generation {
		position, incarnation, epoch, valid := decodeCDCStateEntry(entry)
		if valid {
			state = reduceCDCStateEntry(state, entry, position, incarnation, epoch)
		}
	}
	m.states[key] = state
	m.entries = append(m.entries, entry)
	if entry.StateKind == cdcStateKindSnapshot && entry.Status == destination.CDCStateStatusComplete {
		delete(m.knownComplete, entry.SourceTable)
	}
}

func reduceCDCStateEntry(state reducedCDCState, entry destination.CDCStateEntry, position, incarnation string, epoch uint64) reducedCDCState {
	_, _, schemaFingerprint, _ := decodeCDCStatePositionWithSchema(entry.Position)
	if entry.StateKind == cdcStateKindSnapshot || entry.StateKind == cdcStateKindDestination {
		if epoch < state.snapshotEpoch {
			return state
		}
		if epoch > state.snapshotEpoch {
			state.complete = false
			state.position = ""
			state.incarnation = incarnation
			state.schemaFingerprint = schemaFingerprint
			state.destTable = entry.DestinationTable
			state.snapshotEpoch = epoch
		}
	}
	if entry.Status != destination.CDCStateStatusComplete {
		return state
	}
	state.complete = true
	state.destTable = entry.DestinationTable
	if state.position == "" || compareCDCPositions(position, state.position) > 0 ||
		(entry.StateKind == cdcStateKindDestination && compareCDCPositions(position, state.position) == 0) {
		state.position = position
		state.incarnation = incarnation
		state.schemaFingerprint = schemaFingerprint
	}
	return state
}

func (m *CDCStateManager) invalidateAfterFenceLoss(ctx context.Context, observed destination.CDCStateFence, ownershipErr error) error {
	floor := max(m.generation, observed.Generation)
	for attempt := 0; attempt < cdcStateInvalidationMax; attempt++ {
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return errors.Join(ownershipErr, err)
		}
		runID, err := newCDCStateRunID()
		if err != nil {
			return errors.Join(ownershipErr, fmt.Errorf("failed to create CDC recovery invalidation: %w", err))
		}
		generation := floor + 1
		event := m.newStateWriteEventForRun(runID, "", "", cdcStateKindRun, generation, cdcStateStatusInProgress, zeroCDCPosition)
		if err := m.writeStateBatch(ctx, []cdcStateWriteEvent{event}); err != nil {
			return errors.Join(ownershipErr, fmt.Errorf("failed to persist CDC recovery invalidation at generation %d: %w", generation, err))
		}
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return errors.Join(ownershipErr, err)
		}
		m.applyWrittenState(event.entry)

		fence, err := m.fenceReader.LoadCDCStateFence(ctx, m.stateTable, m.connectorID)
		if err != nil {
			return errors.Join(ownershipErr, fmt.Errorf("failed to verify CDC recovery invalidation at generation %d: %w", generation, err))
		}
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return errors.Join(ownershipErr, err)
		}
		logicalIDs := uniqueCDCStateEventIDs(fence.RunEventIDs)
		if fence.Generation == generation {
			if len(logicalIDs) > 1 || (len(logicalIDs) == 1 && logicalIDs[0] == event.entry.EventID) {
				return fmt.Errorf("%w; invalidated destination CDC state at generation %d", ownershipErr, generation)
			}
		}
		floor = max(generation, fence.Generation)
		if err := ctx.Err(); err != nil {
			return errors.Join(ownershipErr, fmt.Errorf("CDC recovery invalidation was superseded through generation %d: %w", floor, err))
		}
	}
	return errors.Join(ownershipErr, fmt.Errorf("CDC recovery invalidation could not become the latest fence after %d attempts (last generation %d)", cdcStateInvalidationMax, floor))
}

func uniqueCDCStateEventIDs(eventIDs []string) []string {
	seen := make(map[string]struct{}, len(eventIDs))
	unique := make([]string, 0, len(eventIDs))
	for _, eventID := range eventIDs {
		if _, ok := seen[eventID]; ok {
			continue
		}
		seen[eventID] = struct{}{}
		unique = append(unique, eventID)
	}
	sort.Strings(unique)
	return unique
}

func (m *CDCStateManager) pruneSuperseded(ctx context.Context) {
	if source.ConnectorLeaseLoss(ctx) != nil {
		return
	}
	stale := m.supersededEventIDs()
	if len(stale) == 0 {
		m.cleanupDue = false
		return
	}
	if !m.cleanupDue && len(stale) < cdcStatePruneThreshold {
		return
	}

	deleted := make(map[string]struct{}, len(stale))
	for len(stale) > 0 {
		if source.ConnectorLeaseLoss(ctx) != nil {
			break
		}
		if err := ctx.Err(); err != nil {
			config.Debug("[CDC STATE] Stopped pruning superseded events: %v", err)
			break
		}
		batchSize := min(len(stale), m.pruneBatchSize)
		pruneBatch := stale[:batchSize]
		if source.ConnectorLeaseLoss(ctx) != nil {
			break
		}
		if err := m.pruner.DeleteCDCStateEvents(ctx, m.stateTable, m.connectorID, pruneBatch); err != nil {
			config.Debug("[CDC STATE] Failed to prune %d superseded events: %v", len(pruneBatch), err)
			break
		}
		if source.ConnectorLeaseLoss(ctx) != nil {
			break
		}
		for _, eventID := range pruneBatch {
			deleted[eventID] = struct{}{}
		}
		stale = stale[batchSize:]
	}
	if len(deleted) > 0 {
		kept := m.entries[:0]
		for _, entry := range m.entries {
			if _, ok := deleted[entry.EventID]; !ok {
				kept = append(kept, entry)
			}
		}
		m.entries = kept
	}
	m.cleanupDue = len(stale) > 0
}

func (m *CDCStateManager) supersededEventIDs() []string {
	// A conflicting run is evidence that the generation cannot safely resume.
	// Preserve every event until a later uncontested run supersedes it.
	if len(m.runs) != 1 {
		return nil
	}
	if _, ok := m.runs[m.runID]; !ok {
		return nil
	}

	keep := make(map[cdcStateKey]destination.CDCStateEntry)
	for _, entry := range m.entries {
		if !cdcStateEntryPositionValid(entry) {
			continue
		}
		if entry.StateKind == cdcStateKindAtomicAttempt {
			continue
		}
		runID, ok := cdcStateRunID(entry.EventID, m.connectorID)
		if !ok || entry.Generation != m.generation || runID != m.runID {
			continue
		}
		key := cdcStateKey{runID: runID, sourceTable: entry.SourceTable, kind: entry.StateKind}
		current, exists := keep[key]
		if !exists || preferCDCStateEntry(entry, current) {
			keep[key] = entry
		}
	}

	keepIDs := make(map[string]struct{}, len(keep))
	for _, entry := range keep {
		keepIDs[entry.EventID] = struct{}{}
	}
	for eventID := range m.atomicSnapshotJournalKeepIDs() {
		keepIDs[eventID] = struct{}{}
	}
	stale := make([]string, 0, len(m.entries)-len(keepIDs))
	for _, entry := range m.entries {
		if _, ok := keepIDs[entry.EventID]; !ok && entry.EventID != "" && cdcStateEntryPositionValid(entry) {
			stale = append(stale, entry.EventID)
		}
	}
	return stale
}

func (m *CDCStateManager) atomicSnapshotJournalKeepIDs() map[string]struct{} {
	type candidate struct {
		entry   destination.CDCStateEntry
		attempt atomicSnapshotJournalEntry
	}
	candidates := make(map[string]candidate)
	for _, entry := range m.entries {
		if entry.StateKind != cdcStateKindAtomicAttempt || entry.EventID == "" {
			continue
		}
		attempt, valid := decodeAtomicSnapshotAttempt(entry.Position)
		if !valid {
			continue
		}
		current, exists := candidates[entry.SourceTable]
		if !exists || preferAtomicSnapshotJournalAnchor(entry, attempt, current.entry, current.attempt) {
			candidates[entry.SourceTable] = candidate{entry: entry, attempt: attempt}
		}
	}
	conflicted := make(map[string]bool)
	for _, entry := range m.entries {
		if entry.StateKind != cdcStateKindAtomicAttempt {
			continue
		}
		attempt, valid := decodeAtomicSnapshotAttempt(entry.Position)
		selected, exists := candidates[entry.SourceTable]
		if !valid || !exists || attempt.sequence != selected.attempt.sequence {
			continue
		}
		if atomicSnapshotJournalEntriesConflict(entry, attempt, selected.entry, selected.attempt) {
			conflicted[entry.SourceTable] = true
		}
	}
	keep := make(map[string]struct{}, len(candidates))
	for sourceTable, candidate := range candidates {
		if !conflicted[sourceTable] {
			keep[candidate.entry.EventID] = struct{}{}
		}
	}
	if len(conflicted) > 0 {
		for _, entry := range m.entries {
			if entry.StateKind == cdcStateKindAtomicAttempt && conflicted[entry.SourceTable] {
				keep[entry.EventID] = struct{}{}
			}
		}
	}
	return keep
}

func atomicSnapshotJournalEntriesConflict(
	left destination.CDCStateEntry,
	leftAttempt atomicSnapshotJournalEntry,
	right destination.CDCStateEntry,
	rightAttempt atomicSnapshotJournalEntry,
) bool {
	if leftAttempt.attemptID != rightAttempt.attemptID ||
		leftAttempt.snapshotEpoch != rightAttempt.snapshotEpoch ||
		left.DestinationTable != right.DestinationTable {
		return true
	}
	return leftAttempt.ready && rightAttempt.ready &&
		(leftAttempt.resumeLSN != rightAttempt.resumeLSN ||
			leftAttempt.expectedIncarnation != rightAttempt.expectedIncarnation ||
			leftAttempt.sourceMetadataKnown != rightAttempt.sourceMetadataKnown ||
			leftAttempt.sourceIncarnation != rightAttempt.sourceIncarnation ||
			leftAttempt.sourceSchemaFingerprint != rightAttempt.sourceSchemaFingerprint)
}

func preferAtomicSnapshotJournalAnchor(
	candidate destination.CDCStateEntry,
	candidateAttempt atomicSnapshotJournalEntry,
	current destination.CDCStateEntry,
	currentAttempt atomicSnapshotJournalEntry,
) bool {
	if candidateAttempt.sequence != currentAttempt.sequence {
		return candidateAttempt.sequence > currentAttempt.sequence
	}
	candidateTerminal := candidate.Status == destination.CDCStateStatusComplete
	currentTerminal := current.Status == destination.CDCStateStatusComplete
	if candidateTerminal != currentTerminal {
		return candidateTerminal
	}
	if candidateAttempt.ready != currentAttempt.ready {
		return candidateAttempt.ready
	}
	if candidate.Generation != current.Generation {
		return candidate.Generation > current.Generation
	}
	return candidate.EventID > current.EventID
}

func preferCDCStateEntry(candidate, current destination.CDCStateEntry) bool {
	if candidate.StateKind == current.StateKind &&
		(candidate.StateKind == cdcStateKindSnapshot ||
			candidate.StateKind == cdcStateKindDestination ||
			candidate.StateKind == cdcStateKindAtomicAttempt) {
		_, _, candidateEpoch, _ := decodeCDCStateEntry(candidate)
		_, _, currentEpoch, _ := decodeCDCStateEntry(current)
		if candidateEpoch != currentEpoch {
			return candidateEpoch > currentEpoch
		}
	}
	if candidate.Status != current.Status {
		return candidate.Status == destination.CDCStateStatusComplete
	}
	if candidate.Position != current.Position {
		candidatePosition, _, _, candidateValid := decodeCDCStateEntry(candidate)
		currentPosition, _, _, currentValid := decodeCDCStateEntry(current)
		if candidateValid && currentValid {
			if compared := compareCDCPositions(candidatePosition, currentPosition); compared != 0 {
				return compared > 0
			}
		}
	}
	return candidate.EventID > current.EventID
}

func cdcStatePositionValid(position string) bool {
	position, _, _ = decodeCDCStatePosition(position)
	_, err := pglogrepl.ParseLSN(position)
	return err == nil
}

func cdcStateEntryPositionValid(entry destination.CDCStateEntry) bool {
	_, _, _, valid := decodeCDCStateEntry(entry)
	return valid
}

func decodeCDCStateEntry(entry destination.CDCStateEntry) (position, incarnation string, epoch uint64, valid bool) {
	if entry.StateKind == cdcStateKindDestination {
		position, incarnation, epoch, valid = decodeCDCDestinationState(entry.Position)
		return position, incarnation, epoch, valid
	}
	if entry.StateKind == cdcStateKindAtomicAttempt {
		attempt, valid := decodeAtomicSnapshotAttempt(entry.Position)
		return zeroCDCPosition, attempt.attemptID, attempt.sequence, valid
	}
	position, incarnation, epoch = decodeCDCStatePosition(entry.Position)
	return position, incarnation, epoch, cdcStatePositionValid(entry.Position)
}

func compareCDCPositions(left, right string) int {
	left, _, _ = decodeCDCStatePosition(left)
	right, _, _ = decodeCDCStatePosition(right)
	leftLSN, leftErr := pglogrepl.ParseLSN(left)
	rightLSN, rightErr := pglogrepl.ParseLSN(right)
	if leftErr != nil || rightErr != nil {
		return 0
	}
	switch {
	case leftLSN < rightLSN:
		return -1
	case leftLSN > rightLSN:
		return 1
	default:
		return 0
	}
}

const (
	cdcStateIncarnationSeparator   = ";incarnation="
	cdcStateSchemaSeparator        = ";schema="
	cdcStateSnapshotEpochSeparator = ";epoch="
	cdcStateCompactIncarnation     = ";i="
	cdcStateCompactSchema          = ";s="
	cdcStateCompactEpoch           = ";e="
)

func encodeCDCStatePositionWithSchema(position, incarnation, schemaFingerprint string, snapshotEpoch uint64) string {
	if schemaFingerprint != "" {
		if oid, err := strconv.ParseUint(incarnation, 10, 64); err == nil {
			incarnation = strconv.FormatUint(oid, 36)
		}
		position += cdcStateCompactIncarnation + incarnation
		position += cdcStateCompactSchema + compactSchemaFingerprint(schemaFingerprint)
		if snapshotEpoch != 0 {
			position += cdcStateCompactEpoch + strconv.FormatUint(snapshotEpoch, 36)
		}
		return position
	}
	if incarnation != "" {
		position += cdcStateIncarnationSeparator + incarnation
	}
	if schemaFingerprint != "" {
		position += cdcStateSchemaSeparator + schemaFingerprint
	}
	if snapshotEpoch != 0 {
		position += cdcStateSnapshotEpochSeparator + strconv.FormatUint(snapshotEpoch, 16)
	}
	return position
}

func decodeCDCStatePosition(position string) (string, string, uint64) {
	lsn, incarnation, _, epoch := decodeCDCStatePositionWithSchema(position)
	return lsn, incarnation, epoch
}

func decodeCDCStatePositionWithSchema(position string) (string, string, string, uint64) {
	if strings.Contains(position, cdcStateCompactSchema) {
		withoutEpoch, encodedEpoch, hasEpoch := strings.Cut(position, cdcStateCompactEpoch)
		var epoch uint64
		if hasEpoch {
			parsed, err := strconv.ParseUint(encodedEpoch, 36, 64)
			if err != nil {
				return position, "", "", 0
			}
			epoch = parsed
		}
		withoutSchema, schemaFingerprint, hasSchema := strings.Cut(withoutEpoch, cdcStateCompactSchema)
		lsn, incarnation, hasIncarnation := strings.Cut(withoutSchema, cdcStateCompactIncarnation)
		if !hasSchema || !hasIncarnation {
			return position, "", "", 0
		}
		if oid, err := strconv.ParseUint(incarnation, 36, 64); err == nil {
			incarnation = strconv.FormatUint(oid, 10)
		}
		return lsn, incarnation, schemaFingerprint, epoch
	}
	withoutEpoch, encodedEpoch, hasEpoch := strings.Cut(position, cdcStateSnapshotEpochSeparator)
	var epoch uint64
	if hasEpoch {
		parsed, err := strconv.ParseUint(encodedEpoch, 16, 64)
		if err != nil {
			return position, "", "", 0
		}
		epoch = parsed
	}
	withoutSchema, schemaFingerprint, _ := strings.Cut(withoutEpoch, cdcStateSchemaSeparator)
	lsn, incarnation, _ := strings.Cut(withoutSchema, cdcStateIncarnationSeparator)
	return lsn, incarnation, schemaFingerprint, epoch
}

func compactSchemaFingerprint(fingerprint string) string {
	const encodedLength = 18
	if len(fingerprint) <= encodedLength {
		return fingerprint
	}
	return fingerprint[:encodedLength]
}

func cdcDestinationIncarnationDigest(incarnation string) string {
	sum := sha256.Sum256([]byte(incarnation))
	return hex.EncodeToString(sum[:10])
}

func encodeCDCDestinationState(position, incarnation string, snapshotEpoch uint64) string {
	return strconv.FormatUint(snapshotEpoch, 16) + ":" + incarnation + ":" + position
}

func decodeCDCDestinationState(encoded string) (position, incarnation string, snapshotEpoch uint64, valid bool) {
	parts := strings.SplitN(encoded, ":", 3)
	if len(parts) != 3 || len(parts[1]) != 20 {
		return "", "", 0, false
	}
	epoch, err := strconv.ParseUint(parts[0], 16, 64)
	if err != nil {
		return "", "", 0, false
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return "", "", 0, false
	}
	if _, err := pglogrepl.ParseLSN(parts[2]); err != nil {
		return "", "", 0, false
	}
	return parts[2], parts[1], epoch, true
}

func encodeAtomicSnapshotAttempt(attempt atomicSnapshotJournalEntry) string {
	ready := "0"
	if attempt.ready {
		ready = "1"
	}
	parts := []string{
		"a2",
		strconv.FormatUint(attempt.sequence, 16),
		strconv.FormatUint(attempt.snapshotEpoch, 16),
		attempt.attemptID,
		ready,
		attempt.resumeLSN,
		base64.RawURLEncoding.EncodeToString([]byte(attempt.expectedIncarnation)),
		base64.RawURLEncoding.EncodeToString([]byte(attempt.sourceIncarnation)),
		base64.RawURLEncoding.EncodeToString([]byte(attempt.sourceSchemaFingerprint)),
	}
	if attempt.ready && !attempt.sourceMetadataKnown {
		parts = parts[:7]
		parts[0] = "a1"
	}
	return strings.Join(parts, ":")
}

func decodeAtomicSnapshotAttempt(encoded string) (atomicSnapshotJournalEntry, bool) {
	parts := strings.Split(encoded, ":")
	legacy := len(parts) == 7 && parts[0] == "a1"
	current := len(parts) == 9 && parts[0] == "a2"
	if (!legacy && !current) || len(parts[3]) != 32 {
		return atomicSnapshotJournalEntry{}, false
	}
	sequence, err := strconv.ParseUint(parts[1], 16, 64)
	if err != nil || sequence == 0 {
		return atomicSnapshotJournalEntry{}, false
	}
	epoch, err := strconv.ParseUint(parts[2], 16, 64)
	if err != nil {
		return atomicSnapshotJournalEntry{}, false
	}
	if _, err := hex.DecodeString(parts[3]); err != nil {
		return atomicSnapshotJournalEntry{}, false
	}
	incarnation, err := base64.RawURLEncoding.DecodeString(parts[6])
	if err != nil {
		return atomicSnapshotJournalEntry{}, false
	}
	var sourceIncarnation, sourceSchemaFingerprint []byte
	if parts[0] == "a2" {
		sourceIncarnation, err = base64.RawURLEncoding.DecodeString(parts[7])
		if err != nil {
			return atomicSnapshotJournalEntry{}, false
		}
		sourceSchemaFingerprint, err = base64.RawURLEncoding.DecodeString(parts[8])
		if err != nil {
			return atomicSnapshotJournalEntry{}, false
		}
	}
	attempt := atomicSnapshotJournalEntry{
		attemptID:               parts[3],
		sequence:                sequence,
		snapshotEpoch:           epoch,
		ready:                   parts[4] == "1",
		resumeLSN:               parts[5],
		expectedIncarnation:     string(incarnation),
		sourceMetadataKnown:     parts[0] == "a2" && parts[4] == "1",
		sourceIncarnation:       string(sourceIncarnation),
		sourceSchemaFingerprint: string(sourceSchemaFingerprint),
	}
	if parts[4] != "0" && parts[4] != "1" {
		return atomicSnapshotJournalEntry{}, false
	}
	if attempt.ready && (!cdcStatePositionValid(attempt.resumeLSN) || attempt.expectedIncarnation == "") {
		return atomicSnapshotJournalEntry{}, false
	}
	if !attempt.ready && (attempt.resumeLSN != "" ||
		attempt.expectedIncarnation != "" ||
		attempt.sourceIncarnation != "" ||
		attempt.sourceSchemaFingerprint != "") {
		return atomicSnapshotJournalEntry{}, false
	}
	return attempt, true
}
