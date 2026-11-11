package postgres_cdc

import (
	"context"
	"fmt"
	"slices"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5"
)

func (s *Snapshot) validateProgressIdentity(ctx context.Context, tx pgx.Tx) error {
	if s.progress.Incarnation != "" {
		current, err := tableIncarnation(ctx, tx, s.tableName)
		if err != nil {
			return err
		}
		if current != s.progress.Incarnation {
			return fmt.Errorf("snapshot source table %s was replaced before reading", s.tableName)
		}
	}
	if s.progress.SchemaFingerprint != "" {
		current, err := tableSchemaFingerprint(ctx, tx, s.tableName)
		if err != nil {
			return err
		}
		if current != s.progress.SchemaFingerprint {
			return fmt.Errorf("snapshot source schema for %s changed before reading", s.tableName)
		}
	}
	return nil
}

func (s *Snapshot) readProgressSnapshot(ctx context.Context, name string, lsn pglogrepl.LSN, results chan<- source.RecordBatchResult, opts source.ReadOptions) error {
	progress := s.progress.Clone()
	boundary, err := pglogrepl.ParseLSN(progress.Boundary)
	if err != nil || boundary == 0 || progress.Attempt == "" {
		return fmt.Errorf("snapshot progress for %s requires a valid boundary and attempt", s.tableName)
	}
	var ranges []snapshotRange
	if s.resuming {
		if len(progress.Ranges) == 0 {
			return fmt.Errorf("snapshot resume for %s requires a saved range plan", s.tableName)
		}
		ranges = snapshotRangesFromProgress(progress.Ranges)
	}
	return s.readBoundedSnapshot(ctx, boundedSnapshotOptions{
		name: name, boundary: lsn, ranges: ranges, read: opts, budget: s.readBudget, normalSnapshot: true,
		started: func() error {
			if !opts.CDCSnapshotReplace {
				return nil
			}
			return sendResult(ctx, results, source.RecordBatchResult{
				Truncate: true, CDCSnapshotPosition: progress.Boundary, CDCSnapshotResumed: s.resuming,
			})
		},
		progress: func(ctx context.Context, ranges []snapshotRange) error {
			current := progress
			current.Key = ""
			current.Ranges = snapshotRangesToProgress(ranges)
			return sendResult(ctx, results, source.RecordBatchResult{CommitToken: source.CDCStateCommitToken{
				SnapshotProgress: map[string]source.CDCSnapshotProgress{s.tableName: current},
			}})
		},
	}, results)
}

func snapshotRangesFromProgress(progress []source.CDCSnapshotRangeProgress) []snapshotRange {
	ranges := make([]snapshotRange, len(progress))
	for i, r := range progress {
		ranges[i] = snapshotRange{ID: r.ID, After: slices.Clone(r.After), Upper: slices.Clone(r.Upper), LastKey: slices.Clone(r.Key), Complete: r.Complete}
	}
	return ranges
}

func snapshotRangesToProgress(ranges []snapshotRange) []source.CDCSnapshotRangeProgress {
	progress := make([]source.CDCSnapshotRangeProgress, len(ranges))
	for i, r := range ranges {
		progress[i] = source.CDCSnapshotRangeProgress{ID: r.ID, After: slices.Clone(r.After), Upper: slices.Clone(r.Upper), Key: slices.Clone(r.LastKey), Complete: r.Complete}
	}
	return progress
}
