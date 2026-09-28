package multitable

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
)

func TestRouterSourceErrorReachesEveryConsumer(t *testing.T) {
	for _, drain := range []bool{false, true} {
		t.Run(map[bool]string{false: "without_input_drain", true: "with_input_drain"}[drain], func(t *testing.T) {
			router := NewRouter([]string{"full", "empty"}, 1)
			want := errors.New("source failed")
			var buffered, failed, trailing atomic.Int64
			input := make(chan source.RecordBatchResult, 3)
			input <- source.RecordBatchResult{TableName: "full", Batch: &releaseCountingBatch{releases: &buffered}}
			input <- source.RecordBatchResult{Err: want, Batch: &releaseCountingBatch{releases: &failed}}
			if drain {
				input <- source.RecordBatchResult{Batch: &releaseCountingBatch{releases: &trailing}}
			}
			close(input)
			router.Route(context.Background(), input, drain)
			waitForRouter(t, router)
			if !errors.Is(router.Err(), want) {
				t.Fatalf("Err() = %v, want %v", router.Err(), want)
			}
			for _, table := range []string{"full", "empty"} {
				ch := router.GetChannel(table)
				result, ok := <-ch
				if !ok || !errors.Is(result.Err, want) || result.Batch != nil {
					t.Fatalf("table %s: got %+v, open=%v; want source error", table, result, ok)
				}
				if _, ok := <-ch; ok {
					t.Fatalf("table %s: channel remains open after terminal error", table)
				}
			}
			if buffered.Load() != 1 || failed.Load() != 1 || (drain && trailing.Load() != 1) {
				t.Fatalf("release counts: buffered=%d failed=%d trailing=%d", buffered.Load(), failed.Load(), trailing.Load())
			}
		})
	}
}

func TestRouterCancellationReleasesBufferedAndBlockedBatches(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	router := NewRouter([]string{"items", "empty"}, 1)
	input := make(chan source.RecordBatchResult)
	var buffered, blocked atomic.Int64
	router.Route(ctx, input, false)
	for _, releases := range []*atomic.Int64{&buffered, &blocked} {
		select {
		case input <- source.RecordBatchResult{TableName: "items", Batch: &releaseCountingBatch{releases: releases}}:
		case <-time.After(5 * time.Second):
			t.Fatal("router did not accept batch")
		}
	}
	cancel()
	waitForRouter(t, router)
	if !errors.Is(router.Err(), context.Canceled) {
		t.Fatalf("Err() = %v, want cancellation", router.Err())
	}
	for _, table := range []string{"items", "empty"} {
		if result, ok := <-router.GetChannel(table); ok {
			t.Fatalf("table %s: unexpected result after cancellation: %+v", table, result)
		}
	}
	if buffered.Load() != 1 || blocked.Load() != 1 {
		t.Fatalf("release counts: buffered=%d blocked=%d", buffered.Load(), blocked.Load())
	}
}

func TestWriteSourceErrorCancelsProducer(t *testing.T) {
	input := make(chan source.RecordBatchResult, 1)
	input <- source.RecordBatchResult{Err: errors.New("source failed")}
	var cancellations atomic.Int64
	err := Write(context.Background(), &fakeDestination{}, input, destination.MultiTableWriteOptions{
		TableConfigs: map[string]destination.TableWriteConfig{
			"first":  {DestTable: "first"},
			"second": {DestTable: "second"},
		},
		CancelSource: func() {
			cancellations.Add(1)
			close(input)
		},
	})
	if err == nil || !strings.Contains(err.Error(), "source failed") {
		t.Fatalf("Write() = %v, want source failure", err)
	}
	if got := cancellations.Load(); got != 1 {
		t.Fatalf("source cancellations = %d, want 1", got)
	}
}

func waitForRouter(t *testing.T, router *Router) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		router.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("router did not finish")
	}
}
