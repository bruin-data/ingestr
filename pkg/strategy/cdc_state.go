package strategy

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	cdcStateVersion          = "1"
	cdcStateTableName        = "cdc_state"
	cdcStateKindCheckpoint   = "checkpoint"
	cdcStateKindSnapshot     = "snapshot"
	cdcStateStatusInProgress = "in_progress"
	zeroCDCPosition          = "00000000/00000000"
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
	{Name: "_cdc_lsn", DataType: schema.TypeString, MaxLength: 64, Nullable: false},
	{Name: "recorded_at", DataType: schema.TypeTimestampTZ, Nullable: false},
}}

type cdcStateKey struct {
	sourceTable string
	kind        string
}

type reducedCDCState struct {
	generation int64
	position   string
	complete   bool
}

// CDCStateManager stores every connector and source-table marker in one shared
// destination table. Run generations make append-only in-progress events
// invalidate older completions without deleting another connector's state.
type CDCStateManager struct {
	dest        destination.Destination
	reader      destination.CDCStateReader
	connectorID string
	stateTable  string

	mu            sync.Mutex
	prepared      bool
	loaded        bool
	started       bool
	generation    int64
	states        map[cdcStateKey]reducedCDCState
	destTables    map[string]string
	knownComplete map[string]string
}

func NewCDCStateManager(dest destination.Destination, connectorID, anchorTable, stagingDataset string) (*CDCStateManager, error) {
	reader, ok := dest.(destination.CDCStateReader)
	if !ok {
		return nil, fmt.Errorf("destination scheme %q does not support destination-managed CDC state", dest.GetScheme())
	}
	if connectorID == "" {
		return nil, fmt.Errorf("CDC state connector ID is empty")
	}
	return &CDCStateManager{
		dest:          dest,
		reader:        reader,
		connectorID:   connectorID,
		stateTable:    managedCDCStateTableName(dest, anchorTable, cdcStateTableName, stagingDataset),
		states:        make(map[cdcStateKey]reducedCDCState),
		destTables:    make(map[string]string),
		knownComplete: make(map[string]string),
	}, nil
}

func (m *CDCStateManager) RegisterTable(ctx context.Context, sourceTable, destTable string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.prepareTable(ctx); err != nil {
		return err
	}
	if _, exists := m.destTables[sourceTable]; exists {
		m.destTables[sourceTable] = destTable
		return nil
	}
	m.destTables[sourceTable] = destTable
	if m.started {
		return m.writeState(ctx, sourceTable, destTable, cdcStateKindSnapshot, m.generation, cdcStateStatusInProgress, zeroCDCPosition)
	}
	return nil
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

	snapshot := m.states[cdcStateKey{sourceTable: sourceTable, kind: cdcStateKindSnapshot}]
	if !snapshot.complete {
		return "", nil
	}
	m.knownComplete[sourceTable] = snapshot.position

	checkpoint := m.states[cdcStateKey{kind: cdcStateKindCheckpoint}]
	if checkpoint.complete && checkpoint.position > snapshot.position {
		return checkpoint.position, nil
	}
	return snapshot.position, nil
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
	if err := m.load(ctx); err != nil {
		return err
	}
	if fullRefresh {
		m.knownComplete = make(map[string]string)
	}
	m.generation++
	m.started = true

	tables := make([]string, 0, len(m.destTables))
	for table := range m.destTables {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	for _, sourceTable := range tables {
		if err := m.writeState(ctx, sourceTable, m.destTables[sourceTable], cdcStateKindSnapshot, m.generation, cdcStateStatusInProgress, zeroCDCPosition); err != nil {
			return err
		}
	}
	return nil
}

func (m *CDCStateManager) Persist(ctx context.Context, token source.CDCStateCommitToken) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started || m.generation == 0 {
		return fmt.Errorf("CDC state run has not started")
	}
	if token.Position != "" {
		if err := m.writeState(ctx, "", "", cdcStateKindCheckpoint, m.generation, destination.CDCStateStatusComplete, token.Position); err != nil {
			return err
		}
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
	for _, sourceTable := range tables {
		destTable, ok := m.destTables[sourceTable]
		if !ok {
			return fmt.Errorf("CDC state is not registered for source table %q", sourceTable)
		}
		position := positions[sourceTable]
		if err := m.writeState(ctx, sourceTable, destTable, cdcStateKindSnapshot, m.generation, destination.CDCStateStatusComplete, position); err != nil {
			return err
		}
		delete(m.knownComplete, sourceTable)
	}
	return nil
}

func (m *CDCStateManager) prepareTable(ctx context.Context) error {
	if m.prepared {
		return nil
	}
	if err := m.dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: m.stateTable, Schema: cdcStateSchema, DropFirst: false, PrimaryKeys: []string{"connector_id", "event_id"},
	}); err != nil {
		return fmt.Errorf("failed to prepare shared CDC state table %s: %w", m.stateTable, err)
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
	for _, entry := range entries {
		if entry.Generation > m.generation {
			m.generation = entry.Generation
		}
		key := cdcStateKey{sourceTable: entry.SourceTable, kind: entry.StateKind}
		state := m.states[key]
		if entry.Generation > state.generation {
			state = reducedCDCState{generation: entry.Generation}
		}
		if entry.Generation == state.generation && entry.Status == destination.CDCStateStatusComplete {
			state.complete = true
			if entry.Position > state.position {
				state.position = entry.Position
			}
		}
		m.states[key] = state
	}
	m.loaded = true
	return nil
}

func (m *CDCStateManager) writeState(ctx context.Context, sourceTable, destTable, kind string, generation int64, status, position string) error {
	now := time.Now().UTC()
	eventSeed := fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%s\x00%s\x00%d", m.connectorID, sourceTable, kind, generation, status, position, now.UnixNano())
	eventID := fmt.Sprintf("%s-%x", m.connectorID, sha256.Sum256([]byte(eventSeed)))

	builder := array.NewRecordBuilder(memory.DefaultAllocator, cdcStateSchema.ToArrowSchema())
	defer builder.Release()
	builder.Field(0).(*array.StringBuilder).Append(eventID)
	builder.Field(1).(*array.StringBuilder).Append(cdcStateVersion)
	builder.Field(2).(*array.StringBuilder).Append(m.connectorID)
	builder.Field(3).(*array.StringBuilder).Append(sourceTable)
	builder.Field(4).(*array.StringBuilder).Append(destTable)
	builder.Field(5).(*array.StringBuilder).Append(kind)
	builder.Field(6).(*array.Int64Builder).Append(generation)
	builder.Field(7).(*array.StringBuilder).Append(status)
	builder.Field(8).(*array.StringBuilder).Append(position)
	builder.Field(9).(*array.TimestampBuilder).Append(arrow.Timestamp(now.UnixMicro()))

	record := builder.NewRecordBatch()
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: record}
	close(records)
	if err := m.dest.WriteParallel(ctx, records, destination.WriteOptions{Table: m.stateTable, Schema: cdcStateSchema, Parallelism: 1}); err != nil {
		return fmt.Errorf("failed to persist CDC %s state at %s: %w", kind, position, err)
	}
	key := cdcStateKey{sourceTable: sourceTable, kind: kind}
	state := m.states[key]
	if generation > state.generation {
		state = reducedCDCState{generation: generation}
	}
	if generation == state.generation && status == destination.CDCStateStatusComplete {
		state.complete = true
		if position > state.position {
			state.position = position
		}
	}
	m.states[key] = state
	return nil
}
