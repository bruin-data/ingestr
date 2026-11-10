package postgres_cdc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/source"
)

type snapshotSpoolRetired struct {
	Epoch     string `json:"epoch"`
	Mode      string `json:"mode"`
	Directory string `json:"directory,omitempty"`
	Identity  string `json:"identity"`
	Consumed  uint64 `json:"consumed"`
}

func retireSnapshotSpool(state *snapshotSpoolState) []snapshotSpoolRetired {
	if state == nil {
		return nil
	}
	retired := append([]snapshotSpoolRetired(nil), state.Retired...)
	if !state.Reset && state.Mode != "inmemory" {
		retired = append(retired, snapshotSpoolRetired{
			Epoch: state.Epoch, Mode: state.Mode, Directory: state.Directory,
			Identity: state.Identity, Consumed: state.Consumed,
		})
	}
	return retired
}

func (r *snapshotSpoolReader) cleanupRetiredSpools(ctx context.Context) error {
	if len(r.state.Retired) == 0 {
		return nil
	}
	if !r.state.CopyComplete {
		return fmt.Errorf("cannot retire normal snapshot WAL before its replacement snapshot is durable")
	}
	for _, retired := range r.state.Retired {
		if retired.Epoch == r.state.Epoch {
			return fmt.Errorf("cannot retire the active normal snapshot WAL epoch")
		}
	}
	var pending []snapshotSpoolRetired
	for _, retired := range r.state.Retired {
		if err := errors.Join(source.ConnectorLeaseLoss(ctx), ctx.Err()); err != nil {
			return err
		}
		if err := cleanupRetiredSnapshotSpool(ctx, r.options.Store, retired); err != nil {
			if stopped := errors.Join(source.ConnectorLeaseLoss(ctx), ctx.Err()); stopped != nil {
				return stopped
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, source.ErrConnectorLeaseLost) {
				return err
			}
			output.Warnf("[CDC] Could not clean up retired snapshot WAL epoch %s; keeping it for a later retry: %v\n", retired.Epoch, err)
			pending = append(pending, retired)
		}
	}
	if err := errors.Join(source.ConnectorLeaseLoss(ctx), ctx.Err()); err != nil {
		return err
	}
	if len(pending) == len(r.state.Retired) {
		return nil
	}
	updated := r.state.clone()
	updated.Retired = pending
	commit := &snapshotSpoolCommit{store: r.options.Store, state: updated}
	if err := commit.persist(ctx); err != nil {
		return err
	}
	r.state.Retired = pending
	return nil
}

func cleanupRetiredSnapshotSpool(ctx context.Context, store source.CDCChunkStore, retired snapshotSpoolRetired) error {
	if retired.Epoch == "" || retired.Mode != "inmemory" && retired.Mode != "destination" && retired.Mode != "disk" {
		return fmt.Errorf("invalid retired normal snapshot WAL epoch")
	}
	if retired.Mode == "inmemory" {
		return nil
	}
	if retired.Identity == "" {
		return fmt.Errorf("retired normal snapshot WAL has no identity")
	}
	if retired.Mode == "disk" {
		var err error
		store, err = newSnapshotDiskStore(retired.Directory, retired.Identity)
		if err != nil {
			return err
		}
	}
	journal := &chunkJournal{store: store, epoch: "normal-" + retired.Epoch}
	data, err := store.Load(ctx, journal.manifestKey())
	if err != nil {
		return err
	}
	if data == nil {
		journal.manifest = chunkJournalManifest{Version: 1, Head: retired.Consumed, Tail: retired.Consumed}
	} else {
		if err := json.Unmarshal(data, &journal.manifest); err != nil {
			return fmt.Errorf("invalid retired normal snapshot WAL manifest: %w", err)
		}
		if journal.manifest.Version != 1 || journal.manifest.Head > journal.manifest.Tail || retired.Consumed > journal.manifest.Tail {
			return fmt.Errorf("invalid retired normal snapshot WAL manifest")
		}
	}
	// A completed prune can precede a crash that leaves this retirement pending.
	// Its manifest head may therefore be beyond the saved consumed position.
	if err := journal.prune(ctx, retired.Consumed, journal.manifest.Tail); err != nil {
		return err
	}
	// An interrupted append can leave an unpublished page at the manifest tail.
	if err := store.Delete(ctx, journal.pageKey(journal.manifest.Tail)); err != nil {
		return err
	}
	return store.Delete(ctx, journal.manifestKey())
}
