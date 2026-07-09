package postgres_cdc

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
)

// walReceiveBuffer bounds how many WAL messages the receiver may buffer ahead
// of the decoder. Big enough to keep the decoder fed while the socket drains,
// small enough that a slow decoder exerts backpressure instead of buffering
// unbounded row data in memory.
const walReceiveBuffer = 256

// standbyStatusInterval is how often the receiver reports standby status to
// the walsender.
const standbyStatusInterval = 10 * time.Second

// walMessage is one replication-stream event, delivered to the decoder in
// arrival order: an XLogData payload (data != nil) or a keepalive's server
// WAL end position (data == nil). Keepalives travel through the same channel
// so the decode side's processed position advances strictly in stream order —
// it must never move past WAL data that has not been decoded yet, or a batch
// run could confirm undrained WAL to the slot.
type walMessage struct {
	data         []byte
	walStart     pglogrepl.LSN
	serverWALEnd pglogrepl.LSN
}

// walReceiver owns the replication connection after StartReplication: it is
// the only goroutine reading from or writing to it. It splits network receive
// from decode — the socket keeps draining into a bounded channel while the
// decoder works — and answers keepalives and the standby-status cadence
// itself, so a busy decoder can never starve the walsender of status updates
// (which would get the connection killed after wal_sender_timeout).
type walReceiver struct {
	// receive and sendStatusUpdate abstract the pgconn calls for testability.
	receive          func(ctx context.Context) (pgproto3.BackendMessage, error)
	sendStatusUpdate func(ctx context.Context, ssu pglogrepl.StandbyStatusUpdate) error

	streaming bool
	startLSN  pglogrepl.LSN
	pos       *streamPosition

	received atomic.Uint64 // highest LSN received from the server

	msgs   chan walMessage
	errc   chan error
	done   chan struct{}
	cancel context.CancelFunc

	// savedErr caches the receiver's terminal error after the decode side
	// first reads it (errc only delivers once). Decode-side access only.
	savedErr error
}

func startWALReceiver(ctx context.Context, conn *pgconn.PgConn, streaming bool, startLSN pglogrepl.LSN, pos *streamPosition) *walReceiver {
	return startWALReceiverFuncs(
		ctx,
		conn.ReceiveMessage,
		func(ctx context.Context, ssu pglogrepl.StandbyStatusUpdate) error {
			return pglogrepl.SendStandbyStatusUpdate(ctx, conn, ssu)
		},
		streaming, startLSN, pos,
	)
}

func startWALReceiverFuncs(
	ctx context.Context,
	receive func(ctx context.Context) (pgproto3.BackendMessage, error),
	sendStatusUpdate func(ctx context.Context, ssu pglogrepl.StandbyStatusUpdate) error,
	streaming bool,
	startLSN pglogrepl.LSN,
	pos *streamPosition,
) *walReceiver {
	rctx, cancel := context.WithCancel(ctx)
	r := &walReceiver{
		receive:          receive,
		sendStatusUpdate: sendStatusUpdate,
		streaming:        streaming,
		startLSN:         startLSN,
		pos:              pos,
		msgs:             make(chan walMessage, walReceiveBuffer),
		errc:             make(chan error, 1),
		done:             make(chan struct{}),
		cancel:           cancel,
	}
	r.received.Store(uint64(startLSN))
	go r.run(rctx)
	return r
}

// stop terminates the receiver and waits for it to release the connection.
func (r *walReceiver) stop() {
	r.cancel()
	<-r.done
}

func (r *walReceiver) receivedLSN() pglogrepl.LSN {
	return pglogrepl.LSN(r.received.Load())
}

func (r *walReceiver) noteReceived(lsn pglogrepl.LSN) {
	for {
		cur := r.received.Load()
		if uint64(lsn) <= cur || r.received.CompareAndSwap(cur, uint64(lsn)) {
			return
		}
	}
}

// err returns the receiver's terminal error, once the receiver has stopped.
func (r *walReceiver) err() error {
	if r.savedErr == nil {
		select {
		case r.savedErr = <-r.errc:
		default:
			r.savedErr = fmt.Errorf("replication receiver stopped")
		}
	}
	return r.savedErr
}

func (r *walReceiver) fail(err error) {
	select {
	case r.errc <- err:
	default:
	}
}

func (r *walReceiver) sendStatus(ctx context.Context, replyRequested bool) error {
	var committed pglogrepl.LSN
	if r.streaming && r.pos != nil {
		committed = r.pos.Committed()
	}
	status := standbyUpdate(r.streaming, r.receivedLSN(), committed, r.startLSN)
	status.ReplyRequested = replyRequested
	return r.sendStatusUpdate(ctx, status)
}

func (r *walReceiver) run(ctx context.Context) {
	defer close(r.done)

	lastStatus := time.Now()
	lastMessageAt := time.Now()

	for {
		if ctx.Err() != nil {
			return
		}

		if time.Since(lastStatus) >= standbyStatusInterval {
			if err := r.sendStatus(ctx, time.Since(lastMessageAt) > silenceProbeAfter); err != nil {
				r.fail(fmt.Errorf("failed to send standby status (replication connection lost): %w", err))
				return
			}
			lastStatus = time.Now()
		}

		// Bound a single receive so the loop keeps the standby cadence and
		// reacts to cancellation. See receiveTimeout for why this is not
		// sub-second.
		rctx, cancel := context.WithTimeout(ctx, receiveTimeout)
		msg, err := r.receive(rctx)
		rctxErr := rctx.Err()
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			// Timeout is expected when no data is available. But total silence
			// for longer than deadConnectionTimeout (no data and no keepalives)
			// means a dead or half-open connection that the per-call read
			// timeout would mask forever.
			if rctxErr != nil {
				if time.Since(lastMessageAt) > deadConnectionTimeout {
					r.fail(fmt.Errorf("no message from server for %s; replication connection appears dead", deadConnectionTimeout))
					return
				}
				continue
			}
			r.fail(fmt.Errorf("failed to receive message: %w", err))
			return
		}

		lastMessageAt = time.Now()

		copyData, ok := msg.(*pgproto3.CopyData)
		if !ok || len(copyData.Data) == 0 {
			continue
		}

		switch copyData.Data[0] {
		case pglogrepl.PrimaryKeepaliveMessageByteID:
			pkm, err := pglogrepl.ParsePrimaryKeepaliveMessage(copyData.Data[1:])
			if err != nil {
				r.fail(fmt.Errorf("failed to parse keepalive: %w", err))
				return
			}
			r.noteReceived(pkm.ServerWALEnd)
			if pkm.ReplyRequested {
				if err := r.sendStatus(ctx, false); err != nil {
					r.fail(fmt.Errorf("failed to send standby status (replication connection lost): %w", err))
					return
				}
				lastStatus = time.Now()
			}
			if !r.deliver(ctx, walMessage{serverWALEnd: pkm.ServerWALEnd}, &lastStatus) {
				return
			}

		case pglogrepl.XLogDataByteID:
			xld, err := pglogrepl.ParseXLogData(copyData.Data[1:])
			if err != nil {
				r.fail(fmt.Errorf("failed to parse xlog data: %w", err))
				return
			}
			r.noteReceived(xld.WALStart)
			// Copy: pgconn's message buffer is only valid until the next
			// receive, and this payload sits in the channel while the socket
			// keeps draining.
			if !r.deliver(ctx, walMessage{data: append([]byte(nil), xld.WALData...), walStart: xld.WALStart}, &lastStatus) {
				return
			}
		}
	}
}

// deliver pushes a message to the decoder, keeping standby status flowing
// while blocked on a full buffer so backpressure cannot get the walsender
// killed for silence.
func (r *walReceiver) deliver(ctx context.Context, m walMessage, lastStatus *time.Time) bool {
	for {
		select {
		case r.msgs <- m:
			return true
		case <-ctx.Done():
			return false
		case <-time.After(standbyStatusInterval):
			if err := r.sendStatus(ctx, false); err != nil {
				r.fail(fmt.Errorf("failed to send standby status (replication connection lost): %w", err))
				return false
			}
			*lastStatus = time.Now()
		}
	}
}
