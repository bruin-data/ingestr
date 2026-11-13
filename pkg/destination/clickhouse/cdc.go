package clickhouse

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/tablename"
)

var (
	_ destination.Destination                  = (*ClickHouseDestination)(nil)
	_ destination.TruncateCapable              = (*ClickHouseDestination)(nil)
	_ destination.CDCTruncateCapable           = (*ClickHouseDestination)(nil)
	_ destination.CDCConditionalSchemaEvolver  = (*ClickHouseDestination)(nil)
	_ destination.CDCMergeAware                = (*ClickHouseDestination)(nil)
	_ destination.CDCUnchangedColsAware        = (*ClickHouseDestination)(nil)
	_ destination.CDCHardDeleteCapable         = (*ClickHouseDestination)(nil)
	_ destination.CDCResumeProvider            = (*ClickHouseDestination)(nil)
	_ destination.SerializedCDCRunsRequired    = (*ClickHouseDestination)(nil)
	_ destination.StagingTableLister           = (*ClickHouseDestination)(nil)
	_ destination.CDCStateTablePreparer        = (*ClickHouseDestination)(nil)
	_ destination.CDCStatePositionMigrator     = (*ClickHouseDestination)(nil)
	_ destination.CDCStateReader               = (*ClickHouseDestination)(nil)
	_ destination.CDCStateFenceReader          = (*ClickHouseDestination)(nil)
	_ destination.CDCStatePruner               = (*ClickHouseDestination)(nil)
	_ destination.CDCStatePruneBatchSizer      = (*ClickHouseDestination)(nil)
	_ destination.CDCTargetClaimer             = (*ClickHouseDestination)(nil)
	_ destination.CDCTargetIdentityProvider    = (*ClickHouseDestination)(nil)
	_ destination.CDCTargetIncarnationProvider = (*ClickHouseDestination)(nil)
	_ destination.ManagedCDCStateValidator     = (*ClickHouseDestination)(nil)
	_ destination.ManagedCDCRunLeaser          = (*ClickHouseDestination)(nil)
	_ destination.CDCConditionalMergeCapable   = (*ClickHouseDestination)(nil)
	_ destination.CDCConditionalTruncater      = (*ClickHouseDestination)(nil)
	_ destination.CDCMultiTableAtomicMerger    = (*ClickHouseDestination)(nil)
	_ destination.CDCLateTargetClaimPreparer   = (*ClickHouseDestination)(nil)
	_ destination.CDCConditionalSwapCapable    = (*ClickHouseDestination)(nil)
	_ destination.CDCConditionalSwapPlanner    = (*ClickHouseDestination)(nil)
)

func (d *ClickHouseDestination) SupportsCDCMerge() bool            { return true }
func (d *ClickHouseDestination) SupportsCDCUnchangedCols() bool    { return true }
func (d *ClickHouseDestination) SupportsCDCHardDelete() bool       { return true }
func (d *ClickHouseDestination) SupportsCDCConditionalMerge() bool { return true }
func (d *ClickHouseDestination) SupportsCDCConditionalSwap() bool  { return true }
func (d *ClickHouseDestination) RequiresSerializedCDCRuns() bool   { return true }

func (d *ClickHouseDestination) CanonicalCDCTarget(_ context.Context, table string) (string, error) {
	if err := tablename.TwoLevel("clickhouse").CheckName(table); err != nil {
		return "", err
	}
	db, name := d.parseTableName(table)
	if db == "" {
		return "", fmt.Errorf("ClickHouse CDC requires a database for target %q", table)
	}
	return destination.CDCTargetKey(db, name), nil
}

const (
	clickHouseShadowStagingDB          = "_bruin_staging"
	clickHouseZeroUUID                 = "00000000-0000-0000-0000-000000000000"
	clickHouseIncarnationCommentMarker = "__ingestr_cdc_incarnation="
)

type clickHouseTableMeta struct {
	uuid    string
	modTime string
	comment string
}

func (m clickHouseTableMeta) physicalID() string {
	return formatTableIdentity(m.uuid, m.modTime)
}

func (m clickHouseTableMeta) commentBindingID() string {
	if m.uuid != "" && m.uuid != clickHouseZeroUUID {
		return m.uuid
	}
	return "legacy"
}

func (m clickHouseTableMeta) incarnationID() string {
	if logical, ok := parseBoundIncarnationComment(m.comment, m.commentBindingID()); ok {
		return logical
	}
	return m.physicalID()
}

func (d *ClickHouseDestination) CDCTargetIncarnation(ctx context.Context, table string) (string, bool, error) {
	db, name := d.parseTableName(table)
	meta, exists, err := d.readTableMeta(ctx, db, name)
	if err != nil || !exists {
		return "", false, err
	}
	return destination.CDCTargetKey(db, name, meta.incarnationID()), true, nil
}

func (d *ClickHouseDestination) readTableMeta(ctx context.Context, db, name string) (clickHouseTableMeta, bool, error) {
	var meta clickHouseTableMeta
	err := d.conn.QueryRow(
		ctx,
		"SELECT toString(uuid), toString(metadata_modification_time), comment FROM system.tables WHERE database = ? AND name = ?",
		db, name,
	).Scan(&meta.uuid, &meta.modTime, &meta.comment)
	if errors.Is(err, sql.ErrNoRows) {
		return clickHouseTableMeta{}, false, nil
	}
	if err != nil {
		return clickHouseTableMeta{}, false, fmt.Errorf("read ClickHouse table identity for %s.%s: %w", db, name, err)
	}
	return meta, true, nil
}

func formatTableIdentity(uuid, modTime string) string {
	if uuid != "" && uuid != clickHouseZeroUUID {
		return uuid
	}
	return "legacy:" + modTime
}

func formatBoundIncarnationComment(existingComment, bindingID, logicalID string) string {
	base := stripBoundIncarnationComment(existingComment)
	marker := clickHouseIncarnationCommentMarker + hex.EncodeToString([]byte(bindingID)) + ":" + hex.EncodeToString([]byte(logicalID))
	if base == "" {
		return marker
	}
	return base + "\n" + marker
}

func stripBoundIncarnationComment(comment string) string {
	idx := strings.LastIndex(comment, clickHouseIncarnationCommentMarker)
	if idx == 0 {
		return ""
	}
	if idx > 0 && comment[idx-1] == '\n' {
		return comment[:idx-1]
	}
	return comment
}

func parseBoundIncarnationComment(comment, expectedBindingID string) (string, bool) {
	idx := strings.LastIndex(comment, clickHouseIncarnationCommentMarker)
	if idx < 0 || (idx > 0 && comment[idx-1] != '\n') {
		return "", false
	}
	rest := comment[idx+len(clickHouseIncarnationCommentMarker):]
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		rest = rest[:nl]
	}
	boundHex, logicalHex, ok := strings.Cut(rest, ":")
	if !ok || boundHex == "" || logicalHex == "" {
		return "", false
	}
	boundBytes, err := hex.DecodeString(boundHex)
	if err != nil || string(boundBytes) != expectedBindingID {
		return "", false
	}
	logicalBytes, err := hex.DecodeString(logicalHex)
	if err != nil || len(logicalBytes) == 0 {
		return "", false
	}
	return string(logicalBytes), true
}

func (d *ClickHouseDestination) checkCDCIncarnation(ctx context.Context, table, expected string) error {
	if expected == "" {
		return nil
	}
	actual, exists, err := d.CDCTargetIncarnation(ctx, table)
	if err != nil {
		return err
	}
	if !exists || actual != expected {
		return fmt.Errorf("ClickHouse CDC target %q physical incarnation changed", table)
	}
	return nil
}

// Fenced CDC writes never mutate the live target. Each write builds a shadow
// copy in _bruin_staging, applies the change there, and publishes it with
// EXCHANGE TABLES, which atomically swaps exactly one pair of names in an
// Atomic/Replicated/Shared database, so the target name always resolves to a
// complete table. ClickHouse has no multi-table DDL transaction (a multi-pair
// RENAME or EXCHANGE runs pair by pair), so a batch is published one atomic
// exchange per table:
//
//   - every shadow is built, and every target's identity and data version are
//     checked, before the first exchange, so a failing batch publishes nothing;
//   - each exchange is verified by the table it displaced: if that is not the
//     expected incarnation at its expected data version (the target was
//     replaced or written concurrently), it is exchanged back untouched;
//   - any failure exchanges every already-published table back in reverse
//     order, so a failed batch leaves no table changed, unless a published
//     table was written after it went live: that table stays published so
//     the outside write is not discarded, as after a crash between exchanges;
//   - a crash between exchanges leaves CDC state uncommitted, and the next run
//     replays the batch; LSN-ordered merges make the replay idempotent, as in
//     the StarRocks destination.
type cdcShadowPlan struct {
	table               string
	targetDB            string
	targetName          string
	expectedIncarnation string
	expectedMeta        clickHouseTableMeta
	expectedVersion     string
	shadowDB            string
	shadowName          string
	shadowID            string
	shadowVersion       string
	shadowConsumed      bool
	keepShadow          bool
}

func (p *cdcShadowPlan) shadowTable() string {
	return p.shadowDB + "." + p.shadowName
}

func (p *cdcShadowPlan) shadowRef() string {
	return quoteIdentifier(p.shadowDB) + "." + quoteIdentifier(p.shadowName)
}

func (p *cdcShadowPlan) targetRef() string {
	return quoteIdentifier(p.targetDB) + "." + quoteIdentifier(p.targetName)
}

func (p *cdcShadowPlan) incarnationChanged() error {
	return fmt.Errorf("ClickHouse CDC target %q physical incarnation changed", p.table)
}

func newCDCShadowName(targetDB, targetName string) (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate ClickHouse CDC shadow nonce: %w", err)
	}
	digest := destination.CDCTargetKeyDigest(destination.CDCTargetKey(targetDB, targetName))
	if len(digest) > 12 {
		digest = digest[:12]
	}
	return fmt.Sprintf("__ingestr_cdc_shw_%s_%s", digest, hex.EncodeToString(raw[:])), nil
}

// tableDataVersion fingerprints a MergeTree table's committed data per
// partition. Every INSERT, mutation (including lightweight DELETE), and
// partition operation allocates a higher block number or data version, and
// dropping parts changes a partition's block range or removes the partition,
// while background merges keep each partition's lowest block and highest
// version, so the value changes when the data changes but not when parts merge.
func (d *ClickHouseDestination) tableDataVersion(ctx context.Context, db, name string) (string, error) {
	var version string
	err := d.conn.QueryRow(
		ctx,
		"SELECT arrayStringConcat(arraySort(groupArray(concat(partition_id, ':', toString(lo), ':', toString(hi)))), ',') "+
			"FROM (SELECT partition_id, min(min_block_number) AS lo, max(greatest(max_block_number, data_version)) AS hi "+
			"FROM system.parts WHERE database = ? AND table = ? AND active GROUP BY partition_id)",
		db, name,
	).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read ClickHouse data version of %s.%s: %w", db, name, err)
	}
	return version, nil
}

func (d *ClickHouseDestination) prepareCDCShadowPlan(ctx context.Context, table, expectedIncarnation string, copyData bool) (*cdcShadowPlan, error) {
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return nil, err
	}
	targetDB, targetName := d.parseTableName(table)
	meta, exists, err := d.readTableMeta(ctx, targetDB, targetName)
	if err != nil {
		return nil, err
	}
	if !exists {
		if expectedIncarnation != "" {
			return nil, fmt.Errorf("ClickHouse CDC target %q physical incarnation changed", table)
		}
		return nil, fmt.Errorf("ClickHouse CDC target %s does not exist", table)
	}
	if expectedIncarnation != "" {
		actual := destination.CDCTargetKey(targetDB, targetName, meta.incarnationID())
		if actual != expectedIncarnation {
			return nil, fmt.Errorf("ClickHouse CDC target %q physical incarnation changed", table)
		}
	}
	if err := d.ensureDatabaseExists(ctx, clickHouseShadowStagingDB); err != nil {
		return nil, fmt.Errorf("ensure ClickHouse CDC shadow staging database: %w", err)
	}
	shadowName, err := newCDCShadowName(targetDB, targetName)
	if err != nil {
		return nil, err
	}
	if err := d.waitForMutations(ctx, targetDB, targetName); err != nil {
		return nil, err
	}
	version, err := d.tableDataVersion(ctx, targetDB, targetName)
	if err != nil {
		return nil, err
	}
	plan := &cdcShadowPlan{
		table:               table,
		targetDB:            targetDB,
		targetName:          targetName,
		expectedIncarnation: expectedIncarnation,
		expectedMeta:        meta,
		expectedVersion:     version,
		shadowDB:            clickHouseShadowStagingDB,
		shadowName:          shadowName,
	}

	createSQL := fmt.Sprintf(
		"CREATE TABLE %s%s AS %s",
		plan.shadowRef(),
		onClusterClause(d.cluster),
		plan.targetRef(),
	)
	if err := d.conn.Exec(ctx, createSQL); err != nil {
		config.LogFailedQuery(createSQL, err)
		return nil, fmt.Errorf("create ClickHouse CDC shadow table for %s: %w", table, err)
	}

	if copyData {
		if err := d.cloneTablePartitions(ctx, targetDB, targetName, plan.shadowDB, plan.shadowName); err != nil {
			_ = d.DropTable(context.WithoutCancel(ctx), plan.shadowTable())
			return nil, err
		}
	}
	if err := d.verifyPlanSource(ctx, plan, targetDB, targetName); err != nil {
		_ = d.DropTable(context.WithoutCancel(ctx), plan.shadowTable())
		return nil, err
	}
	return plan, nil
}

// verifyPlanSource checks that db.name is still the table the plan was built
// from: the same physical table and logical incarnation, with no data
// committed since the shadow was cloned.
func (d *ClickHouseDestination) verifyPlanSource(ctx context.Context, p *cdcShadowPlan, db, name string) error {
	meta, exists, err := d.readTableMeta(ctx, db, name)
	if err != nil {
		return err
	}
	if !exists || meta.physicalID() != p.expectedMeta.physicalID() || meta.incarnationID() != p.expectedMeta.incarnationID() {
		return p.incarnationChanged()
	}
	version, err := d.tableDataVersion(ctx, db, name)
	if err != nil {
		return err
	}
	if version != p.expectedVersion {
		return fmt.Errorf("ClickHouse CDC target %q was written outside this run while its change was being prepared; retry the run", p.table)
	}
	return nil
}

func (d *ClickHouseDestination) cloneTablePartitions(ctx context.Context, srcDB, srcName, dstDB, dstName string) error {
	rows, err := d.conn.Query(
		ctx,
		"SELECT DISTINCT partition_id FROM system.parts WHERE database = ? AND table = ? AND active ORDER BY partition_id",
		srcDB, srcName,
	)
	if err != nil {
		return fmt.Errorf("list ClickHouse partitions of %s.%s: %w", srcDB, srcName, err)
	}
	var partitions []string
	for rows.Next() {
		var partID string
		if err := rows.Scan(&partID); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan ClickHouse partition of %s.%s: %w", srcDB, srcName, err)
		}
		partitions = append(partitions, partID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("read ClickHouse partitions of %s.%s: %w", srcDB, srcName, err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close ClickHouse partition rows of %s.%s: %w", srcDB, srcName, err)
	}
	for _, partID := range partitions {
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		replaceSQL := fmt.Sprintf(
			"ALTER TABLE %s.%s%s REPLACE PARTITION ID %s FROM %s.%s",
			quoteIdentifier(dstDB),
			quoteIdentifier(dstName),
			onClusterClause(d.cluster),
			sqlString(partID),
			quoteIdentifier(srcDB),
			quoteIdentifier(srcName),
		)
		if err := d.conn.Exec(ctx, replaceSQL); err != nil {
			config.LogFailedQuery(replaceSQL, err)
			return fmt.Errorf("clone ClickHouse partition %q from %s.%s: %w", partID, srcDB, srcName, err)
		}
	}
	return nil
}

func (d *ClickHouseDestination) cleanupCDCShadowPlans(ctx context.Context, plans []*cdcShadowPlan) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	for _, p := range plans {
		if p != nil && !p.shadowConsumed && !p.keepShadow {
			_ = d.DropTable(cleanupCtx, p.shadowTable())
		}
	}
}

func (d *ClickHouseDestination) exchangeShadow(ctx context.Context, p *cdcShadowPlan) error {
	exchangeSQL := fmt.Sprintf("EXCHANGE TABLES %s AND %s%s", p.shadowRef(), p.targetRef(), onClusterClause(d.cluster))
	if err := d.conn.Exec(ctx, exchangeSQL); err != nil {
		config.LogFailedQuery(exchangeSQL, err)
		return err
	}
	return nil
}

func (d *ClickHouseDestination) publishCDCShadowPlans(ctx context.Context, plans []*cdcShadowPlan) error {
	if len(plans) == 0 {
		return source.ConnectorLeaseLoss(ctx)
	}
	for _, p := range plans {
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		shadowMeta, exists, err := d.readTableMeta(ctx, p.shadowDB, p.shadowName)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("ClickHouse CDC shadow table %s disappeared before publish", p.shadowTable())
		}
		stamped := formatBoundIncarnationComment(p.expectedMeta.comment, shadowMeta.commentBindingID(), p.expectedMeta.incarnationID())
		commentSQL := fmt.Sprintf("ALTER TABLE %s%s MODIFY COMMENT %s", p.shadowRef(), onClusterClause(d.cluster), sqlString(stamped))
		if err := d.conn.Exec(ctx, commentSQL); err != nil {
			config.LogFailedQuery(commentSQL, err)
			return fmt.Errorf("stamp ClickHouse CDC shadow incarnation on %s: %w", p.shadowTable(), err)
		}
		p.shadowID = shadowMeta.physicalID()
		if err := d.waitForMutations(ctx, p.shadowDB, p.shadowName); err != nil {
			return err
		}
		shadowVersion, err := d.tableDataVersion(ctx, p.shadowDB, p.shadowName)
		if err != nil {
			return err
		}
		p.shadowVersion = shadowVersion
		if err := d.verifyPlanSource(ctx, p, p.targetDB, p.targetName); err != nil {
			return err
		}
	}

	published := make([]*cdcShadowPlan, 0, len(plans))
	for _, p := range plans {
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return errors.Join(err, d.rollbackPublishedShadows(ctx, published))
		}
		if err := d.verifyPlanSource(ctx, p, p.targetDB, p.targetName); err != nil {
			return errors.Join(err, d.rollbackPublishedShadows(ctx, published))
		}
		if err := d.exchangeShadow(ctx, p); err != nil {
			publishErr := fmt.Errorf("publish ClickHouse CDC shadow table for %s (EXCHANGE TABLES requires an Atomic, Replicated, or Shared database): %w", p.table, err)
			if checkErr := d.verifyPlanSource(context.WithoutCancel(ctx), p, p.targetDB, p.targetName); checkErr != nil {
				publishErr = checkErr
			}
			return errors.Join(publishErr, d.rollbackPublishedShadows(ctx, published))
		}
		published = append(published, p)
		if err := d.verifyPlanSource(context.WithoutCancel(ctx), p, p.shadowDB, p.shadowName); err != nil {
			return errors.Join(err, d.rollbackPublishedShadows(ctx, published))
		}
	}

	dropCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	for _, p := range plans {
		p.shadowConsumed = true
		if err := d.DropTable(dropCtx, p.shadowTable()); err != nil {
			config.Debug("[CLICKHOUSE CDC] Could not drop replaced table %s: %v", p.shadowTable(), err)
		}
	}
	return nil
}

// rollbackPublishedShadows exchanges published tables back in reverse order,
// verifying that the table each exchange displaced is back at the target name
// and the shadow is back at its own name. A published table that was replaced
// or written since it went live is left in place, so an outside write is never
// moved aside and dropped. A table that cannot be restored, or that is written
// while being restored, is kept at its shadow name and reported.
func (d *ClickHouseDestination) rollbackPublishedShadows(ctx context.Context, published []*cdcShadowPlan) error {
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
	defer cancel()
	var errs []error
	for i := len(published) - 1; i >= 0; i-- {
		p := published[i]
		live, liveExists, err := d.readTableMeta(rollbackCtx, p.targetDB, p.targetName)
		var liveVersion string
		if err == nil && liveExists {
			liveVersion, err = d.tableDataVersion(rollbackCtx, p.targetDB, p.targetName)
		}
		if err != nil {
			p.keepShadow = true
			errs = append(errs, fmt.Errorf("inspect published ClickHouse CDC target %s before restoring it (the table it replaced is kept at %s): %w", p.table, p.shadowTable(), err))
			continue
		}
		if !liveExists || live.physicalID() != p.shadowID {
			p.keepShadow = true
			errs = append(errs, fmt.Errorf("published ClickHouse CDC target %s was replaced outside this run and is not restored; the table it replaced is kept at %s", p.table, p.shadowTable()))
			continue
		}
		if liveVersion != p.shadowVersion {
			errs = append(errs, fmt.Errorf("published ClickHouse CDC target %s was written outside this run after it was published; it is left published so that write is kept, and the next run replays the batch", p.table))
			continue
		}
		displaced, displacedExists, displacedErr := d.readTableMeta(rollbackCtx, p.shadowDB, p.shadowName)
		if err := d.exchangeShadow(rollbackCtx, p); err != nil {
			p.keepShadow = true
			errs = append(errs, fmt.Errorf("restore ClickHouse CDC target %s: the table it replaced is kept at %s: %w", p.table, p.shadowTable(), err))
			continue
		}
		restored, restoredExists, restoredErr := d.readTableMeta(rollbackCtx, p.targetDB, p.targetName)
		shadow, shadowExists, shadowErr := d.readTableMeta(rollbackCtx, p.shadowDB, p.shadowName)
		if err := errors.Join(displacedErr, restoredErr, shadowErr); err != nil {
			p.keepShadow = true
			errs = append(errs, fmt.Errorf("verify restored ClickHouse CDC target %s (table kept at %s): %w", p.table, p.shadowTable(), err))
			continue
		}
		if !displacedExists || !restoredExists || !shadowExists ||
			restored.physicalID() != displaced.physicalID() || shadow.physicalID() != p.shadowID {
			p.keepShadow = true
			errs = append(errs, fmt.Errorf("restored ClickHouse CDC target %s does not hold the table the publish displaced; table kept at %s", p.table, p.shadowTable()))
			continue
		}
		shadowVersion, err := d.tableDataVersion(rollbackCtx, p.shadowDB, p.shadowName)
		if err != nil || shadowVersion != p.shadowVersion {
			p.keepShadow = true
			errs = append(errs, errors.Join(fmt.Errorf("published ClickHouse CDC target %s may have been written outside this run while it was being restored; that table is kept at %s", p.table, p.shadowTable()), err))
		}
	}
	return errors.Join(errs...)
}

func (d *ClickHouseDestination) TruncateCDCTable(ctx context.Context, table string) error {
	return d.TruncateTable(ctx, table)
}

func (d *ClickHouseDestination) TruncateCDCTableIfIncarnation(ctx context.Context, table, expectedIncarnation string) error {
	if expectedIncarnation == "" {
		return errors.New("ClickHouse conditional CDC truncate requires an expected incarnation")
	}
	plan, err := d.prepareCDCShadowPlan(ctx, table, expectedIncarnation, false)
	if err != nil {
		return err
	}
	plans := []*cdcShadowPlan{plan}
	defer d.cleanupCDCShadowPlans(ctx, plans)
	return d.publishCDCShadowPlans(ctx, plans)
}

func (d *ClickHouseDestination) MergeCDCTablesAtomically(ctx context.Context, merges []destination.CDCAtomicTableMerge) error {
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if len(merges) == 0 {
		return nil
	}

	for _, merge := range merges {
		opts := merge.Options
		if destination.HasCDCDeletedColumn(opts.Columns) && len(opts.PrimaryKeys) > 0 {
			if opts.IncrementalPredicate != "" {
				return fmt.Errorf("ClickHouse CDC merge does not support incremental predicates")
			}
			if err := d.validatePrimaryKey(ctx, "CDC target", opts.TargetTable, opts.PrimaryKeys); err != nil {
				return err
			}
		}
	}

	plans := make([]*cdcShadowPlan, 0, len(merges))
	planByKey := make(map[string]*cdcShadowPlan, len(merges))
	defer func() { d.cleanupCDCShadowPlans(ctx, plans) }()

	for _, merge := range merges {
		if err := source.ConnectorLeaseLoss(ctx); err != nil {
			return err
		}
		opts := merge.Options
		targetDB, targetName := d.parseTableName(opts.TargetTable)
		key := destination.CDCTargetKey(targetDB, targetName)
		plan, exists := planByKey[key]
		if !exists {
			var err error
			plan, err = d.prepareCDCShadowPlan(ctx, opts.TargetTable, opts.CDCExpectedIncarnation, !merge.Truncate)
			if err != nil {
				return err
			}
			planByKey[key] = plan
			plans = append(plans, plan)
		} else {
			if opts.CDCExpectedIncarnation != "" {
				actual := destination.CDCTargetKey(targetDB, targetName, plan.expectedMeta.incarnationID())
				if actual != opts.CDCExpectedIncarnation {
					return fmt.Errorf("ClickHouse CDC target %q physical incarnation changed", opts.TargetTable)
				}
				if plan.expectedIncarnation == "" {
					plan.expectedIncarnation = opts.CDCExpectedIncarnation
				}
			}
			if merge.Truncate {
				if err := d.TruncateTable(ctx, plan.shadowTable()); err != nil {
					return fmt.Errorf("truncate ClickHouse CDC shadow target: %w", err)
				}
			}
		}

		stagingDB, stagingName := d.parseTableName(opts.StagingTable)
		if destination.HasCDCDeletedColumn(opts.Columns) && len(opts.PrimaryKeys) > 0 {
			if err := d.mergeCDCInto(ctx, plan.shadowDB, plan.shadowName, stagingDB, stagingName, opts); err != nil {
				return err
			}
		} else {
			if err := d.insertStagingInto(ctx, plan.shadowDB, plan.shadowName, stagingDB, stagingName, destination.DestinationColumns(opts.Columns)); err != nil {
				return err
			}
		}
	}

	return d.publishCDCShadowPlans(ctx, plans)
}

func (d *ClickHouseDestination) CDCConditionalSwapIncarnations(ctx context.Context, targetTable, stagingTable string) (string, string, error) {
	targetDB, targetName := d.parseTableName(targetTable)
	stageDB, stageName := d.parseTableName(stagingTable)
	meta, exists, err := d.readTableMeta(ctx, stageDB, stageName)
	if err != nil {
		return "", "", err
	}
	if !exists {
		return "", "", fmt.Errorf("ClickHouse swap staging table %q does not exist", stagingTable)
	}
	id := meta.incarnationID()
	return destination.CDCTargetKey(stageDB, stageName, id), destination.CDCTargetKey(targetDB, targetName, id), nil
}

func (d *ClickHouseDestination) mergeCDC(ctx context.Context, opts destination.MergeOptions) error {
	startMerge := time.Now()
	if opts.IncrementalPredicate != "" {
		return fmt.Errorf("ClickHouse CDC merge does not support incremental predicates")
	}
	if len(opts.PrimaryKeys) == 0 {
		return fmt.Errorf("ClickHouse CDC merge requires primary keys")
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return err
	}
	if err := d.validatePrimaryKey(ctx, "CDC target", opts.TargetTable, opts.PrimaryKeys); err != nil {
		return err
	}

	stagingDB, stagingName := d.parseTableName(opts.StagingTable)
	targetDB, targetName := d.parseTableName(opts.TargetTable)

	if opts.CDCExpectedIncarnation == "" {
		if err := d.mergeCDCInto(ctx, targetDB, targetName, stagingDB, stagingName, opts); err != nil {
			return err
		}
		config.Debug("[CLICKHOUSE CDC MERGE] Merge completed in %v", time.Since(startMerge))
		return nil
	}

	plan, err := d.prepareCDCShadowPlan(ctx, opts.TargetTable, opts.CDCExpectedIncarnation, true)
	if err != nil {
		return err
	}
	plans := []*cdcShadowPlan{plan}
	defer d.cleanupCDCShadowPlans(ctx, plans)

	if err := d.mergeCDCInto(ctx, plan.shadowDB, plan.shadowName, stagingDB, stagingName, opts); err != nil {
		return err
	}
	if err := d.publishCDCShadowPlans(ctx, plans); err != nil {
		return err
	}

	config.Debug("[CLICKHOUSE CDC MERGE] Merge completed in %v", time.Since(startMerge))
	return nil
}

func (d *ClickHouseDestination) mergeCDCInto(ctx context.Context, targetDB, targetName, stagingDB, stagingName string, opts destination.MergeOptions) error {
	insertSQL := buildCDCMergeInsert(targetDB, targetName, stagingDB, stagingName, opts)
	config.Debug("[CLICKHOUSE CDC MERGE] Executing INSERT: %s", insertSQL)

	if err := d.conn.Exec(ctx, insertSQL); err != nil {
		config.LogFailedQuery(insertSQL, err)
		return fmt.Errorf("failed to merge ClickHouse CDC records: %w", err)
	}

	if opts.CDCHardDelete && !opts.CDCSnapshot {
		hasDeletes, err := d.stagingHasCDCDeletes(ctx, stagingDB, stagingName)
		if err != nil {
			return err
		}
		if hasDeletes {
			if err := d.applyCDCHardDeletes(ctx, targetDB, targetName, stagingDB, stagingName, opts.PrimaryKeys); err != nil {
				return err
			}
		}
	}

	optimizeSQL := fmt.Sprintf("OPTIMIZE TABLE %s.%s FINAL", quoteIdentifier(targetDB), quoteIdentifier(targetName))
	if err := d.conn.Exec(ctx, optimizeSQL); err != nil {
		config.LogFailedQuery(optimizeSQL, err)
		config.Debug("[CLICKHOUSE CDC MERGE] OPTIMIZE FINAL completed with note: %v", err)
	}
	return nil
}

func (d *ClickHouseDestination) stagingHasCDCDeletes(ctx context.Context, stagingDB, stagingName string) (bool, error) {
	query := fmt.Sprintf("SELECT count() FROM %s.%s WHERE %s = true",
		quoteIdentifier(stagingDB), quoteIdentifier(stagingName), quoteIdentifier(destination.CDCDeletedColumn))
	var cnt uint64
	if err := d.conn.QueryRow(ctx, query).Scan(&cnt); err != nil {
		return false, fmt.Errorf("check ClickHouse CDC deletes in %s.%s: %w", stagingDB, stagingName, err)
	}
	return cnt > 0, nil
}

func (d *ClickHouseDestination) applyCDCHardDeletes(ctx context.Context, targetDB, targetName, stagingDB, stagingName string, primaryKeys []string) error {
	mutationSettings, err := d.cdcMutationSettings(ctx, targetDB, targetName, stagingDB, stagingName)
	if err != nil {
		return err
	}
	deleteSQL := buildCDCHardDeleteSQL(targetDB, targetName, stagingDB, stagingName, primaryKeys, d.cluster, mutationSettings)
	config.Debug("[CLICKHOUSE CDC MERGE] Executing hard DELETE: %s", deleteSQL)
	if err := d.conn.Exec(ctx, deleteSQL); err != nil {
		config.LogFailedQuery(deleteSQL, err)
		return fmt.Errorf("failed to hard-delete ClickHouse CDC records: %w", err)
	}
	if err := d.waitForMutations(ctx, targetDB, targetName); err != nil {
		return fmt.Errorf("failed waiting for ClickHouse CDC hard-delete mutation: %w", err)
	}
	return nil
}

func (d *ClickHouseDestination) cdcMutationSettings(ctx context.Context, targetDB, targetName, stagingDB, stagingName string) (string, error) {
	replicated, err := d.isReplicatedTarget(ctx, targetDB, targetName)
	if err != nil || !replicated {
		return "", err
	}
	for _, table := range []string{
		fmt.Sprintf("%s.%s", quoteIdentifier(stagingDB), quoteIdentifier(stagingName)),
		fmt.Sprintf("%s.%s", quoteIdentifier(targetDB), quoteIdentifier(targetName)),
	} {
		syncSQL := fmt.Sprintf("SYSTEM SYNC REPLICA%s %s", onClusterClause(d.cluster), table)
		if err := d.conn.Exec(ctx, syncSQL); err != nil {
			config.LogFailedQuery(syncSQL, err)
			return "", fmt.Errorf("failed to sync replicas of %s: %w", table, err)
		}
	}
	return " SETTINGS allow_nondeterministic_mutations = 1, mutations_sync = 2", nil
}

func (d *ClickHouseDestination) isReplicatedTarget(ctx context.Context, database, tableName string) (bool, error) {
	if d.cluster == "" || !strings.EqualFold(d.engineType, "replicated_merge_tree") {
		return false, nil
	}
	var engine string
	if err := d.conn.QueryRow(ctx, "SELECT engine FROM system.tables WHERE database = ? AND name = ?", database, tableName).Scan(&engine); err != nil {
		return false, fmt.Errorf("failed to read engine of %s.%s: %w", database, tableName, err)
	}
	return strings.HasPrefix(engine, "Replicated"), nil
}

func buildCDCHardDeleteSQL(targetDB, targetName, stagingDB, stagingName string, primaryKeys []string, cluster, mutationSettings string) string {
	target := fmt.Sprintf("%s.%s", quoteIdentifier(targetDB), quoteIdentifier(targetName))
	staging := fmt.Sprintf("%s.%s", quoteIdentifier(stagingDB), quoteIdentifier(stagingName))
	lsn := quoteIdentifier(destination.CDCLSNColumn)
	deleted := quoteIdentifier(destination.CDCDeletedColumn)

	pkExpr := pkTupleExpr(primaryKeys, "")
	targetPKExpr := pkTupleExpr(primaryKeys, "target")
	joinConds := make([]string, len(primaryKeys))
	for i, pk := range primaryKeys {
		qk := quoteIdentifier(pk)
		joinConds[i] = fmt.Sprintf("target.%s = s.%s", qk, qk)
	}

	return fmt.Sprintf(
		"ALTER TABLE %s%s DELETE WHERE %s IN ("+
			"SELECT %s FROM %s AS target FINAL "+
			"INNER JOIN %s AS s ON %s "+
			"WHERE target.%s = true AND s.%s = true AND target.%s = s.%s"+
			")%s",
		target, onClusterClause(cluster), pkExpr,
		targetPKExpr, target,
		staging, strings.Join(joinConds, " AND "),
		deleted, deleted, lsn, lsn,
		mutationSettings,
	)
}

func buildCDCMergeInsert(targetDB, targetName, stagingDB, stagingName string, opts destination.MergeOptions) string {
	cols := destination.DestinationColumns(opts.Columns)
	hasUnchanged := destination.HasCDCUnchangedColsColumn(opts.Columns)

	pkMap := make(map[string]bool, len(opts.PrimaryKeys))
	quotedPKs := make([]string, len(opts.PrimaryKeys))
	for i, pk := range opts.PrimaryKeys {
		pkMap[strings.ToLower(pk)] = true
		quotedPKs[i] = quoteIdentifier(pk)
	}
	pkList := strings.Join(quotedPKs, ", ")
	pkExpr := pkTupleExpr(opts.PrimaryKeys, "")

	joinOn := func(left, right string) string {
		conds := make([]string, len(quotedPKs))
		for i, pk := range quotedPKs {
			conds[i] = fmt.Sprintf("%s.%s = %s.%s", left, pk, right, pk)
		}
		return strings.Join(conds, " AND ")
	}
	aliased := func(alias string) string {
		parts := make([]string, len(cols))
		for i, col := range cols {
			qc := quoteIdentifier(col)
			parts[i] = alias + "." + qc + " AS " + qc
		}
		return strings.Join(parts, ", ")
	}
	unique := func(base string) string {
		for slices.ContainsFunc(opts.Columns, func(col string) bool { return strings.EqualFold(base, col) }) {
			base += "_"
		}
		return quoteIdentifier(base)
	}

	originCol := unique("__ingestr_origin")
	presentCol := unique("__ingestr_present")
	tombstoneCol := unique("__ingestr_tombstone")
	unchangedCol := unique("__ingestr_unchanged")

	lsn := quoteIdentifier(destination.CDCLSNColumn)
	deleted := quoteIdentifier(destination.CDCDeletedColumn)
	staging := fmt.Sprintf("%s.%s", quoteIdentifier(stagingDB), quoteIdentifier(stagingName))
	target := fmt.Sprintf("%s.%s", quoteIdentifier(targetDB), quoteIdentifier(targetName))

	targetScoped := ""
	if opts.CDCSnapshot {
		if pred := snapshotKeyRange(opts); pred != "" {
			targetScoped = fmt.Sprintf("(SELECT %s, toUInt8(1) AS %s FROM %s FINAL WHERE %s)", strings.Join(quoteColumns(cols), ", "), presentCol, target, pred)
		} else {
			targetScoped = fmt.Sprintf("(SELECT %s, toUInt8(1) AS %s FROM %s FINAL)", strings.Join(quoteColumns(cols), ", "), presentCol, target)
		}
	} else {
		targetScoped = fmt.Sprintf("(SELECT %s, toUInt8(1) AS %s FROM %s FINAL WHERE %s IN (SELECT %s FROM %s))",
			strings.Join(quoteColumns(cols), ", "), presentCol, target, pkExpr, pkExpr, staging)
	}

	unchangedSelect := "'[]'"
	if hasUnchanged {
		unchangedSelect = fmt.Sprintf("ifNull(s.%s, '[]')", quoteIdentifier(destination.CDCUnchangedColsColumn))
	}

	changesCTE := fmt.Sprintf(
		"changes AS ("+
			"SELECT %s, %s AS %s FROM %s AS s "+
			"LEFT JOIN %s AS t ON %s "+
			"LEFT JOIN (SELECT %s, %s, toUInt8(1) AS %s FROM %s WHERE %s = true GROUP BY %s, %s) AS __ingestr_tb ON %s AND s.%s = __ingestr_tb.%s "+
			"WHERE ifNull(t.%s, 0) = 0 OR t.%s IS NULL OR s.%s > t.%s "+
			"OR (s.%s = t.%s AND ifNull(t.%s, false) = false AND ifNull(__ingestr_tb.%s, 0) = 1)"+
			")",
		aliased("s"), unchangedSelect, unchangedCol, staging,
		targetScoped, joinOn("s", "t"),
		pkList, lsn, tombstoneCol, staging, deleted, pkList, lsn, joinOn("s", "__ingestr_tb"), lsn, lsn,
		presentCol, lsn, lsn, lsn,
		lsn, lsn, deleted, tombstoneCol,
	)

	quotedCols := strings.Join(quoteColumns(cols), ", ")
	if opts.CDCSnapshot {
		return fmt.Sprintf(
			"INSERT INTO %s (%s) WITH %s SELECT %s FROM (SELECT * FROM changes ORDER BY %s DESC, %s DESC LIMIT 1 BY %s) AS l",
			target, quotedCols, changesCTE, aliased("l"), lsn, deleted, pkList,
		)
	}

	historyCTE := fmt.Sprintf(
		"history AS ("+
			"SELECT %s, %s, toUInt8(1) AS %s FROM changes "+
			"UNION ALL "+
			"SELECT %s, '[]' AS %s, toUInt8(0) AS %s FROM (SELECT %s FROM %s FINAL WHERE %s IN (SELECT %s FROM changes)) AS t"+
			")",
		quotedCols, unchangedCol, originCol,
		aliased("t"), unchangedCol, originCol, quotedCols, target, pkExpr, pkExpr,
	)

	overallWeight := fmt.Sprintf("(ifNull(%s, ''), toUInt8(ifNull(%s, false)), %s)", lsn, deleted, originCol)
	payloadWeight := fmt.Sprintf("(ifNull(%s, ''), %s)", lsn, originCol)

	selectExprs := make([]string, len(cols))
	for i, col := range cols {
		qc := quoteIdentifier(col)
		switch {
		case pkMap[strings.ToLower(col)]:
			selectExprs[i] = qc
		case destination.IsCDCColumn(col):
			selectExprs[i] = fmt.Sprintf("argMax(tuple(%s), %s).1", qc, overallWeight)
		default:
			cond := fmt.Sprintf("(%s = 0 OR ifNull(%s, false) = false)", originCol, deleted)
			if hasUnchanged {
				cond = fmt.Sprintf("(%s = 0 OR (ifNull(%s, false) = false AND NOT has(arrayMap(x -> lowerUTF8(x), JSONExtract(ifNull(%s, '[]'), 'Array(String)')), %s)))",
					originCol, deleted, unchangedCol, sqlString(strings.ToLower(col)))
			}
			selectExprs[i] = fmt.Sprintf("argMaxIf(tuple(%s), %s, %s).1", qc, payloadWeight, cond)
		}
	}

	return fmt.Sprintf(
		"INSERT INTO %s (%s) WITH %s, %s SELECT %s FROM history GROUP BY %s HAVING max(%s) = 1 AND max(if(%s = 0 OR ifNull(%s, false) = false, 1, 0)) = 1",
		target, quotedCols, changesCTE, historyCTE, strings.Join(selectExprs, ", "),
		pkList, originCol, originCol, deleted,
	)
}

func pkTupleExpr(primaryKeys []string, alias string) string {
	quoted := make([]string, len(primaryKeys))
	for i, pk := range primaryKeys {
		if alias != "" {
			quoted[i] = alias + "." + quoteIdentifier(pk)
		} else {
			quoted[i] = quoteIdentifier(pk)
		}
	}
	if len(quoted) == 1 {
		return quoted[0]
	}
	return "(" + strings.Join(quoted, ", ") + ")"
}

func snapshotKeyRange(opts destination.MergeOptions) string {
	bounds := opts.CDCSnapshotRange
	if !opts.CDCSnapshot || bounds == nil || opts.Schema == nil || len(opts.PrimaryKeys) == 0 || len(bounds.Last) != len(opts.PrimaryKeys) || bounds.After != nil && len(bounds.After) != len(opts.PrimaryKeys) {
		return ""
	}
	last, after := make([]string, len(opts.PrimaryKeys)), make([]string, len(bounds.After))
	ordering := 0
	for i, key := range opts.PrimaryKeys {
		index := slices.IndexFunc(opts.Schema.Columns, func(col schema.Column) bool { return strings.EqualFold(col.Name, key) })
		if index < 0 {
			return ""
		}
		col := opts.Schema.Columns[index]
		bits := 0
		switch col.DataType {
		case schema.TypeInt8:
			bits = 8
		case schema.TypeInt16:
			bits = 16
		case schema.TypeInt32:
			bits = 32
		case schema.TypeInt64:
			bits = 64
		}
		if bits == 0 || col.Unsigned {
			return ""
		}
		upper, err := strconv.ParseInt(bounds.Last[i], 10, bits)
		if err != nil {
			return ""
		}
		last[i] = strconv.FormatInt(upper, 10)
		if bounds.After != nil {
			lower, err := strconv.ParseInt(bounds.After[i], 10, bits)
			if err != nil {
				return ""
			}
			after[i] = strconv.FormatInt(lower, 10)
			if ordering == 0 {
				if lower < upper {
					ordering = -1
				} else if lower > upper {
					ordering = 1
				}
			}
		}
	}
	upper := snapshotKeyComparison(opts.PrimaryKeys, last, true)
	if bounds.After == nil {
		return upper
	}
	if ordering != -1 {
		return ""
	}
	return snapshotKeyComparison(opts.PrimaryKeys, after, false) + " AND " + upper
}

func snapshotKeyComparison(keys, values []string, upper bool) string {
	terms := make([]string, len(keys))
	equal := make([]string, 0, len(keys))
	for i, key := range keys {
		operator := ">"
		if upper {
			operator = "<"
			if i == len(keys)-1 {
				operator = "<="
			}
		}
		comparison := quoteIdentifier(key) + " " + operator + " " + values[i]
		if len(equal) > 0 {
			comparison = "(" + strings.Join(equal, " AND ") + " AND " + comparison + ")"
		}
		terms[i] = comparison
		equal = append(equal, quoteIdentifier(key)+" = "+values[i])
	}
	return "(" + strings.Join(terms, " OR ") + ")"
}

func (d *ClickHouseDestination) tableHasColumn(ctx context.Context, database, tableName, colName string) (bool, error) {
	var count uint64
	if err := d.conn.QueryRow(
		ctx,
		"SELECT count() FROM system.columns WHERE database = ? AND table = ? AND name = ?",
		database, tableName, colName,
	).Scan(&count); err != nil {
		return false, fmt.Errorf("inspect ClickHouse column %s on %s.%s: %w", colName, database, tableName, err)
	}
	return count > 0, nil
}

func (d *ClickHouseDestination) validatePrimaryKey(ctx context.Context, role, table string, expected []string) error {
	database, tableName := d.parseTableName(table)
	var engine, sortingKey string
	err := d.conn.QueryRow(
		ctx,
		"SELECT engine, sorting_key FROM system.tables WHERE database = ? AND name = ?",
		database, tableName,
	).Scan(&engine, &sortingKey)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("ClickHouse %s %s does not exist", role, table)
	}
	if err != nil {
		return fmt.Errorf("read ClickHouse %s metadata for %s: %w", role, table, err)
	}

	actual := parseClickHouseKeyList(sortingKey)

	normalize := func(cols []string) []string {
		out := make([]string, len(cols))
		for i, col := range cols {
			out[i] = strings.ToLower(col)
		}
		slices.Sort(out)
		return out
	}

	if !strings.Contains(engine, "ReplacingMergeTree") || !slices.Equal(normalize(actual), normalize(expected)) {
		return fmt.Errorf("ClickHouse %s %s requires ReplacingMergeTree with primary key (%s); found %s (sorting key: %s). If the key changed, rerun with --full-refresh to rebuild the table",
			role, table, strings.Join(expected, ", "), engine, strings.Join(actual, ", "))
	}
	return nil
}

func parseClickHouseKeyList(expr string) []string {
	expr = strings.TrimSpace(expr)
	if expr == "" || expr == "tuple()" {
		return nil
	}
	var out []string
	var cur strings.Builder
	inBacktick := false
	parenDepth := 0
	for i := 0; i < len(expr); i++ {
		ch := expr[i]
		switch {
		case ch == '`':
			if inBacktick && i+1 < len(expr) && expr[i+1] == '`' {
				cur.WriteByte('`')
				i++
			} else {
				inBacktick = !inBacktick
			}
		case ch == '(' && !inBacktick:
			parenDepth++
			cur.WriteByte(ch)
		case ch == ')' && !inBacktick && parenDepth > 0:
			parenDepth--
			cur.WriteByte(ch)
		case ch == ',' && !inBacktick && parenDepth == 0:
			if part := strings.TrimSpace(cur.String()); part != "" {
				out = append(out, part)
			}
			cur.Reset()
		default:
			cur.WriteByte(ch)
		}
	}
	if part := strings.TrimSpace(cur.String()); part != "" {
		out = append(out, part)
	}
	return out
}

func isMissingTableOrDB(err error) bool {
	if err == nil {
		return false
	}
	var chErr *clickhouse.Exception
	if errors.As(err, &chErr) && (chErr.Code == 60 || chErr.Code == 81) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unknown_table") ||
		strings.Contains(msg, "unknown_database") ||
		strings.Contains(msg, "unknown table") ||
		strings.Contains(msg, "unknown database") ||
		strings.Contains(msg, "doesn't exist") ||
		strings.Contains(msg, "does not exist")
}

func isTableExistsError(err error) bool {
	if err == nil {
		return false
	}
	var chErr *clickhouse.Exception
	if errors.As(err, &chErr) && chErr.Code == 57 {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "table_already_exists") || strings.Contains(msg, "already exists")
}

func sqlString(value string) string {
	return "'" + escapeClickHouseString(value) + "'"
}
