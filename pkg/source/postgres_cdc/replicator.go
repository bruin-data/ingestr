package postgres_cdc

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgproto3"
)

type Replicator struct {
	source                    *PostgresCDCSource
	tableName                 string
	tableSchema               *schema.TableSchema
	cdcConfig                 CDCConfig
	startLSN                  pglogrepl.LSN
	decoder                   *Decoder
	clientXLogPos             pglogrepl.LSN
	barrierNonce              string
	barrierSeen               bool
	standbyTimer              time.Time
	lastMessageAt             time.Time
	started                   bool
	streaming                 bool
	expectedIncarnation       string
	expectedSchemaFingerprint string
	snapshotBoundary          bool
	lastIncarnationCheck      time.Time
	incarnationLookup         func(context.Context) (string, error)
	schemaFingerprintLookup   func(context.Context) (string, error)
}

func (r *Replicator) ExpectTableSchemaFingerprint(fingerprint string) {
	r.expectedSchemaFingerprint = fingerprint
}

func (r *Replicator) SetSnapshotBoundary(snapshotBoundary bool) {
	r.snapshotBoundary = snapshotBoundary
}

func (r *Replicator) ShouldFilterChange(changeLSN pglogrepl.LSN) bool {
	if changeLSN != r.startLSN {
		return changeLSN < r.startLSN
	}
	return !r.snapshotBoundary && len(r.tableSchema.PrimaryKeys) == 0
}

func (r *Replicator) ExpectTableIncarnation(incarnation string) error {
	oid, err := strconv.ParseUint(incarnation, 10, 32)
	if err != nil {
		return fmt.Errorf("invalid PostgreSQL table incarnation %q: %w", incarnation, err)
	}
	r.expectedIncarnation = incarnation
	r.decoder.ExpectRelationID(uint32(oid))
	return nil
}

func (r *Replicator) checkTableIncarnation(ctx context.Context) error {
	if !r.streaming || r.expectedIncarnation == "" || r.cdcConfig.DiscoverInterval <= 0 ||
		time.Since(r.lastIncarnationCheck) < r.cdcConfig.DiscoverInterval {
		return nil
	}
	r.lastIncarnationCheck = time.Now()
	current, err := r.incarnationLookup(ctx)
	if err != nil {
		config.Debug("[CDC] Table incarnation check failed for %s: %v", r.tableName, err)
		return nil
	}
	if current != r.expectedIncarnation {
		return &TableReincarnatedError{Table: r.tableName, Previous: r.expectedIncarnation, Current: current}
	}
	if r.expectedSchemaFingerprint != "" {
		fingerprint, err := r.schemaFingerprintLookup(ctx)
		if err != nil {
			config.Debug("[CDC] Table schema fingerprint check failed for %s: %v", r.tableName, err)
			return nil
		}
		if fingerprint != r.expectedSchemaFingerprint {
			return &SchemaChangedError{Table: r.tableName, Column: "*", Reason: "schema fingerprint changed"}
		}
	}
	return nil
}

func NewReplicator(src *PostgresCDCSource, tableName string, tableSchema *schema.TableSchema, cdcConfig CDCConfig, startLSN pglogrepl.LSN, streaming bool, barrierNonce string) (*Replicator, error) {
	schemaName, tblName := parseTableName(tableName)

	decoder := NewDecoder(tableSchema, schemaName, tblName)

	src.lag.streaming.Store(streaming)

	return &Replicator{
		source:        src,
		tableName:     tableName,
		tableSchema:   tableSchema,
		cdcConfig:     cdcConfig,
		startLSN:      startLSN,
		decoder:       decoder,
		clientXLogPos: startLSN,
		barrierNonce:  barrierNonce,
		standbyTimer:  time.Now(),
		lastMessageAt: time.Now(),
		started:       false,
		streaming:     streaming,
		incarnationLookup: func(ctx context.Context) (string, error) {
			return src.TableIncarnation(ctx, tableName)
		},
		schemaFingerprintLookup: func(ctx context.Context) (string, error) {
			return src.TableSchemaFingerprint(ctx, tableName)
		},
	}, nil
}

// PendingLowWater reports the LSN of an in-flight or committed-but-not-fully-
// drained transaction whose changes have not all reached the accumulator.
func (r *Replicator) PendingLowWater() (pglogrepl.LSN, bool) {
	if low, ok := r.decoder.CommittedLowWater(); ok {
		return low, true
	}
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
	return r.decoder.Close()
}

func (r *Replicator) CurrentLSN() pglogrepl.LSN {
	return r.clientXLogPos
}

func (r *Replicator) BarrierReached() bool {
	return r.barrierSeen
}

func (r *Replicator) EmitStreamHeartbeat(ctx context.Context) error {
	if r.source.serverVersion < 140000 {
		return nil
	}
	return emitStreamHeartbeat(ctx, r.source.queryPool)
}

func (r *Replicator) handleLogicalMessage(data []byte) (bool, error) {
	message, err := parseLogicalDecodingMessage(data, false, false)
	if err != nil || message == nil {
		return message != nil, err
	}
	if matchesBatchBarrier(message, r.barrierNonce) {
		r.barrierSeen = true
	}
	if matchesBatchBarrier(message, r.barrierNonce) || matchesStreamHeartbeat(message) {
		if message.LSN > r.clientXLogPos {
			r.clientXLogPos = message.LSN
		}
	}
	return true, nil
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
	if r.decoder.HasCommitted() {
		changes, err := r.decoder.DrainCommitted(defaultCommittedDrainChanges)
		return changes, r.decoder.CurrentTxLSN(), true, err
	}
	if err := r.checkTableIncarnation(ctx); err != nil {
		return nil, 0, false, err
	}
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
		err := sendStandbyStatusUpdate(
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

			r.source.lag.observe(pkm.ServerWALEnd)

		case pglogrepl.XLogDataByteID:
			xld, err := pglogrepl.ParseXLogData(msg.Data[1:])
			if err != nil {
				return nil, 0, true, fmt.Errorf("failed to parse xlog data: %w", err)
			}

			handledLogicalMessage, err := r.handleLogicalMessage(xld.WALData)
			if err != nil {
				return nil, 0, true, err
			}
			if handledLogicalMessage {
				r.source.lag.observe(xld.ServerWALEnd)
				return nil, 0, true, nil
			}

			changes, err := r.decoder.Decode(xld.WALData, xld.WALStart)
			if err != nil {
				return nil, 0, true, fmt.Errorf("failed to decode WAL data: %w", err)
			}

			processedLSN := xld.WALStart
			if commitLSN, ok := logicalCommitLSN(xld.WALData); ok && commitLSN > processedLSN {
				processedLSN = commitLSN
			}
			if processedLSN > r.clientXLogPos {
				r.clientXLogPos = processedLSN
			}
			// ServerWALEnd, not WALStart: during a long burst with no
			// interleaved keepalive it is the only fresh view of the head.
			r.source.lag.observe(xld.ServerWALEnd)

			return changes, r.decoder.CurrentTxLSN(), true, nil
		}
	}

	return nil, 0, true, nil // Received some message type we don't handle
}
