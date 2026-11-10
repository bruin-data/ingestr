package strategy

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/destination"
)

func canDirectStreamSnapshot(dest destination.Destination, cfg *config.IngestConfig, st *streamTableState) bool {
	return destination.SupportsDirectCDCSnapshot(dest) && st.isCDC && len(st.primaryKeys) > 0 &&
		st.incrementalPredicate == "" && !cfg.KeepStaging && !cfg.CDCChunkedSnapshot &&
		len(cfg.Mask) == 0 && strings.TrimSpace(cfg.Columns) == ""
}

func (l *flushLoop) prepareDeferredStaging(ctx context.Context, st *streamTableState) error {
	if err := connectorLeaseLoss(ctx); err != nil {
		return err
	}
	if err := l.dest.PrepareTable(ctx, destination.PrepareOptions{
		Table: st.stagingTable, Schema: st.schema, DropFirst: true,
		CDCMode: st.isCDC, CDCKeys: st.primaryKeys, PartitionBy: st.partitionBy,
		ClusterBy: st.clusterBy, ExpiresAfter: destination.ManagedStagingTTL,
	}); err != nil {
		return fmt.Errorf("failed to prepare CDC staging table %s: %w", st.stagingTable, err)
	}
	st.stagingDeferred = false
	output.Statusf("[STREAM] %s | Using staging table: %s\n", time.Now().Format("15:04:05"), st.stagingTable)
	return connectorLeaseLoss(ctx)
}
