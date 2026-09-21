package strategy

import (
	"context"
	"fmt"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
)

// UpdateStrategy updates existing records only (never creates). Reverse-ETL only:
// the destination must implement destination.ReverseETLDestination.
type UpdateStrategy struct{}

func (s *UpdateStrategy) Name() config.IncrementalStrategy { return config.StrategyUpdate }

func (s *UpdateStrategy) Validate(cfg *config.IngestConfig) error {
	return validateReverseETLReject(cfg)
}

// RequiresPrimaryKey is false: the destination defaults the match key (e.g.
// hs_object_id) when no --primary-key is given.
func (s *UpdateStrategy) RequiresPrimaryKey() bool { return false }

func (s *UpdateStrategy) RequiresIncrementalKey() bool { return false }

func (s *UpdateStrategy) Execute(ctx context.Context, job *IngestionJob) error {
	// Reverse-ETL destinations update via their API; SQL support goes in
	// executeSQL below (seam).
	if destination.IsReverseETL(job.Destination) {
		return executeReverseETL(ctx, job, config.StrategyUpdate)
	}
	return s.executeSQL(ctx, job)
}

// executeSQL is the native (warehouse) update path. Not implemented yet — the
// strategy is reverse-ETL-only for now, but this is the seam to add it.
func (s *UpdateStrategy) executeSQL(_ context.Context, job *IngestionJob) error {
	return fmt.Errorf("the %q strategy is not yet supported for %s destinations", config.StrategyUpdate, job.Destination.GetScheme())
}
