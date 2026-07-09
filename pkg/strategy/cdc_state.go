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

const cdcStateVersion = "1"

var cdcStateSchema = &schema.TableSchema{Columns: []schema.Column{
	{Name: "state_version", DataType: schema.TypeString, Nullable: false},
	{Name: "connector_id", DataType: schema.TypeString, Nullable: false},
	{Name: "source_table", DataType: schema.TypeString, Nullable: false},
	{Name: "destination_table", DataType: schema.TypeString, Nullable: false},
	{Name: "state_kind", DataType: schema.TypeString, Nullable: false},
	{Name: "_cdc_lsn", DataType: schema.TypeString, Nullable: false},
	{Name: "recorded_at", DataType: schema.TypeTimestampTZ, Nullable: false},
}}

// CDCStateManager stores CDC progress in connector-scoped tables in the
// destination's managed staging namespace. Checkpoints are global to a
// connector; snapshot markers are separate per source table so an interrupted
// snapshot can never make a partially populated target resumable.
type CDCStateManager struct {
	dest           destination.Destination
	resumeProvider destination.CDCResumeProvider
	connectorID    string
	stagingDataset string
	anchorTable    string
	checkpoint     string

	mu           sync.Mutex
	destTables   map[string]string
	markerTables map[string]string
}

func NewCDCStateManager(dest destination.Destination, connectorID, anchorTable, stagingDataset string) (*CDCStateManager, error) {
	resumeProvider, ok := dest.(destination.CDCResumeProvider)
	if !ok {
		return nil, fmt.Errorf("destination scheme %q does not support destination-managed CDC state", dest.GetScheme())
	}
	if connectorID == "" {
		return nil, fmt.Errorf("CDC state connector ID is empty")
	}
	return &CDCStateManager{
		dest:           dest,
		resumeProvider: resumeProvider,
		connectorID:    connectorID,
		stagingDataset: stagingDataset,
		anchorTable:    anchorTable,
		checkpoint:     managedCDCStateTableName(dest, anchorTable, "cdc_checkpoint_"+connectorID, stagingDataset),
		destTables:     make(map[string]string),
		markerTables:   make(map[string]string),
	}, nil
}

func (m *CDCStateManager) RegisterTable(ctx context.Context, sourceTable, destTable string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.prepareTable(ctx, m.checkpoint); err != nil {
		return err
	}
	m.destTables[sourceTable] = destTable
	marker := managedCDCStateTableName(m.dest, destTable, "cdc_snapshot_"+m.connectorID+"_"+shortStateHash(sourceTable), m.stagingDataset)
	m.markerTables[sourceTable] = marker
	return m.prepareTable(ctx, marker)
}

func (m *CDCStateManager) Reset(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	tables := []string{m.checkpoint}
	for _, table := range m.markerTables {
		tables = append(tables, table)
	}
	for _, table := range tables {
		if err := m.dest.DropTable(ctx, table); err != nil {
			return fmt.Errorf("failed to reset CDC state table %s: %w", table, err)
		}
	}
	for _, table := range tables {
		if err := m.prepareTable(ctx, table); err != nil {
			return err
		}
	}
	return nil
}

// ResumePosition returns no position until the table has a durable snapshot
// marker. Once marked, the later of the snapshot boundary and global connector
// checkpoint is safe to resume from.
func (m *CDCStateManager) ResumePosition(ctx context.Context, sourceTable string) (string, error) {
	m.mu.Lock()
	marker := m.markerTables[sourceTable]
	m.mu.Unlock()
	if marker == "" {
		return "", fmt.Errorf("CDC state table is not registered for source table %q", sourceTable)
	}

	snapshotPosition, err := m.resumeProvider.GetMaxCDCLSN(ctx, marker)
	if err != nil {
		return "", fmt.Errorf("failed to read CDC snapshot state for %s: %w", sourceTable, err)
	}
	if snapshotPosition == "" {
		return "", nil
	}
	checkpoint, err := m.resumeProvider.GetMaxCDCLSN(ctx, m.checkpoint)
	if err != nil {
		return "", fmt.Errorf("failed to read CDC checkpoint: %w", err)
	}
	if checkpoint > snapshotPosition {
		return checkpoint, nil
	}
	return snapshotPosition, nil
}

func (m *CDCStateManager) Persist(ctx context.Context, token source.CDCStateCommitToken) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if token.Position != "" {
		if err := m.writeState(ctx, m.checkpoint, "", "", "checkpoint", token.Position); err != nil {
			return err
		}
	}

	tables := make([]string, 0, len(token.SnapshotPositions))
	for table := range token.SnapshotPositions {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	for _, sourceTable := range tables {
		position := token.SnapshotPositions[sourceTable]
		marker := m.markerTables[sourceTable]
		if marker == "" {
			return fmt.Errorf("CDC state table is not registered for source table %q", sourceTable)
		}
		if err := m.writeState(ctx, marker, sourceTable, m.destTables[sourceTable], "snapshot", position); err != nil {
			return err
		}
	}
	return nil
}

func (m *CDCStateManager) prepareTable(ctx context.Context, table string) error {
	if err := m.dest.PrepareTable(ctx, destination.PrepareOptions{Table: table, Schema: cdcStateSchema, DropFirst: false}); err != nil {
		return fmt.Errorf("failed to prepare CDC state table %s: %w", table, err)
	}
	return nil
}

func (m *CDCStateManager) writeState(ctx context.Context, table, sourceTable, destTable, kind, position string) error {
	builder := array.NewRecordBuilder(memory.DefaultAllocator, cdcStateSchema.ToArrowSchema())
	defer builder.Release()
	builder.Field(0).(*array.StringBuilder).Append(cdcStateVersion)
	builder.Field(1).(*array.StringBuilder).Append(m.connectorID)
	builder.Field(2).(*array.StringBuilder).Append(sourceTable)
	builder.Field(3).(*array.StringBuilder).Append(destTable)
	builder.Field(4).(*array.StringBuilder).Append(kind)
	builder.Field(5).(*array.StringBuilder).Append(position)
	builder.Field(6).(*array.TimestampBuilder).Append(arrow.Timestamp(time.Now().UTC().UnixMicro()))

	record := builder.NewRecordBatch()
	records := make(chan source.RecordBatchResult, 1)
	records <- source.RecordBatchResult{Batch: record}
	close(records)
	if err := m.dest.WriteParallel(ctx, records, destination.WriteOptions{Table: table, Schema: cdcStateSchema, Parallelism: 1}); err != nil {
		return fmt.Errorf("failed to persist CDC %s state at %s: %w", kind, position, err)
	}
	return nil
}

func shortStateHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", sum[:6])
}
