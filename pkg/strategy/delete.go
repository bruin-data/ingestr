package strategy

import (
	"context"
	"fmt"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
)

// DeleteStrategy removes (archives) the records identified by the source. Reverse-ETL
// only: the destination must implement destination.ReverseETLDestination.
type DeleteStrategy struct{}

func (s *DeleteStrategy) Name() config.IncrementalStrategy { return config.StrategyDelete }

func (s *DeleteStrategy) Validate(cfg *config.IngestConfig) error {
	return validateReverseETLReject(cfg)
}

// RequiresPrimaryKey is false: the destination defaults the match key (e.g.
// hs_object_id) when no --primary-key is given.
func (s *DeleteStrategy) RequiresPrimaryKey() bool { return false }

func (s *DeleteStrategy) RequiresIncrementalKey() bool { return false }

func (s *DeleteStrategy) Execute(ctx context.Context, job *IngestionJob) error {
	// Reverse-ETL destinations archive via their API; SQL support goes in
	// executeSQL below (seam).
	if destination.IsReverseETL(job.Destination) {
		return executeReverseETL(ctx, job, config.StrategyDelete)
	}
	return s.executeSQL(ctx, job)
}

// executeSQL is the native (warehouse) delete path. Not implemented yet — the
// strategy is reverse-ETL-only for now, but this is the seam to add it.
func (s *DeleteStrategy) executeSQL(_ context.Context, job *IngestionJob) error {
	return fmt.Errorf("the %q strategy is not yet supported for %s destinations", config.StrategyDelete, job.Destination.GetScheme())
}
