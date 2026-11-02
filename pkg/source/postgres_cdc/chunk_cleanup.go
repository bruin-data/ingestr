package postgres_cdc

import (
	"context"
	"fmt"
)

type chunkRetiredEpoch struct {
	Epoch    string `json:"epoch"`
	Consumed uint64 `json:"consumed"`
}

func chunkEpochsToRetire(state *chunkBootstrapState) []chunkRetiredEpoch {
	if state == nil {
		return nil
	}
	retired := append([]chunkRetiredEpoch(nil), state.RetiredEpochs...)
	if state.Mode == "destination" {
		retired = append(retired, chunkRetiredEpoch{Epoch: state.Epoch, Consumed: state.Consumed})
	}
	return retired
}

func (r *chunkReader) cleanupRetiredEpochs(ctx context.Context) error {
	for _, retired := range r.state.RetiredEpochs {
		if retired.Epoch == "" || retired.Epoch == r.state.Epoch {
			return fmt.Errorf("invalid retired snapshot epoch")
		}
	}
	for _, retired := range r.state.RetiredEpochs {
		journal := &chunkJournal{store: r.options.Store, epoch: retired.Epoch}
		if err := journal.load(ctx, retired.Consumed); err != nil {
			return err
		}
		// Tail can hold an orphan page whose manifest write was interrupted.
		for sequence := journal.manifest.Head; ; sequence++ {
			if err := journal.store.Delete(ctx, journal.pageKey(sequence)); err != nil {
				return err
			}
			if sequence == journal.manifest.Tail {
				break
			}
		}
		if err := journal.store.Delete(ctx, journal.manifestKey()); err != nil {
			return err
		}
	}
	r.state.RetiredEpochs = nil
	return nil
}
