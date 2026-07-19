package multitable

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
)

// Write routes records from a multi-table source to the appropriate destination tables.
// It uses a router to distribute batches by table name and writes to each table concurrently.
// This is a generic implementation that works with any Destination.
func Write(
	ctx context.Context,
	dest destination.Destination,
	records <-chan source.RecordBatchResult,
	opts destination.MultiTableWriteOptions,
) error {
	_, err := WriteWithResult(ctx, dest, records, opts)
	return err
}

type WriteResult struct {
	TruncatedTables map[string]bool
}

func WriteWithResult(
	ctx context.Context,
	dest destination.Destination,
	records <-chan source.RecordBatchResult,
	opts destination.MultiTableWriteOptions,
) (WriteResult, error) {
	result := WriteResult{TruncatedTables: make(map[string]bool)}
	tables := make([]string, 0, len(opts.TableConfigs))
	for table := range opts.TableConfigs {
		tables = append(tables, table)
	}

	if len(tables) == 0 {
		return result, nil
	}

	config.Debug("[MULTITABLE] Starting multi-table write for %d tables", len(tables))
	startTotal := time.Now()

	// Derive a cancellable context so that when any table's writer fails,
	// we cancel the router and all other writers to avoid a deadlock where
	// a failed table's full channel blocks the shared router goroutine.
	writeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var cancelOnce sync.Once
	cancelInput := func() {
		cancelOnce.Do(func() {
			cancel()
			if opts.CancelSource != nil {
				opts.CancelSource()
			}
		})
	}

	router := NewRouter(tables, 8)
	router.Route(writeCtx, records, opts.CancelSource != nil)

	var wg sync.WaitGroup
	errChan := make(chan error, len(tables))
	var resultMu sync.Mutex

	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = 4
	}

	for sourceTable, cfg := range opts.TableConfigs {
		wg.Add(1)
		go func(table string, tableConfig destination.TableWriteConfig) {
			defer wg.Done()

			ch := router.GetChannel(table)
			if ch == nil {
				return
			}

			truncated, err := destination.WriteWithTruncateBoundariesAfterCancel(writeCtx, dest, ch, destination.WriteOptions{
				Table:                  tableConfig.DestTable,
				Schema:                 tableConfig.Schema,
				PrimaryKeys:            tableConfig.PrimaryKeys,
				Parallelism:            parallelism,
				StagingTable:           opts.StagingTable,
				StagingBucket:          opts.StagingBucket,
				LoaderFileSize:         opts.LoaderFileSize,
				LoaderFileFormat:       opts.LoaderFileFormat,
				DeduplicatePrimaryKeys: tableConfig.DeduplicatePrimaryKeys,
				IncrementalKey:         tableConfig.IncrementalKey,
			}, cancelInput)
			if truncated {
				resultMu.Lock()
				result.TruncatedTables[table] = true
				resultMu.Unlock()
			}
			if err != nil {
				cancelInput()
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return
				}
				errChan <- fmt.Errorf("table %s: %w", table, err)
			}
		}(sourceTable, cfg)
	}

	wg.Wait()
	router.Wait()
	close(errChan)

	var errs []error
	for err := range errChan {
		errs = append(errs, err)
	}

	if routerErr := router.Err(); routerErr != nil && !errors.Is(routerErr, context.Canceled) && !errors.Is(routerErr, context.DeadlineExceeded) {
		errs = append(errs, fmt.Errorf("router error: %w", routerErr))
	}

	if len(errs) > 0 {
		return result, fmt.Errorf("multi-table write failed: %v", errs)
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}

	config.Debug("[MULTITABLE] Multi-table write completed in %v", time.Since(startTotal))
	return result, nil
}
