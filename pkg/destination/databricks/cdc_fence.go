package databricks

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	// cdcIncarnationGuardMarker is raised via SIGNAL from inside a BEGIN ATOMIC
	// block when a fenced target no longer has the bound Unity Catalog identity.
	cdcIncarnationGuardMarker = "INGESTR_CDC_INCARNATION_CHANGED"

	// cdcIncarnationCommentMarker records the Delta table id inside the Unity
	// Catalog table comment so both DROP+CREATE and CREATE OR REPLACE TABLE
	// invalidate the SQL-visible guard inside BEGIN ATOMIC.
	cdcIncarnationCommentMarker = "__ingestr_cdc_incarnation="
)

var errCDCIncarnationChanged = errors.New("physical incarnation changed")

type cdcFenceSpec struct {
	table               string
	expectedIncarnation string
}

// cdcFence binds an expected physical incarnation (Delta table id from
// DESCRIBE DETAIL) to SQL-visible Unity Catalog metadata: both the immutable
// information_schema.tables.created timestamp and the stamped table comment
// carrying the Delta table id. Checking both inside the same BEGIN ATOMIC
// transaction as the target DML keeps live table names available while
// ensuring both DROP+CREATE and CREATE OR REPLACE TABLE trigger SIGNAL and
// roll back the entire transaction.
type cdcFence struct {
	canonKey            string
	rawTable            string
	catalog             string
	schemaName          string
	tableName           string
	targetFull          string
	expectedIncarnation string
	createdMicros       string
	expectedComment     string
}

func splitCDCIncarnationComment(comment string) (prefix, markerID string) {
	if idx := strings.Index(comment, "\n"+cdcIncarnationCommentMarker); idx >= 0 {
		return comment[:idx], comment[idx+1+len(cdcIncarnationCommentMarker):]
	}
	if idx := strings.Index(comment, " "+cdcIncarnationCommentMarker); idx >= 0 {
		return comment[:idx], comment[idx+1+len(cdcIncarnationCommentMarker):]
	}
	if strings.HasPrefix(comment, cdcIncarnationCommentMarker) {
		return "", strings.TrimPrefix(comment, cdcIncarnationCommentMarker)
	}
	return comment, ""
}

func formatCDCIncarnationComment(existingComment, deltaID string) string {
	prefix, _ := splitCDCIncarnationComment(existingComment)
	if prefix == "" {
		return cdcIncarnationCommentMarker + deltaID
	}
	return prefix + " " + cdcIncarnationCommentMarker + deltaID
}

func (d *DatabricksDestination) newCDCFence(spec cdcFenceSpec) (*cdcFence, error) {
	if spec.expectedIncarnation == "" {
		return nil, errors.New("databricks conditional CDC fence requires an expected incarnation")
	}
	cat, sch, tbl := d.parseTableParts(spec.table)
	return &cdcFence{
		canonKey:            canonicalTargetKey(cat, sch, tbl),
		rawTable:            spec.table,
		catalog:             cat,
		schemaName:          sch,
		tableName:           tbl,
		targetFull:          quoteTableParts(cat, sch, tbl),
		expectedIncarnation: spec.expectedIncarnation,
	}, nil
}

func (d *DatabricksDestination) collectCDCFences(specs []cdcFenceSpec) ([]*cdcFence, error) {
	byCanon := make(map[string]*cdcFence, len(specs))
	for _, spec := range specs {
		f, err := d.newCDCFence(spec)
		if err != nil {
			return nil, err
		}
		if existing, ok := byCanon[f.canonKey]; ok {
			if existing.expectedIncarnation != f.expectedIncarnation {
				return nil, fmt.Errorf("conflicting expected incarnations for Databricks CDC target %q", spec.table)
			}
			continue
		}
		byCanon[f.canonKey] = f
	}
	fences := make([]*cdcFence, 0, len(byCanon))
	for _, f := range byCanon {
		fences = append(fences, f)
	}
	sort.Slice(fences, func(i, j int) bool { return fences[i].canonKey < fences[j].canonKey })
	return fences, nil
}

func incarnationChangedError(table string) error {
	return fmt.Errorf("databricks CDC target %q %w", table, errCDCIncarnationChanged)
}

func (d *DatabricksDestination) tableInfoSchemaMeta(ctx context.Context, catalog, schemaName, tableName string) (createdMicros, comment string, exists bool, err error) {
	query := fmt.Sprintf(
		"SELECT CAST(unix_micros(created) AS STRING), COALESCE(comment, '') FROM %s.`information_schema`.`tables` WHERE %s",
		quoteIdentifier(catalog), infoSchemaTablePredicate(catalog, schemaName, tableName),
	)
	res, err := d.queryStatement(ctx, query)
	if err != nil {
		if isDatabricksMissingError(err) {
			return "", "", false, nil
		}
		config.LogFailedQuery(query, err)
		return "", "", false, fmt.Errorf("read Databricks Unity Catalog identity for %s: %w", quoteTableParts(catalog, schemaName, tableName), err)
	}
	if len(res.rows) == 0 || len(res.rows[0]) == 0 {
		return "", "", false, nil
	}
	if len(res.rows) > 1 {
		return "", "", false, fmt.Errorf("databricks Unity Catalog returned %d rows for %s", len(res.rows), quoteTableParts(catalog, schemaName, tableName))
	}
	created := strings.TrimSpace(res.rows[0][0])
	if _, err := strconv.ParseInt(created, 10, 64); err != nil {
		return "", "", false, fmt.Errorf("parse Databricks Unity Catalog creation time %q for %s: %w", created, quoteTableParts(catalog, schemaName, tableName), err)
	}
	if len(res.rows[0]) > 1 {
		comment = res.rows[0][1]
	}
	return created, comment, true, nil
}

func infoSchemaTablePredicate(catalog, schemaName, tableName string) string {
	return fmt.Sprintf(
		"lower(table_catalog) = %s AND lower(table_schema) = %s AND lower(table_name) = %s",
		sqlString(strings.ToLower(catalog)),
		sqlString(strings.ToLower(schemaName)),
		sqlString(strings.ToLower(tableName)),
	)
}

func (d *DatabricksDestination) tableIdentityWithRecovery(ctx context.Context, table string) (catalog, schemaName, tableName, id string, exists bool, err error) {
	catalog, schemaName, tableName, id, exists, err = d.tableIdentity(ctx, table)
	if err != nil || exists {
		return catalog, schemaName, tableName, id, exists, err
	}
	recovered, recErr := d.recoverInterruptedSwap(ctx, catalog, schemaName, tableName, false)
	if recErr != nil || !recovered {
		return catalog, schemaName, tableName, "", false, recErr
	}
	return d.tableIdentity(ctx, table)
}

// ensureTableIncarnationStampLocked verifies (when requiredExpectedIncarnation
// is non-empty) or discovers the live table's Delta id, ensures the table
// comment carries __ingestr_cdc_incarnation=<id> while preserving any user
// comment prefix, and sandwiches DESCRIBE DETAIL checks around
// information_schema.tables reads so an outside replacement is never accepted.
// Callers must hold d.fenceMu.
func (d *DatabricksDestination) ensureTableIncarnationStampLocked(
	ctx context.Context,
	table, requiredExpectedIncarnation string,
) (incarnation, createdMicros, expectedComment string, exists bool, err error) {
	catalog, schemaName, tableName, id, exists, err := d.tableIdentityWithRecovery(ctx, table)
	if err != nil {
		return "", "", "", false, err
	}
	if !exists {
		if requiredExpectedIncarnation != "" {
			return "", "", "", false, incarnationChangedError(table)
		}
		return "", "", "", false, nil
	}
	inc := canonicalIncarnationKey(catalog, schemaName, tableName, id)
	if requiredExpectedIncarnation != "" && inc != requiredExpectedIncarnation {
		return "", "", "", true, incarnationChangedError(table)
	}

	beforeCreated, beforeComment, infoExists, err := d.tableInfoSchemaMeta(ctx, catalog, schemaName, tableName)
	if err != nil {
		return "", "", "", false, err
	}
	if !infoExists {
		if requiredExpectedIncarnation != "" {
			return "", "", "", false, incarnationChangedError(table)
		}
		return "", "", "", false, nil
	}

	_, verifySchema, verifyTable, verifyID, verifyExists, err := d.tableIdentity(ctx, table)
	_ = verifySchema
	_ = verifyTable
	if err != nil {
		return "", "", "", false, err
	}
	if !verifyExists || verifyID != id {
		return "", "", "", false, incarnationChangedError(table)
	}

	if _, markerID := splitCDCIncarnationComment(beforeComment); markerID == id {
		afterCreated, afterComment, afterExists, err := d.tableInfoSchemaMeta(ctx, catalog, schemaName, tableName)
		if err != nil {
			return "", "", "", false, err
		}
		if !afterExists || afterCreated != beforeCreated || afterComment != beforeComment {
			return "", "", "", false, incarnationChangedError(table)
		}
		return inc, afterCreated, afterComment, true, nil
	}

	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return "", "", "", false, err
	}

	fullTable := quoteTableParts(catalog, schemaName, tableName)
	stamped := formatCDCIncarnationComment(beforeComment, id)
	commentSQL := fmt.Sprintf("COMMENT ON TABLE %s IS %s", fullTable, sqlString(stamped))
	if err := d.executeStatement(ctx, commentSQL); err != nil {
		config.LogFailedQuery(commentSQL, err)
		if isDatabricksMissingError(err) {
			return "", "", "", false, incarnationChangedError(table)
		}
		return "", "", "", false, fmt.Errorf("stamp Databricks CDC incarnation comment on %s: %w", fullTable, err)
	}

	_, _, _, postID, postExists, err := d.tableIdentity(ctx, table)
	if err != nil {
		return "", "", "", false, err
	}
	if !postExists || postID != id {
		return "", "", "", false, incarnationChangedError(table)
	}

	afterCreated, afterComment, afterExists, err := d.tableInfoSchemaMeta(ctx, catalog, schemaName, tableName)
	if err != nil {
		return "", "", "", false, err
	}
	if !afterExists || afterCreated != beforeCreated || afterComment != stamped {
		return "", "", "", false, incarnationChangedError(table)
	}

	_, _, _, finalID, finalExists, err := d.tableIdentity(ctx, table)
	if err != nil {
		return "", "", "", false, err
	}
	if !finalExists || finalID != id {
		return "", "", "", false, incarnationChangedError(table)
	}

	return inc, afterCreated, afterComment, true, nil
}

func (d *DatabricksDestination) EnsureCDCTargetIncarnation(ctx context.Context, table string) (string, bool, error) {
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return "", false, err
	}
	d.fenceMu.Lock()
	defer d.fenceMu.Unlock()
	inc, _, _, exists, err := d.ensureTableIncarnationStampLocked(ctx, table, "")
	return inc, exists, err
}

func (d *DatabricksDestination) bindCDCFence(ctx context.Context, f *cdcFence) error {
	_, createdMicros, expectedComment, _, err := d.ensureTableIncarnationStampLocked(ctx, f.rawTable, f.expectedIncarnation)
	if err != nil {
		return err
	}
	f.createdMicros = createdMicros
	f.expectedComment = expectedComment
	return nil
}

func (d *DatabricksDestination) bindCDCFences(ctx context.Context, fences []*cdcFence) error {
	for _, f := range fences {
		if err := d.checkCDCIncarnation(ctx, f.rawTable, f.expectedIncarnation); err != nil {
			return err
		}
	}
	for _, f := range fences {
		if err := d.bindCDCFence(ctx, f); err != nil {
			return err
		}
	}
	return nil
}

func (d *DatabricksDestination) verifyCDCFencesAfterCommit(ctx context.Context, fences []*cdcFence) error {
	verifyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	for _, f := range fences {
		if err := d.checkCDCIncarnation(verifyCtx, f.rawTable, f.expectedIncarnation); err != nil {
			return err
		}
		created, comment, exists, err := d.tableInfoSchemaMeta(verifyCtx, f.catalog, f.schemaName, f.tableName)
		if err != nil {
			return err
		}
		if !exists || created != f.createdMicros || comment != f.expectedComment {
			return incarnationChangedError(f.rawTable)
		}
	}
	return nil
}

func (f *cdcFence) guardSQL() string {
	return fmt.Sprintf(
		"IF NOT EXISTS (SELECT 1 FROM %s.`information_schema`.`tables` WHERE %s AND unix_micros(created) = %s AND coalesce(comment, '') = %s) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = %s; END IF",
		quoteIdentifier(f.catalog),
		infoSchemaTablePredicate(f.catalog, f.schemaName, f.tableName),
		f.createdMicros,
		sqlString(f.expectedComment),
		sqlString(cdcIncarnationGuardMarker+": "+f.targetFull),
	)
}

// buildFencedAtomicSQL checks every fence before and after the DML inside one
// BEGIN ATOMIC block. The trailing guards catch a replacement that happened
// between the leading guard and DML name resolution: SIGNAL aborts the block
// and Databricks rolls back every statement in it.
func buildFencedAtomicSQL(fences []*cdcFence, statements []string) string {
	var b strings.Builder
	b.WriteString("BEGIN ATOMIC\n")
	writeStmt := func(stmt string) {
		b.WriteString("  ")
		b.WriteString(strings.TrimRight(strings.TrimSpace(stmt), ";"))
		b.WriteString(";\n")
	}
	for _, f := range fences {
		writeStmt(f.guardSQL())
	}
	for _, stmt := range statements {
		writeStmt(stmt)
	}
	for _, f := range fences {
		writeStmt(f.guardSQL())
	}
	b.WriteString("END;")
	return b.String()
}

func isCDCIncarnationGuardError(err error) bool {
	return err != nil && strings.Contains(err.Error(), cdcIncarnationGuardMarker)
}

func fenceGuardError(err error, operation string) error {
	return fmt.Errorf("databricks CDC target %w during %s; the transaction was rolled back: %w", errCDCIncarnationChanged, operation, err)
}

func randomTableSuffix() (string, error) {
	var suffixBytes [8]byte
	if _, err := rand.Read(suffixBytes[:]); err != nil {
		return "", fmt.Errorf("generate Databricks table suffix: %w", err)
	}
	return hex.EncodeToString(suffixBytes[:]), nil
}

func newPrivateTableName(tableName, op string) (string, error) {
	suffix, err := randomTableSuffix()
	if err != nil {
		return "", err
	}
	candidate := fmt.Sprintf("__ingestr_%s_%s_%s", op, strings.ToLower(tableName), suffix)
	return destination.ShortenIdentifier(candidate, candidate, destination.MaxIdentifierLength("databricks")), nil
}

// swapOldTableName is deterministic per target so a swap interrupted between
// its two renames can be found and undone by a later process.
func swapOldTableName(catalog, schemaName, tableName string) string {
	sum := sha256.Sum256([]byte(canonicalTargetKey(catalog, schemaName, tableName)))
	candidate := fmt.Sprintf("__ingestr_swap_old_%s_%s", strings.ToLower(tableName), hex.EncodeToString(sum[:6]))
	return destination.ShortenIdentifier(candidate, candidate, destination.MaxIdentifierLength("databricks"))
}

// recoverInterruptedSwap restores a target that a crashed SwapTable left under
// its deterministic swap-old name. When the target exists again, releaseName
// moves the leftover to a unique orphan name instead of dropping it, because
// it may be the only copy of data displaced by an outside replacement.
func (d *DatabricksDestination) recoverInterruptedSwap(ctx context.Context, catalog, schemaName, tableName string, releaseName bool) (bool, error) {
	if d.isSwapActive(catalog, schemaName, tableName) {
		return false, nil
	}
	oldFull := quoteTableParts(catalog, schemaName, swapOldTableName(catalog, schemaName, tableName))
	targetFull := quoteTableParts(catalog, schemaName, tableName)

	_, _, _, _, oldExists, err := d.tableIdentity(ctx, oldFull)
	if err != nil || !oldExists {
		return false, err
	}
	_, _, _, _, targetExists, err := d.tableIdentity(ctx, targetFull)
	if err != nil {
		return false, err
	}
	if targetExists {
		if !releaseName {
			return false, nil
		}
		suffix, err := randomTableSuffix()
		if err != nil {
			return false, err
		}
		orphanCandidate := fmt.Sprintf("__ingestr_swap_orphan_%s_%s", strings.ToLower(tableName), suffix)
		orphanName := destination.ShortenIdentifier(orphanCandidate, orphanCandidate, destination.MaxIdentifierLength("databricks"))
		orphanFull := quoteTableParts(catalog, schemaName, orphanName)
		moveSQL := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", oldFull, orphanFull)
		if err := d.executeStatement(ctx, moveSQL); err != nil {
			config.LogFailedQuery(moveSQL, err)
			return false, fmt.Errorf("move leftover Databricks swap table %s aside: %w", oldFull, err)
		}
		config.Debug("[DATABRICKS] Preserved leftover swap table %s as %s", oldFull, orphanFull)
		return false, nil
	}

	restoreSQL := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", oldFull, targetFull)
	if err := d.executeStatement(ctx, restoreSQL); err != nil {
		config.LogFailedQuery(restoreSQL, err)
		if isDatabricksAlreadyExistsError(err) || isDatabricksMissingError(err) {
			return false, nil
		}
		return false, fmt.Errorf("restore Databricks target %s from interrupted swap: %w", targetFull, err)
	}
	config.Debug("[DATABRICKS] Restored %s from interrupted swap table %s", targetFull, oldFull)
	return true, nil
}

func (d *DatabricksDestination) markSwapActive(catalog, schemaName, tableName string) func() {
	key := canonicalTargetKey(catalog, schemaName, tableName)
	d.swapMu.Lock()
	if d.activeSwaps == nil {
		d.activeSwaps = map[string]int{}
	}
	d.activeSwaps[key]++
	d.swapMu.Unlock()
	return func() {
		d.swapMu.Lock()
		defer d.swapMu.Unlock()
		if d.activeSwaps[key] <= 1 {
			delete(d.activeSwaps, key)
			return
		}
		d.activeSwaps[key]--
	}
}

func (d *DatabricksDestination) isSwapActive(catalog, schemaName, tableName string) bool {
	key := canonicalTargetKey(catalog, schemaName, tableName)
	d.swapMu.Lock()
	defer d.swapMu.Unlock()
	return d.activeSwaps[key] > 0
}

type describedColumn struct {
	name     string
	dataType string
}

func (d *DatabricksDestination) describeColumns(ctx context.Context, tableFull string) ([]describedColumn, error) {
	query := "DESCRIBE TABLE " + tableFull
	res, err := d.queryStatement(ctx, query)
	if err != nil {
		config.LogFailedQuery(query, err)
		return nil, fmt.Errorf("describe Databricks table %s: %w", tableFull, err)
	}
	colIdx := res.columnIndex("col_name")
	if colIdx < 0 {
		colIdx = 0
	}
	typeIdx := res.columnIndex("data_type")
	if typeIdx < 0 {
		typeIdx = 1
	}
	var cols []describedColumn
	for _, row := range res.rows {
		if colIdx >= len(row) {
			continue
		}
		name := strings.TrimSpace(row[colIdx])
		if name == "" || strings.HasPrefix(name, "#") {
			break
		}
		dataType := ""
		if typeIdx < len(row) {
			dataType = strings.TrimSpace(row[typeIdx])
		}
		cols = append(cols, describedColumn{name: name, dataType: dataType})
	}
	return cols, nil
}

func missingColumns(source, target []describedColumn) []describedColumn {
	existing := make(map[string]struct{}, len(target))
	for _, col := range target {
		existing[strings.ToLower(col.name)] = struct{}{}
	}
	var missing []describedColumn
	for _, col := range source {
		if _, ok := existing[strings.ToLower(col.name)]; !ok {
			missing = append(missing, col)
		}
	}
	return missing
}
