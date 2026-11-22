package postgres_cdc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	batchBarrierPrefix    = "ingestr_cdc_batch_barrier"
	streamHeartbeatPrefix = "ingestr_cdc_stream_heartbeat"

	maxExpectedBarrierLSNDifference = pglogrepl.LSN(16 * 1024 * 1024)
)

// batchBarrier marks where a batch run stops. On a primary it is a logical
// decoding message the run emits and then waits to decode. A standby cannot
// write WAL, so there the run stops once decoding passes the replay position
// observed at start (target).
type batchBarrier struct {
	nonce  string
	sqlLSN pglogrepl.LSN
	target pglogrepl.LSN
}

func (b batchBarrier) active() bool {
	return b.nonce != "" || b.target != 0
}

func (s *PostgresCDCSource) beginBatchBarrier(ctx context.Context) (batchBarrier, error) {
	if !s.standby {
		nonce, sqlLSN, err := emitBatchBarrier(ctx, s.queryPool)
		if err != nil {
			return batchBarrier{}, err
		}
		config.Debug("[CDC] Batch mode: emitted logical-decoding barrier; SQL returned %s", sqlLSN)
		return batchBarrier{nonce: nonce, sqlLSN: sqlLSN}, nil
	}
	var raw *string
	if err := s.queryPool.QueryRow(ctx, "SELECT pg_last_wal_replay_lsn()::text").Scan(&raw); err != nil {
		return batchBarrier{}, fmt.Errorf("failed to read standby replay position: %w", err)
	}
	if raw == nil {
		return batchBarrier{}, fmt.Errorf("PostgreSQL standby has not replayed any WAL yet")
	}
	target, err := pglogrepl.ParseLSN(*raw)
	if err != nil {
		return batchBarrier{}, fmt.Errorf("failed to parse standby replay position %q: %w", *raw, err)
	}
	config.Debug("[CDC] Batch mode: standby replay position %s is the batch target", target)
	return batchBarrier{target: target}, nil
}

func emitBatchBarrier(ctx context.Context, pool *pgxpool.Pool) (string, pglogrepl.LSN, error) {
	return emitLogicalMarker(ctx, pool, batchBarrierPrefix)
}

func emitStreamHeartbeat(ctx context.Context, pool *pgxpool.Pool) error {
	_, _, err := emitLogicalMarker(ctx, pool, streamHeartbeatPrefix)
	return err
}

func emitLogicalMarker(ctx context.Context, pool *pgxpool.Pool, prefix string) (string, pglogrepl.LSN, error) {
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", 0, fmt.Errorf("failed to generate CDC logical marker nonce: %w", err)
	}
	nonce := hex.EncodeToString(nonceBytes)

	var rawLSN string
	if err := pool.QueryRow(
		ctx,
		"SELECT pg_logical_emit_message(false, $1, $2)::text",
		prefix, nonce,
	).Scan(&rawLSN); err != nil {
		return "", 0, fmt.Errorf("failed to emit CDC logical marker: %w", err)
	}
	// This return value is diagnostic only. Aurora can return a volume/storage
	// LSN here that is incomparable with replication-protocol LSNs. See AWS,
	// "Migrating RDS for PostgreSQL to Aurora using seeded logical replication."
	lsn, err := pglogrepl.ParseLSN(rawLSN)
	if err != nil {
		return "", 0, fmt.Errorf("failed to parse CDC logical marker LSN %q: %w", rawLSN, err)
	}
	return nonce, lsn, nil
}

func warnIfBarrierLSNsDiverge(sqlResultLSN, decodedLSN pglogrepl.LSN) {
	var difference pglogrepl.LSN
	if sqlResultLSN > decodedLSN {
		difference = sqlResultLSN - decodedLSN
	} else {
		difference = decodedLSN - sqlResultLSN
	}
	if difference <= maxExpectedBarrierLSNDifference {
		return
	}
	output.Warnf(
		"Warning: PostgreSQL returned logical marker LSN %s, but the replication stream decoded it at %s; using the decoded LSN for the CDC checkpoint\n",
		sqlResultLSN,
		decodedLSN,
	)
}

func parseLogicalDecodingMessage(data []byte, protocolV2, inStream bool) (*pglogrepl.LogicalDecodingMessage, error) {
	if len(data) == 0 || pglogrepl.MessageType(data[0]) != pglogrepl.MessageTypeMessage {
		return nil, nil
	}

	var (
		message pglogrepl.Message
		err     error
	)
	if protocolV2 {
		message, err = pglogrepl.ParseV2(data, inStream)
	} else {
		message, err = pglogrepl.Parse(data)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to parse logical decoding message: %w", err)
	}

	switch message := message.(type) {
	case *pglogrepl.LogicalDecodingMessage:
		return message, nil
	case *pglogrepl.LogicalDecodingMessageV2:
		return &message.LogicalDecodingMessage, nil
	default:
		return nil, fmt.Errorf("unexpected logical decoding message type %T", message)
	}
}

func matchesBatchBarrier(message *pglogrepl.LogicalDecodingMessage, nonce string) bool {
	return message != nil && nonce != "" && !message.Transactional &&
		message.Prefix == batchBarrierPrefix && string(message.Content) == nonce
}

func matchesStreamHeartbeat(message *pglogrepl.LogicalDecodingMessage) bool {
	return message != nil && !message.Transactional && message.Prefix == streamHeartbeatPrefix
}

func validateBatchBarrierSupport(serverVersion int) error {
	if serverVersion < 140000 {
		return fmt.Errorf("PostgreSQL CDC batch mode requires PostgreSQL 14 or newer for a safe logical-decoding barrier (server reports %d); use --stream or upgrade PostgreSQL", serverVersion)
	}
	return nil
}
