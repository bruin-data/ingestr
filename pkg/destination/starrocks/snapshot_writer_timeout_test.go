package starrocks

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

func TestSnapshotWriterRecoversExpiredWindowOnlyAfterConfirmedAbort(t *testing.T) {
	for _, state := range []string{"ABORTED", "UNKNOWN"} {
		t.Run(state, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				d, server := newSnapshotTestDestination(t)
				d.snapshotLimits.requestTarget, d.snapshotLimits.transactionBytes = 1, 1
				d.snapshotLimits.retryInitial = 5 * time.Second
				d.snapshotLimits.retryMaximum = time.Minute
				d.snapshotLimits.retryTimeout = 5 * time.Minute
				pressureUntil := time.Now().Add(130 * time.Second)

				type lifetime struct {
					started, lastLoad time.Time
					timeout, idle     time.Duration
					aborted           bool
				}
				var mu sync.Mutex
				lifetimes := make(map[string]*lifetime)
				expired, stateChecks := 0, 0
				// Model the timeouts requested by begin; StarRocks removes expired
				// transactions from its load endpoint but retains their ABORTED state.
				d.loader.client.Transport = parallelLoadTransport(func(r *http.Request) (*http.Response, error) {
					mu.Lock()
					defer mu.Unlock()
					operation := strings.TrimPrefix(r.URL.Path, "/api/transaction/")
					label := r.Header.Get("label")
					if operation == "begin" {
						timeout, err := strconv.Atoi(r.Header.Get("timeout"))
						if err != nil {
							return nil, err
						}
						idle, err := strconv.Atoi(r.Header.Get("idle_transaction_timeout"))
						if err != nil {
							return nil, err
						}
						lifetimes[label] = &lifetime{
							started: time.Now(), lastLoad: time.Now(),
							timeout: time.Duration(timeout) * time.Second, idle: time.Duration(idle) * time.Second,
						}
						return server.request(r)
					}
					life := lifetimes[label]
					server.mu.Lock()
					tx := server.transactions[label]
					firstWindow := len(tx.rows) > 0 && tx.rows[0] == 0
					id := tx.id
					server.mu.Unlock()
					if operation == "load" || operation == "prepare" {
						if !life.aborted && (time.Since(life.lastLoad) >= life.idle || time.Since(life.started) >= life.timeout) {
							life.aborted = true
							expired++
							server.mu.Lock()
							tx.rolledBack = true
							server.mu.Unlock()
						}
					}
					if life.aborted {
						return transactionTestResponse(fmt.Sprintf(`{"Status":"FAILED","Message":"Transcation Not Exist","TxnId":%d}`, id)), nil
					}
					if operation == "prepare" && firstWindow && time.Now().Before(pressureUntil) {
						return transactionTestResponse(fmt.Sprintf(`{"Status":"FAILED","Message":"too many versions","TxnId":%d}`, id)), nil
					}
					response, err := server.request(r)
					if operation == "load" && err == nil {
						life.lastLoad = time.Now()
					}
					return response, err
				})
				d.loader.transactionState = func(_ context.Context, _ string, id int64) (string, error) {
					mu.Lock()
					defer mu.Unlock()
					server.mu.Lock()
					defer server.mu.Unlock()
					stateChecks++
					for label, tx := range server.transactions {
						if tx.id == id && lifetimes[label].aborted {
							return state, nil
						}
					}
					return "PREPARE", nil
				}

				mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
				records := make(chan source.RecordBatchResult, 3)
				for id := range 2 {
					records <- source.RecordBatchResult{Batch: snapshotLoadBatch(mem, int64(id), false, "[]"), CDCSnapshot: true}
				}
				records <- source.RecordBatchResult{CommitToken: source.CDCStateCommitToken{SnapshotProgress: map[string]source.CDCSnapshotProgress{"events": {Boundary: "1"}}}}
				close(records)
				opts := snapshotWriteOptions(1)
				checkpoints := 0
				opts.CDCSnapshotProgress = func(context.Context, source.CDCStateCommitToken) error {
					server.mu.Lock()
					defer server.mu.Unlock()
					var committed []int64
					for _, tx := range server.transactions {
						if tx.committed {
							committed = append(committed, tx.rows...)
						}
					}
					require.ElementsMatch(t, []int64{0, 1}, committed)
					require.Equal(t, server.commits, server.visible)
					checkpoints++
					return nil
				}
				err := d.writeSnapshot(t.Context(), records, opts)
				if state == "ABORTED" {
					require.NoError(t, err)
					require.Equal(t, 1, checkpoints)
				} else {
					require.ErrorContains(t, err, "Transcation Not Exist")
					require.Zero(t, checkpoints)
					require.Equal(t, 1, server.commits)
				}
				require.Equal(t, 1, expired, "the next window must outlive its transaction's requested timeout")
				require.Positive(t, stateChecks)
				mem.AssertSize(t, 0)
			})
		})
	}
}
