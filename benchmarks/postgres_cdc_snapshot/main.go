// Command postgres_cdc_snapshot drains a normal CDC snapshot into a counting
// sink. It creates and removes its own logical slot, without acknowledging WAL.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/source/postgres_cdc"
	"github.com/jackc/pgx/v5/pgxpool"
)

type measurement struct {
	ReadersRequested   int     `json:"readers_requested"`
	Rows               int64   `json:"rows"`
	ArrowBufferBytes   uint64  `json:"source_arrow_buffer_bytes"`
	Batches            int64   `json:"batches"`
	SetupSeconds       float64 `json:"connection_schema_setup_seconds"`
	SnapshotSeconds    float64 `json:"snapshot_seconds"`
	TotalSeconds       float64 `json:"total_seconds"`
	FirstBatchSeconds  float64 `json:"first_batch_seconds"`
	DrainSeconds       float64 `json:"first_batch_to_end_seconds"`
	SinkSeconds        float64 `json:"count_release_seconds"`
	RowsPerSecond      float64 `json:"rows_per_second_total"`
	MBPerSecond        float64 `json:"source_arrow_mb_per_second_total"`
	SnapshotMBPerSec   float64 `json:"source_arrow_mb_per_second_snapshot"`
	PeakConnections    int     `json:"peak_source_connections_sampled"`
	PeakActiveQueries  int     `json:"peak_active_source_queries_sampled"`
	PeakRetainedWAL    int64   `json:"peak_retained_wal_bytes_sampled"`
	MonitoringFailures int     `json:"monitoring_failures"`
}

func main() {
	uriEnv := flag.String("uri-env", "INGESTR_BENCH_SOURCE_URI", "environment variable containing a postgres+cdc URI with an existing publication")
	table := flag.String("table", "bench.wide_25k", "published table to snapshot")
	readers := flag.Int("readers", 4, "total snapshot reader connection budget")
	pageSize := flag.Int("page-size", 5000, "rows per Arrow batch")
	batchBytes := flag.Int64("batch-bytes", 16*1024*1024, "maximum target bytes per Arrow batch")
	timeout := flag.Duration("timeout", time.Hour, "whole benchmark timeout")
	debug := flag.Bool("debug", false, "include reader planning/import/forward timing logs on stderr")
	flag.Parse()
	if *readers < 1 || *pageSize < 1 || *batchBytes < 1 {
		fmt.Fprintln(os.Stderr, "readers, page-size, and batch-bytes must be positive")
		os.Exit(2)
	}
	uri := os.Getenv(*uriEnv)
	if uri == "" {
		fmt.Fprintf(os.Stderr, "%s is not set\n", *uriEnv)
		os.Exit(2)
	}
	config.DebugMode = *debug
	output.Init(os.Stderr, os.Stderr, output.ModeText)
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(signalCtx, *timeout)
	defer cancel()
	result, err := run(ctx, uri, *table, *readers, *pageSize, *batchBytes)
	if err != nil {
		fmt.Fprintln(os.Stderr, redactURIError(err, uri))
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, uri, table string, readers, pageSize int, batchBytes int64) (measurement, error) {
	result := measurement{ReadersRequested: readers}
	started := time.Now()
	parsed, err := url.Parse(uri)
	if err != nil {
		return result, fmt.Errorf("parse source URI: %w", err)
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" && parsed.Scheme != "postgres+cdc" && parsed.Scheme != "postgresql+cdc" {
		return result, fmt.Errorf("source URI must use postgres or postgres+cdc")
	}
	params := parsed.Query()
	publication := params.Get("publication")
	if publication == "" {
		return result, fmt.Errorf("source URI must specify an existing publication")
	}
	slot := fmt.Sprintf("wi2_bench_%d_%d", os.Getpid(), time.Now().UnixNano())
	application := slot
	params.Set("slot", slot)
	params.Set("application_name", application)
	params.Set("pool_max_conns", fmt.Sprint(readers))
	parsed.Scheme = "postgres+cdc"
	parsed.RawQuery = params.Encode()
	cdcURI := parsed.String()
	for _, key := range []string{"publication", "slot", "mode", "dest_schema", "state_id", "binary", "discover_interval"} {
		params.Del(key)
	}
	params.Set("application_name", "wi2_bench_monitor")
	params.Set("pool_max_conns", "1")
	parsed.Scheme = "postgres"
	parsed.RawQuery = params.Encode()
	monitor, err := pgxpool.New(ctx, parsed.String())
	if err != nil {
		return result, fmt.Errorf("create benchmark monitor: %w", err)
	}
	defer monitor.Close()
	src := postgres_cdc.NewPostgresCDCSource()
	if err := src.Connect(ctx, cdcURI); err != nil {
		return result, err
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = src.Close(cleanupCtx)
		if _, err := monitor.Exec(cleanupCtx, "SELECT pg_drop_replication_slot(slot_name) FROM pg_replication_slots WHERE slot_name = $1", slot); err != nil {
			fmt.Fprintf(os.Stderr, "could not remove benchmark slot %s; remove it before leaving the stack idle\n", slot)
		}
	}()
	tableSource, err := src.GetTable(ctx, source.TableRequest{Name: table})
	if err != nil {
		return result, err
	}
	tableSchema, err := tableSource.GetSchema(ctx)
	if err != nil {
		return result, err
	}
	snapshot, err := postgres_cdc.NewSnapshot(src, table, tableSchema, postgres_cdc.CDCConfig{Publication: publication, SlotName: slot}, "")
	if err != nil {
		return result, err
	}
	monitorCtx, stopMonitor := context.WithCancel(ctx)
	monitorDone := make(chan measurement, 1)
	go func() { monitorDone <- monitorSource(monitorCtx, monitor, application, slot) }()
	defer stopMonitor()
	results := make(chan source.RecordBatchResult, 1)
	done := make(chan error, 1)
	executeStarted := time.Now()
	result.SetupSeconds = executeStarted.Sub(started).Seconds()
	sourceColumns := len(tableSchema.Columns) - len(destination.CDCMetadataColumns())
	go func() {
		_, _, err := snapshot.Execute(ctx, results, source.ReadOptions{
			Parallelism: readers, PageSize: pageSize, MaxBatchBytes: batchBytes, RecordBatchBufferSize: 1,
		})
		done <- err
		close(results)
	}()
	var firstBatch time.Time
	var readErr error
	for batch := range results {
		if batch.Err != nil {
			readErr = batch.Err
		}
		if batch.Batch == nil {
			continue
		}
		countStarted := time.Now()
		if firstBatch.IsZero() {
			firstBatch = countStarted
		}
		result.Rows += batch.Batch.NumRows()
		result.Batches++
		for i := range sourceColumns {
			result.ArrowBufferBytes += batch.Batch.Column(i).Data().SizeInBytes()
		}
		batch.Batch.Release()
		result.SinkSeconds += time.Since(countStarted).Seconds()
	}
	finished := time.Now()
	executeErr := <-done
	stopMonitor()
	monitored := <-monitorDone
	result.PeakConnections = monitored.PeakConnections
	result.PeakActiveQueries = monitored.PeakActiveQueries
	result.PeakRetainedWAL = monitored.PeakRetainedWAL
	result.MonitoringFailures = monitored.MonitoringFailures
	if executeErr != nil {
		return result, executeErr
	}
	if readErr != nil {
		return result, readErr
	}
	result.SnapshotSeconds = finished.Sub(executeStarted).Seconds()
	result.TotalSeconds = finished.Sub(started).Seconds()
	if !firstBatch.IsZero() {
		result.FirstBatchSeconds = firstBatch.Sub(executeStarted).Seconds()
		result.DrainSeconds = finished.Sub(firstBatch).Seconds()
	}
	result.RowsPerSecond = float64(result.Rows) / result.TotalSeconds
	result.MBPerSecond = float64(result.ArrowBufferBytes) / 1e6 / result.TotalSeconds
	result.SnapshotMBPerSec = float64(result.ArrowBufferBytes) / 1e6 / result.SnapshotSeconds
	return result, nil
}

func monitorSource(ctx context.Context, pool *pgxpool.Pool, application, slot string) measurement {
	var result measurement
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return result
		case <-ticker.C:
			var connections, active int
			var retained int64
			err := pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE backend_type = 'client backend' AND state = 'active')
				FROM pg_stat_activity WHERE application_name = $1`, application).Scan(&connections, &active)
			if err != nil {
				if ctx.Err() == nil {
					result.MonitoringFailures++
				}
				continue
			}
			err = pool.QueryRow(ctx, `SELECT COALESCE(max(pg_wal_lsn_diff(
				CASE WHEN pg_is_in_recovery() THEN pg_last_wal_replay_lsn() ELSE pg_current_wal_lsn() END,
				restart_lsn)), 0)::bigint FROM pg_replication_slots WHERE slot_name = $1`, slot).Scan(&retained)
			if err != nil && ctx.Err() == nil {
				result.MonitoringFailures++
			}
			result.PeakConnections = max(result.PeakConnections, connections)
			result.PeakActiveQueries = max(result.PeakActiveQueries, active)
			result.PeakRetainedWAL = max(result.PeakRetainedWAL, retained)
		}
	}
}

func redactURIError(err error, uri string) string {
	message := strings.ReplaceAll(err.Error(), uri, "<source URI>")
	if parsed, parseErr := url.Parse(uri); parseErr == nil && parsed.User != nil {
		message = strings.ReplaceAll(message, parsed.User.String(), "<redacted-user>")
		if password, ok := parsed.User.Password(); ok && password != "" {
			message = strings.ReplaceAll(message, password, "<redacted>")
			message = strings.ReplaceAll(message, url.QueryEscape(password), "<redacted>")
		}
	}
	return message
}
