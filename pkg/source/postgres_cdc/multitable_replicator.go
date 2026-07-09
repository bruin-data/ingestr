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
	started       bool
	streaming     bool
	recv          *walReceiver
}

func NewMultiTableReplicator(src *PostgresCDCSource, tables []source.SourceTableInfo, cdcConfig CDCConfig, startLSN pglogrepl.LSN, lsnFilter LSNUpdater, streaming bool) (*MultiTableReplicator, error) {
	decoder := NewMultiTableDecoder(tables)

	src.lag.streaming.Store(streaming)

	return &MultiTableReplicator{
		source:        src,
		tables:        tables,
		cdcConfig:     cdcConfig,
		startLSN:      startLSN,
		decoder:       decoder,
		lsnFilter:     lsnFilter,
		clientXLogPos: startLSN,
		started:       false,
		streaming:     streaming,
	}, nil
}

// PendingLowWater reports the lowest LSN of any change received but not yet
// emitted. Every committed transaction's changes are handed to the caller in
// full, so only an in-flight transaction (BEGIN seen, COMMIT not yet
// processed) or a buffered in-progress streamed transaction (protocol v2)
// can be pending inside the replicator.
func (r *MultiTableReplicator) PendingLowWater() (pglogrepl.LSN, bool) {
	low, found := r.decoder.InFlightTxLSN()
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

	pluginArgs := buildPluginArgs(r.cdcConfig, r.source.serverVersion, true)
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

	r.recv = startWALReceiver(ctx, r.source.replConn, r.streaming, r.startLSN, r.source.pos, r.source.lag)
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
	return nil
}

func (r *MultiTableReplicator) CurrentLSN() pglogrepl.LSN {
	return r.clientXLogPos
}

// NextChanges returns the decoded per-table change groups of the next
// committed transaction, plus a flag indicating WAL activity.
// Returns (nil, false, nil) when no data is available.
// Returns (nil, true, nil) when WAL data was received but no commit completed
// yet (e.g. buffering a transaction) or the commit was filtered.
func (r *MultiTableReplicator) NextChanges(ctx context.Context) ([]DecodedChanges, bool, error) {
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

	if m.data == nil {
		// Keepalive: advance the processed position in stream order.
		if m.serverWALEnd > r.clientXLogPos {
			r.clientXLogPos = m.serverWALEnd
		}
		return nil, true, nil
	}

	config.Debug("[CDC] Processing XLogData at LSN %s, data len=%d, first byte=%x", m.walStart, len(m.data), m.data[0])

	groups, err := r.decoder.Decode(m.data, m.walStart)
	if err != nil {
		return nil, true, fmt.Errorf("failed to decode WAL data: %w", err)
	}

	if m.walStart > r.clientXLogPos {
		r.clientXLogPos = m.walStart
	}

	// Filter change groups based on per-table LSN, and record the surviving
	// ones as processed: ownership passes to the caller's accumulator in full.
	var out []DecodedChanges
	for _, g := range groups {
		if r.lsnFilter != nil && r.lsnFilter.ShouldFilterChange(g.TableName, g.LSN) {
			config.Debug("[CDC] Filtering changes for %s at LSN %s (already processed)", g.TableName, g.LSN)
			continue
		}
		if r.lsnFilter != nil {
			r.lsnFilter.updateProcessedLSN(g.TableName, g.LSN)
		}
		out = append(out, g)
	}

	return out, true, nil
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
