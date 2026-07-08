package postgres_cdc

import (
	"context"
	"fmt"
	"time"

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
	lastMessageAt time.Time
	started       bool
	streaming     bool
}

func NewReplicator(src *PostgresCDCSource, tableName string, tableSchema *schema.TableSchema, cdcConfig CDCConfig, startLSN pglogrepl.LSN, streaming bool) (*Replicator, error) {
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
		lastMessageAt: time.Now(),
		started:       false,
		streaming:     streaming,
	}, nil
}

// PendingLowWater reports the lowest LSN of an in-flight transaction whose
// changes have not yet been emitted. The single-table replicator emits each
// transaction's batch immediately, so only an undecoded (BEGIN-without-COMMIT)
// transaction can be pending.
func (r *Replicator) PendingLowWater() (pglogrepl.LSN, bool) {
	return r.decoder.InFlightTxLSN()
}

func (r *Replicator) Start(ctx context.Context) error {
	if r.started {
		return nil
	}

	config.Debug("[CDC] Starting replication from LSN: %s", r.startLSN)

	// The single-table replicator stays on protocol v1 (no in-progress
	// transaction streaming); binary tuples are still available when opted in.
	pluginArgs := buildPluginArgs(r.cdcConfig, r.source.serverVersion, false)

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

func (r *Replicator) standbyStatus() pglogrepl.StandbyStatusUpdate {
	var committed pglogrepl.LSN
	if r.streaming && r.source.pos != nil {
		committed = r.source.pos.Committed()
	}
	return standbyUpdate(r.streaming, r.clientXLogPos, committed, r.startLSN)
}

// NextChanges returns the next committed transaction's decoded changes, if
// any, with the LSN of the transaction that produced them. The hadActivity
// flag distinguishes a genuine idle period (receive timeout / nil message)
// from WAL activity that produced no changes yet (keepalives, buffered
// Begin/Insert/Update/Delete messages awaiting a Commit). Callers use it to
// avoid flushing the batch accumulator after every transaction.
func (r *Replicator) NextChanges(ctx context.Context) ([]Change, pglogrepl.LSN, bool, error) {
	// Start replication if not yet started
	if !r.started {
		if err := r.Start(ctx); err != nil {
			return nil, 0, false, err
		}
	}

	// Send standby status periodically. A send failure means the replication
	// connection is broken, so surface it rather than spinning on dead reads.
	if time.Since(r.standbyTimer) > 10*time.Second {
		status := r.standbyStatus()
		status.ReplyRequested = time.Since(r.lastMessageAt) > silenceProbeAfter
		err := pglogrepl.SendStandbyStatusUpdate(
			ctx,
			r.source.replConn,
			status,
		)
		if err != nil {
			return nil, 0, false, fmt.Errorf("failed to send standby status (replication connection lost): %w", err)
		}
		r.standbyTimer = time.Now()
	}

	// Bound a single receive so the loop can react to cancellation and flush
	// idle batches. See receiveTimeout for why this is not sub-second.
	ctxWithTimeout, cancel := context.WithTimeout(ctx, receiveTimeout)
	defer cancel()

	msg, err := r.source.replConn.ReceiveMessage(ctxWithTimeout)
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, false, ctx.Err()
		}
		// Timeout is expected when no data is available. But total silence for
		// longer than deadConnectionTimeout (no data and no keepalives) means a
		// dead or half-open connection that the per-call read timeout would mask forever.
		if ctxWithTimeout.Err() != nil {
			if time.Since(r.lastMessageAt) > deadConnectionTimeout {
				return nil, 0, false, fmt.Errorf("no message from server for %s; replication connection appears dead", deadConnectionTimeout)
			}
			return nil, 0, false, nil
		}
		return nil, 0, false, fmt.Errorf("failed to receive message: %w", err)
	}

	r.lastMessageAt = time.Now()

	if msg == nil {
		return nil, 0, false, nil
	}

	switch msg := msg.(type) {
	case *pgproto3.CopyData:
		if len(msg.Data) == 0 {
			return nil, 0, true, nil // Received a message, even if empty
		}

		switch msg.Data[0] {
		case pglogrepl.PrimaryKeepaliveMessageByteID:
			pkm, err := pglogrepl.ParsePrimaryKeepaliveMessage(msg.Data[1:])
			if err != nil {
				return nil, 0, true, fmt.Errorf("failed to parse keepalive: %w", err)
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
				return nil, 0, true, fmt.Errorf("failed to parse xlog data: %w", err)
			}

			changes, err := r.decoder.Decode(xld.WALData, xld.WALStart)
			if err != nil {
				return nil, 0, true, fmt.Errorf("failed to decode WAL data: %w", err)
			}

			if xld.WALStart > r.clientXLogPos {
				r.clientXLogPos = xld.WALStart
			}

			return changes, r.decoder.CurrentTxLSN(), true, nil
		}
	}

	return nil, 0, true, nil // Received some message type we don't handle
}
