package destination

import (
	"context"
	"fmt"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
)

const cdcResultDrainTimeout = time.Second

type truncateBoundary struct {
	truncate bool
	err      error
}

// WriteWithTruncateBoundaries splits a CDC input stream into ordered write
// segments. Each segment is durable before the target is emptied, so parallel
// destination writers cannot race rows from opposite sides of a TRUNCATE.
func WriteWithTruncateBoundaries(
	ctx context.Context,
	dest Destination,
	records <-chan source.RecordBatchResult,
	opts WriteOptions,
) (bool, error) {
	return writeWithTruncateBoundaries(ctx, dest, records, opts, nil)
}

// WriteWithTruncateBoundariesAfterCancel cancels the owner of records before
// draining after an error. Router-owned channels require this ordering because
// cancellation is what closes the per-table channel.
func WriteWithTruncateBoundariesAfterCancel(
	ctx context.Context,
	dest Destination,
	records <-chan source.RecordBatchResult,
	opts WriteOptions,
	cancelInput func(),
) (bool, error) {
	return writeWithTruncateBoundaries(ctx, dest, records, opts, cancelInput)
}

func writeWithTruncateBoundaries(
	ctx context.Context,
	dest Destination,
	records <-chan source.RecordBatchResult,
	opts WriteOptions,
	cancelInput func(),
) (bool, error) {
	truncated := false
	wroteSegment := false
	for {
		first, ok := <-records
		if !ok {
			if !wroteSegment {
				empty := make(chan source.RecordBatchResult)
				close(empty)
				if err := dest.WriteParallel(ctx, empty, opts); err != nil {
					cancelAndDrainCDCResults(records, cancelInput)
					return truncated, err
				}
			}
			return truncated, nil
		}
		if first.Err != nil {
			releaseCDCResult(first)
			cancelAndDrainCDCResults(records, cancelInput)
			return truncated, first.Err
		}
		if first.Truncate {
			releaseCDCResult(first)
			if !wroteSegment {
				readyOpts := opts
				readyOpts.PreStaged = nil
				empty := make(chan source.RecordBatchResult)
				close(empty)
				if err := dest.WriteParallel(ctx, empty, readyOpts); err != nil {
					cancelAndDrainCDCResults(records, cancelInput)
					return truncated, err
				}
				wroteSegment = true
			}
			if err := applyCDCInputTruncate(ctx, dest, opts); err != nil {
				cancelAndDrainCDCResults(records, cancelInput)
				return truncated, err
			}
			truncated = true
			continue
		}

		segmentCtx, cancel := context.WithCancel(ctx)
		segment := make(chan source.RecordBatchResult)
		boundary := make(chan truncateBoundary, 1)
		go forwardUntilTruncate(segmentCtx, first, records, segment, boundary)

		writeErr := dest.WriteParallel(segmentCtx, segment, opts)
		wroteSegment = true
		if writeErr != nil {
			cancel()
			<-boundary
			cancelAndDrainCDCResults(records, cancelInput)
			return truncated, writeErr
		}
		end := <-boundary
		cancel()
		if end.err != nil {
			cancelAndDrainCDCResults(records, cancelInput)
			return truncated, end.err
		}
		if !end.truncate {
			return truncated, nil
		}
		if err := applyCDCInputTruncate(ctx, dest, opts); err != nil {
			cancelAndDrainCDCResults(records, cancelInput)
			return truncated, err
		}
		truncated = true
	}
}

func forwardUntilTruncate(
	ctx context.Context,
	first source.RecordBatchResult,
	records <-chan source.RecordBatchResult,
	segment chan<- source.RecordBatchResult,
	boundary chan<- truncateBoundary,
) {
	defer close(segment)
	send := func(result source.RecordBatchResult) bool {
		select {
		case segment <- result:
			return true
		case <-ctx.Done():
			releaseCDCResult(result)
			return false
		}
	}
	if !send(first) {
		boundary <- truncateBoundary{err: ctx.Err()}
		return
	}
	for {
		select {
		case <-ctx.Done():
			boundary <- truncateBoundary{err: ctx.Err()}
			return
		case result, ok := <-records:
			if !ok {
				boundary <- truncateBoundary{}
				return
			}
			if result.Err != nil {
				releaseCDCResult(result)
				boundary <- truncateBoundary{err: result.Err}
				return
			}
			if result.Truncate {
				releaseCDCResult(result)
				boundary <- truncateBoundary{truncate: true}
				return
			}
			if !send(result) {
				boundary <- truncateBoundary{err: ctx.Err()}
				return
			}
		}
	}
}

func cancelAndDrainCDCResults(records <-chan source.RecordBatchResult, cancelInput func()) {
	if cancelInput != nil {
		cancelInput()
	}
	drainAndReleaseCDCResults(records)
}

func drainAndReleaseCDCResults(records <-chan source.RecordBatchResult) {
	timer := time.NewTimer(cdcResultDrainTimeout)
	defer timer.Stop()
	for {
		select {
		case result, ok := <-records:
			if !ok {
				return
			}
			releaseCDCResult(result)
		case <-timer.C:
			return
		}
	}
}

func releaseCDCResult(result source.RecordBatchResult) {
	if result.Batch != nil {
		result.Batch.Release()
	}
}

func applyCDCInputTruncate(ctx context.Context, dest Destination, opts WriteOptions) error {
	if opts.StagingTable {
		return ApplyTruncate(ctx, dest, opts.Table)
	}
	return ApplyCDCTruncate(ctx, dest, opts.Table)
}

func ApplyTruncate(ctx context.Context, dest Destination, table string) error {
	truncater, ok := dest.(TruncateCapable)
	if !ok {
		return fmt.Errorf("destination scheme %q cannot apply source TRUNCATE events", dest.GetScheme())
	}
	if err := truncater.TruncateTable(ctx, table); err != nil {
		return fmt.Errorf("failed to truncate %s: %w", table, err)
	}
	return nil
}

func ApplyCDCTruncate(ctx context.Context, dest Destination, table string) error {
	if truncater, ok := dest.(CDCTruncateCapable); ok {
		if err := truncater.TruncateCDCTable(ctx, table); err != nil {
			return fmt.Errorf("failed to truncate %s: %w", table, err)
		}
		return nil
	}
	return ApplyTruncate(ctx, dest, table)
}
