package postgres_cdc

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
)

func (r *snapshotSpoolReader) capture(ctx context.Context, stop <-chan struct{}) error {
	var pending []DecodedChanges
	var pendingBytes int64
	lastFlush, lastHeartbeat := time.Now(), time.Now()
	flush := func() error {
		if err := r.stageChanges(ctx, pending); err != nil {
			return err
		}
		pending = nil
		pendingBytes = 0
		lastFlush = time.Now()
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-stop:
			return flush()
		default:
		}
		if r.memory != nil && r.memory.size() >= r.options.WALBufferBytes {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-stop:
				return flush()
			case <-r.wake:
			}
			continue
		}
		groups, _, err := r.repl.NextChanges(ctx)
		if err != nil {
			return err
		}
		for _, group := range groups {
			pending = append(pending, group)
			for _, change := range group.Changes {
				pendingBytes += changeStructBytes + estimateChangeBytes(change)
			}
		}
		if pendingBytes >= min(r.options.WALBufferBytes, int64(4<<20)) || time.Since(lastFlush) >= time.Second {
			if err := flush(); err != nil {
				return err
			}
		}
		if time.Since(lastHeartbeat) >= streamHeartbeatInterval {
			if err := r.repl.EmitStreamHeartbeat(ctx); err != nil {
				return err
			}
			lastHeartbeat = time.Now()
		}
	}
}

func (r *snapshotSpoolReader) stageChanges(ctx context.Context, pending []DecodedChanges) error {
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	captured := r.repl.CurrentLSN()
	if low, ok := r.repl.PendingLowWater(); ok {
		captured = 0
		if low > 0 {
			captured = low - 1
		}
	}
	if err := r.repl.ValidateIdleCheckpoint(ctx); err != nil {
		return err
	}
	r.journalMu.Lock()
	err := r.journal.append(ctx, pending, captured)
	r.journalMu.Unlock()
	if err != nil {
		return err
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if r.memory == nil {
		r.source.pos.Commit(captured)
	}
	return nil
}

func (r *snapshotSpoolReader) replay(ctx context.Context) error {
	r.journalMu.Lock()
	view := r.journal.manifest
	r.journalMu.Unlock()
	for sequence := r.state.Consumed; sequence < view.Tail; sequence++ {
		data, err := r.journal.store.Load(ctx, r.journal.pageKey(sequence))
		if err != nil {
			return err
		}
		if data == nil {
			return fmt.Errorf("normal snapshot WAL page %d is missing; cannot resume safely", sequence)
		}
		var groups []DecodedChanges
		if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&groups); err != nil {
			return fmt.Errorf("decode normal snapshot WAL page %d: %w", sequence, err)
		}
		if err := r.apply(ctx, groups); err != nil {
			return err
		}
	}
	if err := r.flush(ctx); err != nil {
		return err
	}
	previous := r.state.Consumed
	r.state.Applied = max(r.state.Applied, view.Captured)
	r.state.Consumed = view.Tail
	if err := r.checkpoint(ctx, nil); err != nil {
		return err
	}
	r.journalMu.Lock()
	err := r.journal.prune(ctx, previous, view.Tail)
	r.journalMu.Unlock()
	select {
	case r.wake <- struct{}{}:
	default:
	}
	return err
}

func (r *snapshotSpoolReader) apply(ctx context.Context, groups []DecodedChanges) error {
	for _, group := range groups {
		r.accum.add(group.TableName, group.Changes, group.LSN)
	}
	return r.accumulatorResults(ctx, func(results chan<- source.RecordBatchResult) error {
		return r.accum.flushReadyContext(ctx, results, nil)
	})
}

func (r *snapshotSpoolReader) flush(ctx context.Context) error {
	return r.accumulatorResults(ctx, func(results chan<- source.RecordBatchResult) error { return r.accum.flushAllContext(ctx, results, nil) })
}

func (r *snapshotSpoolReader) accumulatorResults(ctx context.Context, flush func(chan<- source.RecordBatchResult) error) error {
	if r.multi {
		return flush(r.results)
	}
	results := make(chan source.RecordBatchResult, 1)
	done := make(chan error, 1)
	go func() { done <- flush(results); close(results) }()
	var forwardErr error
	for result := range results {
		if forwardErr != nil {
			if result.Batch != nil {
				result.Batch.Release()
			}
			continue
		}
		forwardErr = r.emit(ctx, result)
	}
	err := <-done
	if forwardErr != nil {
		return forwardErr
	}
	return err
}

func (r *snapshotSpoolReader) catchUp(ctx context.Context) error {
	barrier, err := r.source.beginBatchBarrier(ctx)
	if err != nil {
		return err
	}
	r.repl.barrierNonce, r.repl.barrierTarget = barrier.nonce, barrier.target
	r.repl.barrierSeen, r.repl.barrierLSN = false, 0
	lastFlush := time.Now()
	for {
		groups, _, err := r.repl.NextChanges(ctx)
		if err != nil {
			return err
		}
		if err := r.apply(ctx, groups); err != nil {
			return err
		}
		if r.repl.BarrierReached() || time.Since(lastFlush) >= streamingMaxAge(r.opts) {
			if err := r.flush(ctx); err != nil {
				return err
			}
			position := safeCommitLSN(r.repl, r.accum)
			r.state.Applied = max(r.state.Applied, position)
			if err := r.checkpoint(ctx, nil); err != nil {
				return err
			}
			lastFlush = time.Now()
			if r.repl.BarrierReached() && position >= r.repl.BarrierLSN() {
				return nil
			}
		}
	}
}
