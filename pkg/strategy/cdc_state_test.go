package strategy

import (
	"context"
	"sync"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
)

type cdcStateDestination struct {
	*fakeDestination
	stateMu sync.Mutex
	maxLSN  map[string]string
}

func newCDCStateDestination() *cdcStateDestination {
	return &cdcStateDestination{fakeDestination: &fakeDestination{}, maxLSN: make(map[string]string)}
}

func (d *cdcStateDestination) WriteParallel(_ context.Context, records <-chan source.RecordBatchResult, opts destination.WriteOptions) error {
	for result := range records {
		if result.Err != nil {
			return result.Err
		}
		if result.Batch == nil {
			continue
		}
		positions := result.Batch.Column(5).(*array.String)
		d.stateMu.Lock()
		for i := 0; i < positions.Len(); i++ {
			if position := positions.Value(i); position > d.maxLSN[opts.Table] {
				d.maxLSN[opts.Table] = position
			}
		}
		d.stateMu.Unlock()
		result.Batch.Release()
	}
	return nil
}

func (d *cdcStateDestination) GetMaxCDCLSN(_ context.Context, table string) (string, error) {
	d.stateMu.Lock()
	defer d.stateMu.Unlock()
	return d.maxLSN[table], nil
}

func (d *cdcStateDestination) DropTable(ctx context.Context, table string) error {
	d.stateMu.Lock()
	delete(d.maxLSN, table)
	d.stateMu.Unlock()
	return d.fakeDestination.DropTable(ctx, table)
}

func TestCDCStateRequiresCompletedSnapshotBeforeResume(t *testing.T) {
	ctx := context.Background()
	dest := newCDCStateDestination()
	manager, err := NewCDCStateManager(dest, "0123456789abcdef", "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.RegisterTable(ctx, "public.orders", "raw.orders"); err != nil {
		t.Fatal(err)
	}

	if err := manager.Persist(ctx, source.CDCStateCommitToken{Position: "00000000/00000020"}); err != nil {
		t.Fatal(err)
	}
	position, err := manager.ResumePosition(ctx, "public.orders")
	if err != nil {
		t.Fatal(err)
	}
	if position != "" {
		t.Fatalf("checkpoint without a snapshot marker became resumable at %s", position)
	}

	if err := manager.Persist(ctx, source.CDCStateCommitToken{SnapshotPositions: map[string]string{
		"public.orders": "00000000/00000010",
	}}); err != nil {
		t.Fatal(err)
	}
	position, err = manager.ResumePosition(ctx, "public.orders")
	if err != nil {
		t.Fatal(err)
	}
	if want := "00000000/00000020"; position != want {
		t.Fatalf("resume position = %s, want latest checkpoint %s", position, want)
	}

	if manager.checkpoint != "_bruin_staging.cdc_checkpoint_0123456789abcdef" {
		t.Fatalf("unexpected checkpoint table: %s", manager.checkpoint)
	}
	if marker := manager.markerTables["public.orders"]; marker == "" || marker == manager.checkpoint {
		t.Fatalf("invalid snapshot marker table: %s", marker)
	}
}

func TestCDCStateConnectorAndTableNamesDoNotCollide(t *testing.T) {
	ctx := context.Background()
	dest := newCDCStateDestination()
	a, err := NewCDCStateManager(dest, "aaaaaaaaaaaaaaaa", "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewCDCStateManager(dest, "bbbbbbbbbbbbbbbb", "raw.orders", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, manager := range []*CDCStateManager{a, b} {
		if err := manager.RegisterTable(ctx, "db1.orders", "raw.orders"); err != nil {
			t.Fatal(err)
		}
		if err := manager.RegisterTable(ctx, "db2.orders", "raw.orders_archive"); err != nil {
			t.Fatal(err)
		}
	}

	if a.checkpoint == b.checkpoint {
		t.Fatal("different connectors share a checkpoint table")
	}
	if a.markerTables["db1.orders"] == a.markerTables["db2.orders"] {
		t.Fatal("different source tables share a snapshot marker table")
	}
}
