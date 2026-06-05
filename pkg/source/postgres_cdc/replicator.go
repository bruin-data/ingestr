package postgres_cdc

import (
	"context"
	"fmt"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgproto3"
)

type Replicator struct {
	source        *PostgresCDCSource
	tableName     string
	tableSchema   *schema.TableSchema
	cdcConfig     CDCConfig
	startLSN      pglogrepl.LSN
	decoder       *Decoder
	clientXLogPos pglogrepl.LSN
	standbyTimer  time.Time
	started       bool
}

func NewReplicator(src *PostgresCDCSource, tableName string, tableSchema *schema.TableSchema, cdcConfig CDCConfig, startLSN pglogrepl.LSN) (*Replicator, error) {
	schemaName, tblName := parseTableName(tableName)

	decoder := NewDecoder(tableSchema, schemaName, tblName)

	return &Replicator{
		source:        src,
		tableName:     tableName,
		tableSchema:   tableSchema,
		cdcConfig:     cdcConfig,
		startLSN:      startLSN,
		decoder:       decoder,
		clientXLogPos: startLSN,
		standbyTimer:  time.Now(),
		started:       false,
	}, nil
}

func (r *Replicator) Start(ctx context.Context) error {
	if r.started {
		return nil
	}

	config.Debug("[CDC] Starting replication from LSN: %s", r.startLSN)

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
	config.Debug("[CDC] Replication started successfully")
	return nil
}

func (r *Replicator) Close(ctx context.Context) error {
	return nil
}

func (r *Replicator) CurrentLSN() pglogrepl.LSN {
	return r.clientXLogPos
}

// NextBatch returns the next decoded batch, if any. The hadActivity flag
// distinguishes a genuine idle period (receive timeout / nil message) from
// WAL activity that produced no batch yet (keepalives, buffered Begin/Insert/
// Update/Delete messages awaiting a Commit). Callers use it to avoid flushing
// the batch accumulator after every transaction.
func (r *Replicator) NextBatch(ctx context.Context, batchSize int) (arrow.RecordBatch, bool, error) {
	// Start replication if not yet started
	if !r.started {
		if err := r.Start(ctx); err != nil {
			return nil, false, err
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
			return nil, false, ctx.Err()
		}
		// Timeout is expected when no data is available
		if ctxWithTimeout.Err() != nil {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to receive message: %w", err)
	}

	if msg == nil {
		return nil, false, nil
	}

	switch msg := msg.(type) {
	case *pgproto3.CopyData:
		if len(msg.Data) == 0 {
			return nil, true, nil // Received a message, even if empty
		}

		switch msg.Data[0] {
		case pglogrepl.PrimaryKeepaliveMessageByteID:
			pkm, err := pglogrepl.ParsePrimaryKeepaliveMessage(msg.Data[1:])
			if err != nil {
				return nil, true, fmt.Errorf("failed to parse keepalive: %w", err)
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
				return nil, true, fmt.Errorf("failed to parse xlog data: %w", err)
			}

			batch, err := r.decoder.Decode(xld.WALData, xld.WALStart)
			if err != nil {
				return nil, true, fmt.Errorf("failed to decode WAL data: %w", err)
			}

			if xld.WALStart > r.clientXLogPos {
				r.clientXLogPos = xld.WALStart
			}

			return batch, true, nil
		}
	}

	return nil, true, nil // Received some message type we don't handle
}
