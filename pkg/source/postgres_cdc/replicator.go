package postgres_cdc

import (
	"context"
	"fmt"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/schema"
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

func (r *Replicator) NextBatch(ctx context.Context, batchSize int) (arrow.RecordBatch, error) {
	// Start replication if not yet started
	if !r.started {
		if err := r.Start(ctx); err != nil {
			return nil, err
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
			return nil, ctx.Err()
		}
		// Timeout is expected when no data is available
		if ctxWithTimeout.Err() != nil {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to receive message: %w", err)
	}

	if msg == nil {
		return nil, nil
	}

	switch msg := msg.(type) {
	case *pgproto3.CopyData:
		if len(msg.Data) == 0 {
			return nil, nil
		}

		switch msg.Data[0] {
		case pglogrepl.PrimaryKeepaliveMessageByteID:
			pkm, err := pglogrepl.ParsePrimaryKeepaliveMessage(msg.Data[1:])
			if err != nil {
				return nil, fmt.Errorf("failed to parse keepalive: %w", err)
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
				return nil, fmt.Errorf("failed to parse xlog data: %w", err)
			}

			batch, err := r.decoder.Decode(xld.WALData, xld.WALStart)
			if err != nil {
				return nil, fmt.Errorf("failed to decode WAL data: %w", err)
			}

			if xld.WALStart > r.clientXLogPos {
				r.clientXLogPos = xld.WALStart
			}

			return batch, nil
		}
	}

	return nil, nil
}
