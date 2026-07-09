package strategy

import (
	"context"
	"fmt"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
)

type truncateBoundary struct {
	truncate bool
	err      error
}

// writeRecordsWithTruncate splits a CDC input stream at TRUNCATE controls.
// Each completed segment is durable before the write table is emptied, so a
// parallel destination writer can never race writes from opposite sides of a
// truncate boundary.
func writeRecordsWithTruncate(
	ctx context.Context,
	dest destination.Destination,
	records <-chan source.RecordBatchResult,
	opts destination.WriteOptions,
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
					return truncated, err
				}
			}
			return truncated, nil
		}
		if first.Err != nil {
			return truncated, first.Err
		}
		if first.Truncate {
			if !wroteSegment {
				readyOpts := opts
				readyOpts.PreStaged = nil
				empty := make(chan source.RecordBatchResult)
				close(empty)
				if err := dest.WriteParallel(ctx, empty, readyOpts); err != nil {
					return truncated, err
				}
				wroteSegment = true
			}
			if err := truncateWriteTable(ctx, dest, opts.Table); err != nil {
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
			return truncated, writeErr
		}
		end := <-boundary
		cancel()
		if end.err != nil {
			return truncated, end.err
		}
		if !end.truncate {
			return truncated, nil
		}
		if err := truncateWriteTable(ctx, dest, opts.Table); err != nil {
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
				boundary <- truncateBoundary{err: result.Err}
				return
			}
			if result.Truncate {
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

func truncateWriteTable(ctx context.Context, dest destination.Destination, table string) error {
	truncater, ok := dest.(destination.TruncateCapable)
	if !ok {
		return fmt.Errorf("destination scheme %q cannot apply source TRUNCATE events", dest.GetScheme())
	}
	if err := truncater.TruncateTable(ctx, table); err != nil {
		return fmt.Errorf("failed to truncate %s: %w", table, err)
	}
	return nil
}
