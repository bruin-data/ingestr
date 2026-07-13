package postgres_cdc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgxpool"
)

const batchBarrierPrefix = "ingestr_cdc_batch_barrier"

func emitBatchBarrier(ctx context.Context, pool *pgxpool.Pool) (string, pglogrepl.LSN, error) {
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", 0, fmt.Errorf("failed to generate CDC batch barrier nonce: %w", err)
	}
	nonce := hex.EncodeToString(nonceBytes)

	var rawLSN string
	if err := pool.QueryRow(
		ctx,
		"SELECT pg_logical_emit_message(false, $1, $2)::text",
		batchBarrierPrefix, nonce,
	).Scan(&rawLSN); err != nil {
		return "", 0, fmt.Errorf("failed to emit CDC batch barrier: %w", err)
	}
	lsn, err := pglogrepl.ParseLSN(rawLSN)
	if err != nil {
		return "", 0, fmt.Errorf("failed to parse CDC batch barrier LSN %q: %w", rawLSN, err)
	}
	return nonce, lsn, nil
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

func validateBatchBarrierSupport(serverVersion int) error {
	if serverVersion < 140000 {
		return fmt.Errorf("PostgreSQL CDC batch mode requires PostgreSQL 14 or newer for a safe logical-decoding barrier (server reports %d); use --stream or upgrade PostgreSQL", serverVersion)
	}
	return nil
}
