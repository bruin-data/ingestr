package fluxx

import (
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
)

// TestFluxxByteCap is the drop-in regression proof of the MaxBatchBytes cap: with
// the cap off all rows land in one batch; with a small cap the same rows split
// across many batches with no row lost. (Reuses the package test helpers; see
// fluxx_test.go for the wider byte-accounting coverage.)
func TestFluxxByteCap(t *testing.T) {
	const total = 50
	srv := fluxxTestServer(t, total, 2048)
	defer srv.Close()

	_, offRows := readAllFluxx(t, srv, source.ReadOptions{PageSize: 1_000_000, MaxBatchBytes: 0})
	_, onRows := readAllFluxx(t, srv, source.ReadOptions{PageSize: 1_000_000, MaxBatchBytes: 4096})

	if len(offRows) != 1 {
		t.Fatalf("cap-off batches=%d want 1", len(offRows))
	}
	if len(onRows) <= 1 {
		t.Fatalf("cap-on batches=%d want >1", len(onRows))
	}
	if sumInts(offRows) != sumInts(onRows) || sumInts(offRows) != total {
		t.Fatalf("row mismatch off=%d on=%d want %d", sumInts(offRows), sumInts(onRows), total)
	}
}
