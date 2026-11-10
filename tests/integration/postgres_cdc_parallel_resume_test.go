//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/registry"
	"github.com/bruin-data/ingestr/pkg/destination"
	postgresdest "github.com/bruin-data/ingestr/pkg/destination/postgres"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errParallelResumeCrash = errors.New("injected crash after snapshot progress persistence")

const parallelResumeSourceTable = "items"

type parallelResumeCheckpoint struct {
	Version  int                               `json:"v"`
	Boundary string                            `json:"b"`
	Attempt  string                            `json:"a"`
	Ranges   []source.CDCSnapshotRangeProgress `json:"r"`
}

type parallelResumeEvent struct {
	id       string
	position string
	progress parallelResumeCheckpoint
}

type parallelResumeGate struct {
	mu       sync.Mutex
	after    string
	armed    bool
	failed   bool
	observed []parallelResumeCheckpoint
	captured chan parallelResumeEvent
}

var parallelResumeGates sync.Map

type parallelResumePostgres struct {
	*postgresdest.PostgresDestination
	gate *parallelResumeGate
}

func init() {
	registry.Default.RegisterDestination([]string{"parallelresumepostgres"}, func() interface{} {
		return &parallelResumePostgres{PostgresDestination: postgresdest.NewPostgresDestination()}
	})
}

func (d *parallelResumePostgres) Schemes() []string { return []string{"parallelresumepostgres"} }

func (d *parallelResumePostgres) Connect(ctx context.Context, raw string) error {
	uri, err := url.Parse(raw)
	if err != nil {
		return err
	}
	params := uri.Query()
	gate, ok := parallelResumeGates.Load(params.Get("parallel_resume_gate"))
	if !ok {
		return fmt.Errorf("parallel snapshot resume test gate is missing")
	}
	d.gate = gate.(*parallelResumeGate)
	params.Del("parallel_resume_gate")
	uri.RawQuery = params.Encode()
	uri.Scheme = "postgres"
	return d.PostgresDestination.Connect(ctx, uri.String())
}

func (d *parallelResumePostgres) WriteCDCState(ctx context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	var batches []source.RecordBatchResult
	var events []parallelResumeEvent
	for result := range records {
		batches = append(batches, result)
		if result.Batch == nil {
			continue
		}
		column := func(name string) *array.String {
			return result.Batch.Column(result.Batch.Schema().FieldIndices(name)[0]).(*array.String)
		}
		ids, tables, kinds := column("event_id"), column("source_table"), column("state_kind")
		statuses, positions := column("state_status"), column("_cdc_lsn")
		for row := 0; row < int(result.Batch.NumRows()); row++ {
			if tables.Value(row) != parallelResumeSourceTable || kinds.Value(row) != "snapshot_progress" || statuses.Value(row) != "in_progress" {
				continue
			}
			var progress parallelResumeCheckpoint
			if json.Unmarshal([]byte(positions.Value(row)), &progress) == nil && progress.Version == 2 && len(progress.Ranges) > 1 {
				events = append(events, parallelResumeEvent{id: strings.Clone(ids.Value(row)), position: strings.Clone(positions.Value(row)), progress: progress})
			}
		}
	}
	d.gate.mu.Lock()
	failed := d.gate.failed
	d.gate.mu.Unlock()
	if failed {
		for _, result := range batches {
			if result.Batch != nil {
				result.Batch.Release()
			}
		}
		return errParallelResumeCrash
	}
	forward := make(chan source.RecordBatchResult, len(batches))
	for _, result := range batches {
		forward <- result
	}
	close(forward)
	if err := d.WriteParallel(ctx, forward, opts); err != nil {
		return err
	}
	d.gate.mu.Lock()
	defer d.gate.mu.Unlock()
	for _, event := range events {
		if event.progress.Boundary <= d.gate.after {
			continue
		}
		d.gate.observed = append(d.gate.observed, event.progress)
		if d.gate.armed {
			d.gate.failed = true
			d.gate.captured <- event
			return errParallelResumeCrash
		}
	}
	return nil
}

func TestPostgresCDCParallelSnapshotDurableResume(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL logical replication and durable destination state")
	}
	for _, allComplete := range []bool{false, true} {
		name := "high_range_complete_low_range_unfinished"
		if allComplete {
			name = "all_ranges_complete_without_table_completion"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			t.Cleanup(cancel)
			container, sourceURI := setupPostgresCDCContainerImage(t, ctx, "postgres:17-alpine")
			t.Cleanup(func() { _ = container.Terminate(context.Background()) })
			src, err := pgxpool.New(ctx, sourceURI)
			require.NoError(t, err)
			t.Cleanup(src.Close)
			_, err = src.Exec(ctx, "CREATE DATABASE parallel_resume_target")
			require.NoError(t, err)
			destinationURI, err := url.Parse(sourceURI)
			require.NoError(t, err)
			destinationURI.Path = "/parallel_resume_target"
			dst, err := pgxpool.New(ctx, destinationURI.String())
			require.NoError(t, err)
			t.Cleanup(dst.Close)
			const totalRows = 800
			_, err = src.Exec(ctx, fmt.Sprintf(`
				CREATE TABLE public.items (id BIGINT PRIMARY KEY, payload TEXT NOT NULL, dropme TEXT);
				INSERT INTO public.items SELECT g, 'payload-' || g, 'old-' || g FROM generate_series(1, %d) g;
				CREATE TABLE public.anchor (id BIGINT PRIMARY KEY, payload TEXT);
				INSERT INTO public.anchor VALUES (1, 'anchor');
				CREATE PUBLICATION parallel_resume_pub FOR TABLE public.items, public.anchor;
			`, totalRows))
			require.NoError(t, err)
			gateID := "parallel_resume_" + strings.ToLower(uniqueSuffix())
			gate := &parallelResumeGate{captured: make(chan parallelResumeEvent, 1)}
			parallelResumeGates.Store(gateID, gate)
			t.Cleanup(func() { parallelResumeGates.Delete(gateID) })
			params := destinationURI.Query()
			params.Set("parallel_resume_gate", gateID)
			destinationURI.RawQuery = params.Encode()
			destinationURI.Scheme = "parallelresumepostgres"
			cfg := config.DefaultConfig()
			cfg.SourceURI = strings.Replace(sourceURI, "postgres://", "postgres+cdc://", 1) + "&publication=parallel_resume_pub&slot=parallel_resume_slot&dest_schema=landing"
			cfg.SourceTables = []string{"public.items", "public.anchor"}
			cfg.DestURI = destinationURI.String()
			cfg.CDCTableNaming = config.TableNamingTable
			cfg.SchemaNaming = "direct"
			cfg.IncrementalStrategy = config.StrategyMerge
			cfg.CDCDeleteBehavior = config.CDCDeleteHard
			cfg.Stream = true
			cfg.ExtractParallelism = 4
			cfg.DestinationParallelism = 1
			cfg.FlushInterval = 50 * time.Millisecond
			cfg.FlushRecords = 10
			cfg.PageSize = 10
			cfg.NoLoadTimestamp, cfg.NoRunID = true, true
			start := func() (context.CancelFunc, <-chan error) {
				runCtx, stop := context.WithCancel(ctx)
				done := make(chan error, 1)
				runConfig := *cfg
				go func() {
					defer close(done)
					done <- pipeline.New(&runConfig).Run(runCtx)
				}()
				t.Cleanup(func() {
					stop()
					select {
					case <-done:
					case <-time.After(30 * time.Second):
						t.Error("snapshot pipeline did not stop")
					}
				})
				return stop, done
			}
			running := func(done <-chan error) {
				select {
				case err := <-done:
					t.Fatalf("snapshot pipeline stopped: %v", err)
				default:
				}
			}
			stop, done := start()
			require.EventuallyWithT(t, func(collect *assert.CollectT) {
				running(done)
				var count, completed int
				require.NoError(collect, dst.QueryRow(ctx, "SELECT count(*) FROM landing.items").Scan(&count))
				require.Equal(collect, totalRows, count)
				require.NoError(collect, dst.QueryRow(ctx, `SELECT count(*) FROM _bruin_staging.cdc_state
					WHERE source_table = $1 AND state_kind = 'snapshot' AND state_status = 'complete'`, parallelResumeSourceTable).Scan(&completed))
				require.Positive(collect, completed, "initial snapshot completion must be durable")
			}, 2*time.Minute, 20*time.Millisecond)
			var initialStamp string
			require.NoError(t, dst.QueryRow(ctx, "SELECT _cdc_lsn FROM landing.items WHERE id = 6").Scan(&initialStamp))
			gate.mu.Lock()
			gate.after, gate.armed = initialStamp, true
			gate.mu.Unlock()
			_, err = src.Exec(ctx, "ALTER TABLE public.items DROP COLUMN dropme; UPDATE public.items SET payload = payload WHERE id = 800")
			require.NoError(t, err)
			select {
			case err := <-done:
				require.ErrorContains(t, err, errParallelResumeCrash.Error())
			case <-ctx.Done():
				t.Fatal("replacement backfill did not reach the injected durable-progress crash")
			}
			stop()
			event := <-gate.captured
			require.Len(t, event.progress.Ranges, 4)
			require.NotEmpty(t, event.progress.Attempt)
			var interruptedStatus string
			require.NoError(t, dst.QueryRow(ctx, `SELECT state_status FROM _bruin_staging.cdc_state
				WHERE source_table = $1 AND state_kind = 'snapshot'
				ORDER BY state_generation DESC, recorded_at DESC LIMIT 1`, parallelResumeSourceTable).Scan(&interruptedStatus))
			require.NotEqual(t, "complete", interruptedStatus, "persisted range progress must not complete the table")
			var raw map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(event.position), &raw))
			var saved parallelResumeCheckpoint
			require.NoError(t, json.Unmarshal([]byte(event.position), &saved))
			lowUpper, err := strconv.ParseInt(saved.Ranges[0].Upper[0], 10, 64)
			require.NoError(t, err)
			highAfter, err := strconv.ParseInt(saved.Ranges[len(saved.Ranges)-1].After[0], 10, 64)
			require.NoError(t, err)
			lowCursor := lowUpper / 2
			require.Greater(t, lowCursor, int64(6))
			require.Less(t, highAfter+4, int64(totalRows))
			for i := range saved.Ranges {
				r := &saved.Ranges[i]
				r.Key, r.Complete = nil, false
				if allComplete || i == len(saved.Ranges)-1 {
					r.Complete = true
					r.Key = []string{fmt.Sprint(totalRows)}
					if r.Upper != nil {
						r.Key = append([]string(nil), r.Upper...)
					}
				} else if i == 0 {
					r.Key = []string{fmt.Sprint(lowCursor)}
				}
			}
			// PostgreSQL truncates the target at the replacement boundary. Restore
			// the unchanged snapshot rows certified by this crash checkpoint.
			tx, err := dst.Begin(ctx)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback(context.Background()) }()
			_, err = tx.Exec(ctx, "DELETE FROM landing.items")
			require.NoError(t, err)
			certifiedRows := lowCursor + int64(totalRows) - highAfter
			if allComplete {
				certifiedRows = int64(totalRows)
			}
			restored, err := tx.Exec(ctx, `
				INSERT INTO landing.items (id, payload, _cdc_lsn, _cdc_deleted, _cdc_synced_at)
				SELECT id, 'payload-' || id, $1, false, clock_timestamp()
				FROM generate_series(1, $2::bigint) AS snapshot_rows(id)
				WHERE $5::boolean OR id <= $3 OR id > $4
			`, saved.Boundary, totalRows, lowCursor, highAfter, allComplete)
			require.NoError(t, err)
			require.Equal(t, certifiedRows, restored.RowsAffected(), "every certified snapshot row must exist before resume")
			raw["r"], err = json.Marshal(saved.Ranges)
			require.NoError(t, err)
			position, err := json.Marshal(raw)
			require.NoError(t, err)
			checkpoint, err := tx.Exec(ctx, "UPDATE _bruin_staging.cdc_state SET _cdc_lsn = $1 WHERE event_id = $2", string(position), event.id)
			require.NoError(t, err)
			require.EqualValues(t, 1, checkpoint.RowsAffected())
			require.NoError(t, tx.Commit(ctx))

			_, err = src.Exec(ctx, fmt.Sprintf(`
				UPDATE public.items SET payload = 'updated-low' WHERE id = 2;
				DELETE FROM public.items WHERE id = 3;
				UPDATE public.items SET id = 900004 WHERE id = 4;
				UPDATE public.items SET payload = 'updated-high' WHERE id = %d;
				DELETE FROM public.items WHERE id = %d;
				UPDATE public.items SET id = 0 WHERE id = %d;
				INSERT INTO public.items VALUES (-1, 'inserted-behind-cursor'), (900001, 'inserted-high');
			`, highAfter+2, highAfter+3, highAfter+4))
			require.NoError(t, err)
			gate.mu.Lock()
			gate.armed, gate.failed, gate.observed = false, false, nil
			gate.mu.Unlock()
			cfg.ExtractParallelism = 2
			stop, done = start()
			_, err = src.Exec(ctx, "UPDATE public.items SET payload = 'during-resume-low' WHERE id = 1; INSERT INTO public.items VALUES (901001, 'during-resume')")
			require.NoError(t, err)
			want, err := parallelResumeRows(ctx, src, "public.items")
			require.NoError(t, err)
			require.EventuallyWithT(t, func(collect *assert.CollectT) {
				running(done)
				actual, err := parallelResumeRows(ctx, dst, "landing.items")
				require.NoError(collect, err)
				assert.Equal(collect, len(want), len(actual), "source and destination row counts must match")
				for id, payload := range want {
					actualPayload, exists := actual[id]
					if !assert.True(collect, exists, "source key %d is missing from the destination", id) ||
						!assert.Equal(collect, payload, actualPayload, "payload differs for source key %d", id) {
						break
					}
				}
				var status string
				err = dst.QueryRow(ctx, `SELECT state_status FROM _bruin_staging.cdc_state
					WHERE source_table = $1 AND state_kind = 'snapshot_progress'
					ORDER BY state_generation DESC, recorded_at DESC LIMIT 1`, parallelResumeSourceTable).Scan(&status)
				require.NoError(collect, err)
				assert.Equal(collect, "complete", status, "resumed snapshot progress must be durably retired")
			}, 2*time.Minute, 20*time.Millisecond, "WAL must repair changes behind unfinished and completed range cursors")
			stop()
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("resumed snapshot pipeline did not stop")
			}
			actual, err := parallelResumeRows(ctx, dst, "landing.items")
			require.NoError(t, err)
			require.Equal(t, want, actual, "compare every key and payload after WAL catch-up")
			for _, id := range []int64{6, highAfter + 1} {
				var stamp string
				require.NoError(t, dst.QueryRow(ctx, "SELECT _cdc_lsn FROM landing.items WHERE id = $1", id).Scan(&stamp))
				require.Equal(t, saved.Boundary, stamp, "durable rows must survive resume without being recopied")
			}
			if !allComplete {
				var stamp string
				require.NoError(t, dst.QueryRow(ctx, "SELECT _cdc_lsn FROM landing.items WHERE id = $1", lowCursor+1).Scan(&stamp))
				require.Greater(t, stamp, saved.Boundary, "unfinished low range must use the new read boundary")
			}
			var tombstones int
			require.NoError(t, dst.QueryRow(ctx, "SELECT count(*) FROM landing.items WHERE _cdc_deleted").Scan(&tombstones))
			require.Zero(t, tombstones)
			gate.mu.Lock()
			observed := append([]parallelResumeCheckpoint(nil), gate.observed...)
			gate.mu.Unlock()
			if !allComplete {
				require.NotEmpty(t, observed, "resuming unfinished ranges must persist the original plan")
			}
			for _, checkpoint := range observed {
				require.Equal(t, saved.Attempt, checkpoint.Attempt)
				require.Equal(t, saved.Boundary, checkpoint.Boundary)
				require.Len(t, checkpoint.Ranges, len(saved.Ranges))
				for i, r := range checkpoint.Ranges {
					require.Equal(t, saved.Ranges[i].ID, r.ID)
					require.Equal(t, saved.Ranges[i].After, r.After)
					require.Equal(t, saved.Ranges[i].Upper, r.Upper)
				}
			}
		})
	}
}

func parallelResumeRows(ctx context.Context, pool *pgxpool.Pool, table string) (map[int64]string, error) {
	rows, err := pool.Query(ctx, "SELECT id, payload FROM "+table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[int64]string)
	for rows.Next() {
		var id int64
		var payload string
		if err := rows.Scan(&id, &payload); err != nil {
			return nil, err
		}
		result[id] = payload
	}
	return result, rows.Err()
}
