package iceberg

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
)

// CommitInfo describes the current Iceberg snapshot and its ingestion metrics.
type CommitInfo struct {
	SnapshotID       int64
	SequenceNumber   int64
	CommittedAt      time.Time
	Operation        string
	AddedRows        int64
	DeletedRows      int64
	TotalRows        int64
	PhysicalRows     int64
	AddedDataFiles   int64
	DeletedDataFiles int64
	CommitToken      string
	CDCResumeLSN     string
}

// GetCommitInfo returns the current committed snapshot metrics for a table.
func (d *Destination) GetCommitInfo(ctx context.Context, table string) (CommitInfo, error) {
	return d.getCommitInfo(ctx, table, true)
}

func (d *Destination) getCommitInfo(ctx context.Context, table string, logicalCount bool) (CommitInfo, error) {
	tbl, err := d.loadIcebergTable(ctx, table)
	if err != nil {
		return CommitInfo{}, err
	}
	snapshot := tbl.CurrentSnapshot()
	if snapshot == nil {
		return CommitInfo{}, nil
	}
	info := CommitInfo{
		SnapshotID:     snapshot.SnapshotID,
		SequenceNumber: snapshot.SequenceNumber,
		CommittedAt:    time.UnixMilli(snapshot.TimestampMs).UTC(),
		CDCResumeLSN:   latestCDCResumeLSN(tbl),
	}
	if snapshot.Summary == nil {
		return info, nil
	}
	info.Operation = snapshot.Summary.Properties["ingestr.operation"]
	if info.Operation == "" {
		info.Operation = string(snapshot.Summary.Operation)
	}
	info.AddedRows = snapshotMetric(snapshot.Summary.Properties, "added-records")
	info.DeletedRows = snapshotMetric(snapshot.Summary.Properties, "deleted-records")
	info.PhysicalRows = snapshotMetric(snapshot.Summary.Properties, "total-records")
	info.TotalRows = info.PhysicalRows
	info.AddedDataFiles = snapshotMetric(snapshot.Summary.Properties, "added-data-files")
	info.DeletedDataFiles = snapshotMetric(snapshot.Summary.Properties, "deleted-data-files")
	info.CommitToken = snapshot.Summary.Properties[snapshotCommitTokenKey]
	if logicalCount {
		rows, err := tbl.Scan().ToArrowTable(ctx)
		if err != nil {
			return CommitInfo{}, fmt.Errorf("iceberg: failed to count logical rows for table %s: %w", table, err)
		}
		info.TotalRows = rows.NumRows()
		rows.Release()
	}
	return info, nil
}

func snapshotMetric(properties map[string]string, key string) int64 {
	value, _ := strconv.ParseInt(properties[key], 10, 64)
	return value
}

func (d *Destination) afterSuccessfulCommit(ctx context.Context, table string) {
	d.afterSuccessfulCommitExpected(ctx, table, "")
}

func (d *Destination) afterSuccessfulCommitExpected(ctx context.Context, table, expectedIncarnation string) {
	inspectCtx, cancelInspect := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	info, err := d.getCommitInfo(inspectCtx, table, false)
	cancelInspect()
	if err != nil {
		config.Debug("[ICEBERG] Failed to inspect committed snapshot for %s: %v", table, err)
	} else {
		config.Debug(
			"[ICEBERG] Committed table=%s snapshot=%d sequence=%d operation=%s added_rows=%d deleted_rows=%d total_rows=%d added_files=%d deleted_files=%d cdc_resume_lsn=%s",
			table,
			info.SnapshotID,
			info.SequenceNumber,
			info.Operation,
			info.AddedRows,
			info.DeletedRows,
			info.TotalRows,
			info.AddedDataFiles,
			info.DeletedDataFiles,
			info.CDCResumeLSN,
		)
	}
	// Budgets only the load/lease/cleanup phases; runConfiguredMaintenance
	// gives an actual maintenance run its own budget once the table's
	// effective properties say it is due.
	maintenanceCtx, cancelMaintenance := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	d.runConfiguredMaintenanceExpected(maintenanceCtx, table, expectedIncarnation)
	cancelMaintenance()
}

func (i CommitInfo) String() string {
	return fmt.Sprintf("snapshot=%d operation=%s added_rows=%d deleted_rows=%d total_rows=%d", i.SnapshotID, i.Operation, i.AddedRows, i.DeletedRows, i.TotalRows)
}
