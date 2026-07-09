package destination

import (
	"context"
	"fmt"

	"github.com/bruin-data/ingestr/pkg/source"
)

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
			if err := ApplyTruncate(ctx, dest, opts.Table); err != nil {
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
		if err := ApplyTruncate(ctx, dest, opts.Table); err != nil {
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
