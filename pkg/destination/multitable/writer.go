package multitable

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
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

	type tableError struct {
		table string
		err   error
	}
	var wg sync.WaitGroup
	errChan := make(chan tableError, len(tables))
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
				errChan <- tableError{table: table, err: err}
			}
		}(sourceTable, cfg)
	}

	wg.Wait()
	router.Wait()
	close(errChan)

	// A source failure reaches every table's writer; report each distinct
	// error once with the tables it affected.
	var errs []error
	tablesByError := map[string][]string{}
	var order []string
	for te := range errChan {
		msg := te.err.Error()
		if _, seen := tablesByError[msg]; !seen {
			order = append(order, msg)
		}
		tablesByError[msg] = append(tablesByError[msg], te.table)
	}
	for _, msg := range order {
		tables := tablesByError[msg]
		sort.Strings(tables)
		label := "table " + tables[0]
		if len(tables) > 1 {
			label = "tables " + strings.Join(tables, ", ")
		}
		errs = append(errs, fmt.Errorf("%s: %s", label, msg))
	}

	if routerErr := router.Err(); routerErr != nil && !errors.Is(routerErr, context.Canceled) && !errors.Is(routerErr, context.DeadlineExceeded) && tablesByError[routerErr.Error()] == nil {
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
