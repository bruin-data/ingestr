package postgres_cdc

import (
	"context"
	"fmt"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
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

// MultiTableReplicator streams WAL changes for multiple tables. Network
// receive and decode are pipelined: a walReceiver goroutine owns the
// replication connection and drains the socket into a bounded channel, while
// NextChanges consumes and decodes from it. clientXLogPos is the decode-side
// processed position — it only advances as messages are consumed from the
// channel, so batch mode's target check and safeCommitLSN never move past WAL
// that is received but not yet decoded.
type MultiTableReplicator struct {
	source        *PostgresCDCSource
	tables        []source.SourceTableInfo
	cdcConfig     CDCConfig
	startLSN      pglogrepl.LSN
	decoder       *MultiTableDecoder
	lsnFilter     LSNUpdater
	clientXLogPos pglogrepl.LSN
	barrierNonce  string
	barrierSeen   bool
	protocolV2    bool
	started       bool
	streaming     bool
	recv          *walReceiver
	decoderBudget *byteBudget
	walBudget     *byteBudget

	filterLSN       pglogrepl.LSN
	filterDecisions map[string]bool
}

func NewMultiTableReplicator(src *PostgresCDCSource, tables []source.SourceTableInfo, cdcConfig CDCConfig, startLSN pglogrepl.LSN, lsnFilter LSNUpdater, streaming bool, barrierNonce string) (*MultiTableReplicator, error) {
	decoderBudget := newByteBudget(defaultDecoderMemoryBytes)
	decoder := newMultiTableDecoderWithBudget(tables, decoderBudget)
	if reader, ok := lsnFilter.(*MultiTableCDCReader); ok {
		decoder.AllowUnknownRelationColumns(reader.allowedUnknown)
		decoder.AllowHistoricalRelationIDs(reader.historicalRelIDs)
	}

	src.lag.streaming.Store(streaming)

	return &MultiTableReplicator{
		source:        src,
		tables:        tables,
		cdcConfig:     cdcConfig,
		startLSN:      startLSN,
		decoder:       decoder,
		lsnFilter:     lsnFilter,
		clientXLogPos: startLSN,
		barrierNonce:  barrierNonce,
		protocolV2:    streaming && src.serverVersion >= 140000,
		started:       false,
		streaming:     streaming,
		decoderBudget: decoderBudget,
		walBudget:     newByteBudget(defaultWALBufferBytes),
	}, nil
}

// PendingLowWater reports the lowest LSN of any in-flight or committed change
// not yet fully handed to the caller.
func (r *MultiTableReplicator) PendingLowWater() (pglogrepl.LSN, bool) {
	low, found := r.decoder.InFlightTxLSN()
	if committed, ok := r.decoder.CommittedLowWater(); ok && (!found || committed < low) {
		low = committed
		found = true
	}
	if slow, ok := r.decoder.StreamedLowWater(); ok && (!found || slow < low) {
		low = slow
		found = true
	}
	return low, found
}

// buildPluginArgs assembles the pgoutput options. Protocol v2 with
// `streaming 'true'` lets the server ship large in-flight transactions before
// they commit instead of spilling them server-side; `binary 'true'` skips the
// text encode/parse round-trip for column values. Both options exist since
// PostgreSQL 14; older servers get the plain v1 arguments.
func buildPluginArgs(cfg CDCConfig, serverVersion int, allowStreaming bool) []string {
	protoVersion := 1
	var extra []string
	if serverVersion >= 140000 {
		extra = append(extra, "messages 'true'")
		if allowStreaming {
			protoVersion = 2
			extra = append(extra, "streaming 'true'")
		}
		if cfg.Binary {
			extra = append(extra, "binary 'true'")
		}
	}
	args := []string{
		fmt.Sprintf("proto_version '%d'", protoVersion),
		fmt.Sprintf("publication_names '%s'", cfg.Publication),
	}
	return append(args, extra...)
}

func (r *MultiTableReplicator) Start(ctx context.Context) error {
	if r.started {
		return nil
	}

	config.Debug("[CDC] Starting multi-table replication from LSN: %s", r.startLSN)

	pluginArgs := buildPluginArgs(r.cdcConfig, r.source.serverVersion, r.streaming)
	config.Debug("[CDC] pgoutput options: %v", pluginArgs)

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

	r.recv = startWALReceiverWithBudget(ctx, r.source.replConn, r.streaming, r.startLSN, r.source.pos, r.source.lag, r.walBudget)
	r.started = true
	config.Debug("[CDC] Multi-table replication started successfully")
	return nil
}

// Close stops the WAL receiver goroutine and waits for it to release the
// replication connection. Idempotent; must be called before anything else
// (keepalive goroutine, reconnect) touches the connection.
func (r *MultiTableReplicator) Close(ctx context.Context) error {
	if r.recv != nil {
		r.recv.stop()
		r.recv = nil
	}
	return r.decoder.Close()
}

func (r *MultiTableReplicator) CurrentLSN() pglogrepl.LSN {
	return r.clientXLogPos
}

func (r *MultiTableReplicator) BarrierReached() bool {
	return r.barrierSeen
}

func (r *MultiTableReplicator) handleLogicalMessage(data []byte) (bool, error) {
	message, err := parseLogicalDecodingMessage(data, r.protocolV2, r.decoder.InStream())
	if err != nil || message == nil {
		return message != nil, err
	}
	if matchesBatchBarrier(message, r.barrierNonce) {
		r.barrierSeen = true
		if message.LSN > r.clientXLogPos {
			r.clientXLogPos = message.LSN
		}
	}
	return true, nil
}

// NextChanges returns the decoded per-table change groups of the next
// committed transaction, plus a flag indicating WAL activity.
// Returns (nil, false, nil) when no data is available.
// Returns (nil, true, nil) when WAL data was received but no commit completed
// yet (e.g. buffering a transaction) or the commit was filtered.
func (r *MultiTableReplicator) NextChanges(ctx context.Context) ([]DecodedChanges, bool, error) {
	if r.decoder.HasCommitted() {
		groups, err := r.decoder.DrainCommitted(defaultCommittedDrainChanges)
		if err != nil {
			return nil, true, err
		}
		return r.filterGroups(groups, !r.decoder.HasCommitted()), true, nil
	}
	// Start replication if not yet started
	if !r.started {
		if err := r.Start(ctx); err != nil {
			return nil, false, err
		}
	}

	m, ok, err := r.nextMessage(ctx)
	if err != nil || !ok {
		return nil, false, err
	}
	defer m.release()

	if m.data == nil {
		return nil, true, nil
	}

	config.Debug("[CDC] Processing XLogData at LSN %s, data len=%d, first byte=%x", m.walStart, len(m.data), m.data[0])

	handledLogicalMessage, err := r.handleLogicalMessage(m.data)
	if err != nil {
		return nil, true, err
	}
	if handledLogicalMessage {
		return nil, true, nil
	}

	groups, err := r.decoder.Decode(m.data, m.walStart)
	if err != nil {
		return nil, true, fmt.Errorf("failed to decode WAL data: %w", err)
	}

	processedLSN := m.walStart
	if commitLSN, ok := logicalCommitLSN(m.data); ok && commitLSN > processedLSN {
		processedLSN = commitLSN
	}
	if processedLSN > r.clientXLogPos {
		r.clientXLogPos = processedLSN
	}

	// Filter change groups based on per-table LSN, and record the surviving
	// ones as processed: ownership passes to the caller's accumulator in full.
	return r.filterGroups(groups, !r.decoder.HasCommitted()), true, nil
}

func (r *MultiTableReplicator) filterGroups(groups []DecodedChanges, transactionComplete bool) []DecodedChanges {
	var out []DecodedChanges
	for _, g := range groups {
		if r.filterDecisions == nil || r.filterLSN != g.LSN {
			r.filterLSN = g.LSN
			r.filterDecisions = make(map[string]bool)
		}
		filtered, decided := r.filterDecisions[g.TableName]
		if !decided && r.lsnFilter != nil {
			filtered = r.lsnFilter.ShouldFilterChange(g.TableName, g.LSN)
			r.filterDecisions[g.TableName] = filtered
		}
		if filtered {
			config.Debug("[CDC] Filtering changes for %s at LSN %s (already processed)", g.TableName, g.LSN)
			continue
		}
		out = append(out, g)
	}
	if transactionComplete {
		if r.lsnFilter != nil {
			for tableName, filtered := range r.filterDecisions {
				if !filtered {
					r.lsnFilter.updateProcessedLSN(tableName, r.filterLSN)
				}
			}
		}
		r.filterLSN = 0
		r.filterDecisions = nil
	}

	return out
}

// nextMessage takes the next buffered stream event from the receiver, waiting
// up to receiveTimeout when none is buffered. ok is false on a quiet stream
// (idle). Buffered messages are always drained before a receiver failure is
// surfaced, so decoded-but-undelivered WAL is never dropped ahead of the
// error.
func (r *MultiTableReplicator) nextMessage(ctx context.Context) (walMessage, bool, error) {
	select {
	case m := <-r.recv.msgs:
		return m, true, nil
	default:
	}

	select {
	case m := <-r.recv.msgs:
		return m, true, nil
	case <-ctx.Done():
		return walMessage{}, false, ctx.Err()
	case <-r.recv.done:
		select {
		case m := <-r.recv.msgs:
			return m, true, nil
		default:
		}
		if ctx.Err() != nil {
			return walMessage{}, false, ctx.Err()
		}
		return walMessage{}, false, r.recv.err()
	case <-time.After(receiveTimeout):
		return walMessage{}, false, nil
	}
}
