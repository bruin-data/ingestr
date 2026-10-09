// Package reverseetltest exercises the common reverse-ETL write contract while
// leaving HTTP protocols and rejection payloads to vendor-owned test adapters.
package reverseetltest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stretchr/testify/require"
)

type Writer interface {
	Write(context.Context, <-chan source.RecordBatchResult, destination.WriteOptions) error
	WriteParallel(context.Context, <-chan source.RecordBatchResult, destination.WriteOptions) error
}

type Scenario struct {
	Strategy   string
	RejectMode string
	Reject     bool
	Mixed      bool
	PageError  bool
	Empty      bool
}

// Effects separates successful wire operations from attempted writes. Adapters
// synchronize access because WriteParallel can call their server concurrently.
type Effects struct {
	Created, Updated, Upserted, Deleted []string
	Attempted                           []string
	Pages                               []string
}

type Fixture struct {
	Writer   Writer
	Options  destination.WriteOptions
	Snapshot func() Effects
}

func Run(t *testing.T, factory func(*testing.T, Scenario) Fixture) {
	t.Helper()
	t.Run("WriteParallel/source-error-with-idle-producer", func(t *testing.T) {
		f := factory(t, Scenario{Strategy: "merge"})
		f.Options.Strategy = "merge"
		f.Options.Parallelism = 3
		ch := make(chan source.RecordBatchResult, 1)
		cause := errors.New("source failed")
		ch <- source.RecordBatchResult{Err: cause}
		done := make(chan error, 1)
		go func() { done <- f.Writer.WriteParallel(context.Background(), ch, f.Options) }()
		select {
		case err := <-done:
			close(ch)
			require.ErrorIs(t, err, cause)
		case <-time.After(2 * time.Second):
			close(ch)
			<-done
			t.Fatal("WriteParallel waits for the producer to close after a source error; the strategy cannot cancel the producer until WriteParallel returns")
		}
	})
	t.Run("WriteParallel/fail-fast-with-idle-producer", func(t *testing.T) {
		f := factory(t, Scenario{Strategy: "merge", RejectMode: "fail_fast", Reject: true})
		f.Options.Strategy, f.Options.RejectMode = "merge", "fail_fast"
		f.Options.Parallelism = 3
		alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
		t.Cleanup(func() { alloc.AssertSize(t, 0) })
		// Fewer batches than workers leaves at least one waiting for input.
		ch := make(chan source.RecordBatchResult, 2)
		for _, key := range []string{"alpha", "beta"} {
			b := array.NewRecordBuilder(alloc, arrow.NewSchema([]arrow.Field{{Name: "source_key", Type: arrow.BinaryTypes.String}}, nil))
			b.Field(0).(*array.StringBuilder).Append(key)
			ch <- source.RecordBatchResult{Batch: b.NewRecordBatch()}
			b.Release()
		}
		done := make(chan error, 1)
		go func() { done <- f.Writer.WriteParallel(context.Background(), ch, f.Options) }()
		var err error
		select {
		case err = <-done:
			close(ch)
		case <-time.After(2 * time.Second):
			close(ch)
			err = <-done
			t.Error("WriteParallel waits for the producer to close after an API rejection")
		}
		// Release only batches the workers never received, as the strategy does.
		for result := range ch {
			result.Batch.Release()
		}
		require.ErrorContains(t, err, "bad row")
		e := f.Snapshot()
		require.NotEmpty(t, e.Attempted)
		require.Empty(t, e.Upserted)
	})
	for _, parallel := range []bool{false, true} {
		name := "Write"
		if parallel {
			name = "WriteParallel"
		}
		t.Run(name, func(t *testing.T) {
			cases := []Scenario{
				{Strategy: "append"},
				{Strategy: "merge"},
				{Strategy: "update"},
				{Strategy: "delete"},
				{Strategy: "replace"},
				{Strategy: "replace", Empty: true},
				{Strategy: "replace", PageError: true},
			}
			for _, strategy := range []string{"append", "merge", "update", "replace"} {
				for _, mode := range []string{"fail", "skip", "fail_fast"} {
					cases = append(cases, Scenario{Strategy: strategy, RejectMode: mode, Reject: true})
					if mode != "fail_fast" {
						cases = append(cases, Scenario{Strategy: strategy, RejectMode: mode, Reject: true, Mixed: true})
					}
				}
			}
			for _, tc := range cases {
				t.Run(tc.Strategy+"/"+tc.RejectMode+"/"+suffix(tc), func(t *testing.T) {
					f := factory(t, tc)
					f.Options.Strategy, f.Options.RejectMode = tc.Strategy, tc.RejectMode
					f.Options.Parallelism = 3
					if tc.RejectMode == "fail_fast" {
						// A single worker makes the unsent next batch deterministic.
						f.Options.Parallelism = 1
					}
					alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
					t.Cleanup(func() { alloc.AssertSize(t, 0) })
					ch := make(chan source.RecordBatchResult, 3)
					if !tc.Empty {
						for _, keys := range [][]string{{"alpha", "beta"}, {"gamma"}} {
							b := array.NewRecordBuilder(alloc, arrow.NewSchema([]arrow.Field{{Name: "source_key", Type: arrow.BinaryTypes.String}}, nil))
							b.Field(0).(*array.StringBuilder).AppendValues(keys, nil)
							ch <- source.RecordBatchResult{Batch: b.NewRecordBatch()}
							b.Release()
						}
					}
					close(ch)
					write := f.Writer.Write
					if parallel {
						write = f.Writer.WriteParallel
					}
					err := write(context.Background(), ch, f.Options)
					// On abort the strategy owns draining the producer, not the destination.
					for result := range ch {
						if result.Batch != nil {
							result.Batch.Release()
						}
					}
					if tc.PageError {
						require.ErrorContains(t, err, "page denied")
					} else if tc.Reject && tc.RejectMode != "skip" {
						require.ErrorContains(t, err, "bad row")
					} else {
						require.NoError(t, err)
					}
					e := f.Snapshot()
					if tc.Reject {
						require.Contains(t, e.Attempted, "alpha")
						if tc.RejectMode == "fail_fast" {
							require.NotContains(t, e.Attempted, "gamma")
						} else {
							require.Contains(t, e.Attempted, "gamma")
						}
					}
					if tc.Empty || (tc.Reject && !tc.Mixed) {
						require.Empty(t, e.Created)
						require.Empty(t, e.Updated)
						require.Empty(t, e.Upserted)
						require.Empty(t, e.Deleted)
						require.Empty(t, e.Pages)
						return
					}
					keys := []string{"alpha", "beta", "gamma"}
					if tc.Mixed {
						keys = []string{"alpha", "gamma"}
					}
					switch tc.Strategy {
					case "append":
						require.ElementsMatch(t, keys, e.Created)
					case "update":
						require.ElementsMatch(t, keys, e.Updated)
					case "merge", "replace":
						require.ElementsMatch(t, keys, e.Upserted)
					}
					if tc.Strategy != "append" {
						require.Empty(t, e.Created)
					}
					if tc.Strategy != "update" {
						require.Empty(t, e.Updated)
					}
					if tc.Strategy != "merge" && tc.Strategy != "replace" {
						require.Empty(t, e.Upserted)
					}
					switch tc.Strategy {
					case "delete":
						require.ElementsMatch(t, keys, e.Deleted)
					case "replace":
						if tc.Mixed && tc.RejectMode == "fail" {
							require.Empty(t, e.Pages)
							require.Empty(t, e.Deleted)
							return
						}
						require.Equal(t, []string{"first", "second"}, e.Pages)
						if tc.PageError {
							require.Empty(t, e.Deleted)
						} else {
							require.ElementsMatch(t, []string{"stale-first", "stale-second"}, e.Deleted)
						}
					default:
						require.Empty(t, e.Deleted)
					}
					if tc.Strategy != "replace" {
						require.Empty(t, e.Pages)
					}
				})
			}
		})
	}
}

func suffix(s Scenario) string {
	if s.Empty {
		return "empty"
	}
	if s.PageError {
		return "page-error"
	}
	if s.Mixed {
		return "mixed-rejections"
	}
	if s.Reject {
		return "all-rejected"
	}
	return "success"
}
