package postgres_cdc

import (
	"context"
	"fmt"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgproto3"
)

// LSNFilter provides per-table LSN filtering for multi-table CDC.
type LSNFilter interface {
	ShouldFilterChange(tableName string, changeLSN pglogrepl.LSN) bool
	GetProcessedLSN(tableName string) pglogrepl.LSN
}

// LSNUpdater updates the in-memory processed LSN tracking.
type LSNUpdater interface {
	LSNFilter
	updateProcessedLSN(tableName string, lsn pglogrepl.LSN)
}

// MultiTableReplicator streams WAL changes for multiple tables.
type MultiTableReplicator struct {
	source        *PostgresCDCSource
	tables        []source.SourceTableInfo
	cdcConfig     CDCConfig
	startLSN      pglogrepl.LSN
	decoder       *MultiTableDecoder
	lsnFilter     LSNUpdater
	clientXLogPos pglogrepl.LSN
	standbyTimer  time.Time
	started       bool

	// Buffered batches from a single transaction (may contain multiple tables)
	pendingBatches []DecodedBatch
}

func NewMultiTableReplicator(src *PostgresCDCSource, tables []source.SourceTableInfo, cdcConfig CDCConfig, startLSN pglogrepl.LSN, lsnFilter LSNUpdater) (*MultiTableReplicator, error) {
	decoder := NewMultiTableDecoder(tables)

	return &MultiTableReplicator{
		source:        src,
		tables:        tables,
		cdcConfig:     cdcConfig,
		startLSN:      startLSN,
		decoder:       decoder,
		lsnFilter:     lsnFilter,
		clientXLogPos: startLSN,
		standbyTimer:  time.Now(),
		started:       false,
	}, nil
}

func (r *MultiTableReplicator) Start(ctx context.Context) error {
	if r.started {
		return nil
	}

	config.Debug("[CDC] Starting multi-table replication from LSN: %s", r.startLSN)

	pluginArgs := []string{
		"proto_version '1'",
		fmt.Sprintf("publication_names '%s'", r.cdcConfig.Publication),
	}

	config.Debug("[CDC] Starting replication for slot %s from LSN %s", r.cdcConfig.SlotName, r.startLSN)

	err := pglogrepl.StartReplication(
		ctx,
		r.source.replConn,
		r.cdcConfig.SlotName,
		r.startLSN,
		pglogrepl.StartReplicationOptions{
			Mode:       pglogrepl.LogicalReplication,
			PluginArgs: pluginArgs,
		},
	)
	if err != nil {
		return fmt.Errorf("failed to start replication: %w", err)
	}

	r.started = true
	config.Debug("[CDC] Multi-table replication started successfully")
	return nil
}

func (r *MultiTableReplicator) Close(ctx context.Context) error {
	return nil
}

func (r *MultiTableReplicator) CurrentLSN() pglogrepl.LSN {
	return r.clientXLogPos
}

// NextBatch returns the next batch, its source table name, and a flag indicating WAL activity.
// Returns (nil, "", false, nil) when no data is available.
// Returns (nil, "", true, nil) when WAL data was received but no complete batch yet (e.g., buffering transaction).
func (r *MultiTableReplicator) NextBatch(ctx context.Context, batchSize int) (arrow.RecordBatch, string, bool, error) {
	// Return buffered batches first
	if len(r.pendingBatches) > 0 {
		batch := r.pendingBatches[0]
		r.pendingBatches = r.pendingBatches[1:]

		// Update processed LSN for this table
		if r.lsnFilter != nil {
			r.lsnFilter.updateProcessedLSN(batch.TableName, batch.LSN)
		}

		return batch.Batch, batch.TableName, true, nil
	}

	// Start replication if not yet started
	if !r.started {
		if err := r.Start(ctx); err != nil {
			return nil, "", false, err
		}
	}

	// Send standby status periodically
	if time.Since(r.standbyTimer) > 10*time.Second {
		err := pglogrepl.SendStandbyStatusUpdate(
			ctx,
			r.source.replConn,
			pglogrepl.StandbyStatusUpdate{
				WALWritePosition: r.clientXLogPos,
			},
		)
		if err != nil {
			config.Debug("[CDC] Failed to send standby status: %v", err)
		}
		r.standbyTimer = time.Now()
	}

	// Set a short deadline for receiving messages
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	msg, err := r.source.replConn.ReceiveMessage(ctxWithTimeout)
	if err != nil {
		if ctx.Err() != nil {
			return nil, "", false, ctx.Err()
		}
		// Timeout is expected when no data is available
		if ctxWithTimeout.Err() != nil {
			return nil, "", false, nil
		}
		return nil, "", false, fmt.Errorf("failed to receive message: %w", err)
	}

	if msg == nil {
		return nil, "", false, nil
	}

	switch msg := msg.(type) {
	case *pgproto3.CopyData:
		if len(msg.Data) == 0 {
			return nil, "", true, nil // Received a message, even if empty
		}

		switch msg.Data[0] {
		case pglogrepl.PrimaryKeepaliveMessageByteID:
			pkm, err := pglogrepl.ParsePrimaryKeepaliveMessage(msg.Data[1:])
			if err != nil {
				return nil, "", true, fmt.Errorf("failed to parse keepalive: %w", err)
			}

			if pkm.ReplyRequested {
				r.standbyTimer = time.Time{} // Force status update on next iteration
			}

			if pkm.ServerWALEnd > r.clientXLogPos {
				r.clientXLogPos = pkm.ServerWALEnd
			}

		case pglogrepl.XLogDataByteID:
			xld, err := pglogrepl.ParseXLogData(msg.Data[1:])
			if err != nil {
				return nil, "", true, fmt.Errorf("failed to parse xlog data: %w", err)
			}

			config.Debug("[CDC] Received XLogData at LSN %s, data len=%d, first byte=%x", xld.WALStart, len(xld.WALData), xld.WALData[0])

			batches, err := r.decoder.Decode(xld.WALData, xld.WALStart)
			if err != nil {
				return nil, "", true, fmt.Errorf("failed to decode WAL data: %w", err)
			}

			if len(batches) > 0 {
				config.Debug("[CDC] Decoded %d batches from commit", len(batches))
			}

			if xld.WALStart > r.clientXLogPos {
				r.clientXLogPos = xld.WALStart
			}

			// Filter batches based on per-table LSN
			var filteredBatches []DecodedBatch
			for _, batch := range batches {
				if r.lsnFilter != nil && r.lsnFilter.ShouldFilterChange(batch.TableName, batch.LSN) {
					config.Debug("[CDC] Filtering batch for %s at LSN %s (already processed)", batch.TableName, batch.LSN)
					// Release the batch since we're not using it
					if batch.Batch != nil {
						batch.Batch.Release()
					}
					continue
				}
				filteredBatches = append(filteredBatches, batch)
			}

			if len(filteredBatches) == 0 {
				return nil, "", true, nil // WAL data received but no new batches (filtered or buffered)
			}

			// Return first batch, buffer the rest
			first := filteredBatches[0]
			if len(filteredBatches) > 1 {
				r.pendingBatches = append(r.pendingBatches, filteredBatches[1:]...)
			}

			// Update processed LSN for this table
			if r.lsnFilter != nil {
				r.lsnFilter.updateProcessedLSN(first.TableName, first.LSN)
			}

			return first.Batch, first.TableName, true, nil
		}
	}

	return nil, "", true, nil // Received some message type we don't handle
}
