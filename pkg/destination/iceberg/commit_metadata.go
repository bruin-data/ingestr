package iceberg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	iceberggo "github.com/apache/iceberg-go"
	icebergcatalog "github.com/apache/iceberg-go/catalog"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/bruin-data/ingestr/pkg/destination"
)

const (
	snapshotCommitTokenKey    = "ingestr.commit-token"
	snapshotCDCResumeLSNKey   = "ingestr.cdc-resume-lsn"
	snapshotCDCResetKey       = "ingestr.cdc-resume-reset"
	tableCommitTokenLedgerKey = "ingestr.commit-token-ledger"
	tableCDCResumeStateKey    = "ingestr.cdc-resume-state"
	commitReconcileTimeout    = 2 * time.Second
	commitTokenLedgerLimit    = 1024
)

type commitMetadata struct {
	token               string
	cdcResumeLSN        string
	resetCDCResume      bool
	expectedIncarnation string
}

type durableCDCResumeState struct {
	Position string `json:"position,omitempty"`
	Reset    bool   `json:"reset,omitempty"`
}

func newCommitMetadata(token any, cdcResumeLSN string) commitMetadata {
	metadata := commitMetadata{
		token:        commitTokenID(token),
		cdcResumeLSN: strings.TrimSpace(cdcResumeLSN),
	}
	return metadata
}

func mergeCommitMetadata(opts destination.MergeOptions) commitMetadata {
	metadata := newCommitMetadata(opts.CommitToken, opts.CDCResumeLSN)
	metadata.expectedIncarnation = opts.CDCExpectedIncarnation
	if opts.SkipCDCResume {
		metadata.cdcResumeLSN = ""
		metadata.resetCDCResume = true
	}
	return metadata
}

func (m commitMetadata) withExpectedIncarnation(expected string) commitMetadata {
	m.expectedIncarnation = expected
	return m
}

func (m commitMetadata) withCDCResumeLSN(lsn string) commitMetadata {
	if m.cdcResumeLSN != "" {
		return m
	}
	lsn = strings.TrimSpace(lsn)
	if lsn == "" {
		return m
	}
	m.cdcResumeLSN = lsn
	return m
}

func (m commitMetadata) empty() bool {
	return m.token == "" && m.cdcResumeLSN == "" && !m.resetCDCResume
}

func commitTokenID(token any) string {
	if token == nil {
		return ""
	}

	encoded, err := json.Marshal(token)
	if err != nil || string(encoded) == "{}" {
		encoded = []byte(fmt.Sprintf("%#v", token))
	}
	payload := append([]byte(fmt.Sprintf("%T:", token)), encoded...)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func snapshotProps(operation string, metadata ...commitMetadata) iceberggo.Properties {
	props := iceberggo.Properties{
		"ingestr.destination": "iceberg",
		"ingestr.operation":   operation,
	}
	if operation == "replace" || operation == "truncate" || operation == "truncate+insert" || operation == "snapshot" {
		props[snapshotCDCResetKey] = "true"
	}
	if len(metadata) == 0 {
		return props
	}
	if metadata[0].token != "" {
		props[snapshotCommitTokenKey] = metadata[0].token
	}
	if metadata[0].cdcResumeLSN != "" {
		props[snapshotCDCResumeLSNKey] = metadata[0].cdcResumeLSN
	}
	if metadata[0].resetCDCResume {
		props[snapshotCDCResetKey] = "true"
	}
	return props
}

func observeCDCResumeLSN(props iceberggo.Properties, batch arrow.RecordBatch, preserveToken bool) {
	lsn := maxCDCResumeLSNInBatch(batch)
	if lsn == "" || compareCDCResumeLSN(lsn, props[snapshotCDCResumeLSNKey]) <= 0 {
		return
	}
	props[snapshotCDCResumeLSNKey] = lsn
	if !preserveToken {
		delete(props, snapshotCommitTokenKey)
	}
}

func maxCDCResumeLSNInBatch(batch arrow.RecordBatch) string {
	idx := -1
	for i, field := range batch.Schema().Fields() {
		if strings.EqualFold(field.Name, destination.CDCLSNColumn) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ""
	}

	values, ok := batch.Column(idx).(interface {
		arrow.Array
		Value(int) string
	})
	if !ok {
		return ""
	}
	var maxLSN string
	for i := 0; i < values.Len(); i++ {
		if values.IsNull(i) {
			continue
		}
		if value := values.Value(i); compareCDCResumeLSN(value, maxLSN) > 0 {
			maxLSN = value
		}
	}
	return maxLSN
}

func compareCDCResumeLSN(left, right string) int {
	return destination.CompareCDCPositions(left, right)
}

func validateCDCResumeAdvance(tbl *icebergtable.Table, metadata commitMetadata) error {
	if metadata.cdcResumeLSN == "" || tableHasCommitToken(tbl, metadata.token) {
		return nil
	}
	current := latestCDCResumeLSN(tbl)
	// A PostgreSQL transaction's commit LSN can equal the replication slot's
	// snapshot consistent point. The cursor is therefore a monotonic boundary,
	// not the identity of the write: equal positions may carry distinct rows,
	// while the stable commit token provides exactly-once behavior.
	if current != "" && compareCDCResumeLSN(metadata.cdcResumeLSN, current) < 0 {
		return fmt.Errorf(
			"iceberg: stale CDC resume position %q is older than durable position %q",
			metadata.cdcResumeLSN,
			current,
		)
	}
	return nil
}

func snapshotInCurrentLineage(tbl *icebergtable.Table, visit func(*icebergtable.Snapshot) bool) bool {
	snapshot := tbl.CurrentSnapshot()
	seen := make(map[int64]struct{})
	for snapshot != nil {
		if _, ok := seen[snapshot.SnapshotID]; ok {
			return false
		}
		seen[snapshot.SnapshotID] = struct{}{}
		if visit(snapshot) {
			return true
		}
		if snapshot.ParentSnapshotID == nil {
			return false
		}
		snapshot = tbl.SnapshotByID(*snapshot.ParentSnapshotID)
	}
	return false
}

func tableHasCommitToken(tbl *icebergtable.Table, token string) bool {
	if token == "" {
		return false
	}
	var ledger []string
	if err := json.Unmarshal([]byte(tbl.Properties()[tableCommitTokenLedgerKey]), &ledger); err == nil {
		for _, existing := range ledger {
			if existing == token {
				return true
			}
		}
	}
	snapshot := tbl.CurrentSnapshot()
	seen := make(map[int64]struct{})
	for snapshot != nil {
		if _, ok := seen[snapshot.SnapshotID]; ok {
			return false
		}
		seen[snapshot.SnapshotID] = struct{}{}
		if snapshot.Summary != nil {
			if snapshot.Summary.Properties[snapshotCommitTokenKey] == token {
				return true
			}
			if snapshot.Summary.Properties[snapshotCDCResetKey] == "true" {
				return false
			}
		}
		if snapshot.ParentSnapshotID == nil {
			return false
		}
		snapshot = tbl.SnapshotByID(*snapshot.ParentSnapshotID)
	}
	return false
}

func currentSnapshotHasCommitToken(tbl *icebergtable.Table, token string) bool {
	if token == "" || tbl == nil || tbl.CurrentSnapshot() == nil || tbl.CurrentSnapshot().Summary == nil {
		return false
	}
	return tbl.CurrentSnapshot().Summary.Properties[snapshotCommitTokenKey] == token
}

func lineageHasCommitTokenAfterSnapshot(tbl *icebergtable.Table, token string, baseSnapshotID int64) bool {
	if token == "" || tbl == nil {
		return false
	}
	snapshot := tbl.CurrentSnapshot()
	seen := make(map[int64]struct{})
	for snapshot != nil && snapshot.SnapshotID != baseSnapshotID {
		if _, ok := seen[snapshot.SnapshotID]; ok {
			return false
		}
		seen[snapshot.SnapshotID] = struct{}{}
		if snapshot.Summary != nil && snapshot.Summary.Properties[snapshotCommitTokenKey] == token {
			return true
		}
		if snapshot.ParentSnapshotID == nil {
			return false
		}
		snapshot = tbl.SnapshotByID(*snapshot.ParentSnapshotID)
	}
	return false
}

func stageCommitTokenLedger(txn *icebergtable.Transaction, tbl *icebergtable.Table, token string) error {
	if token == "" {
		return nil
	}
	var ledger []string
	_ = json.Unmarshal([]byte(tbl.Properties()[tableCommitTokenLedgerKey]), &ledger)
	for _, existing := range ledger {
		if existing == token {
			return nil
		}
	}
	ledger = append(ledger, token)
	if len(ledger) > commitTokenLedgerLimit {
		ledger = append([]string(nil), ledger[len(ledger)-commitTokenLedgerLimit:]...)
	}
	encoded, err := json.Marshal(ledger)
	if err != nil {
		return fmt.Errorf("iceberg: failed to encode commit-token ledger: %w", err)
	}
	if err := txn.SetProperties(iceberggo.Properties{tableCommitTokenLedgerKey: string(encoded)}); err != nil {
		return fmt.Errorf("iceberg: failed to stage commit-token ledger: %w", err)
	}
	return nil
}

func stageResetCommitTokenLedger(txn *icebergtable.Transaction, token string) error {
	ledger := []string{}
	if token != "" {
		ledger = append(ledger, token)
	}
	encoded, err := json.Marshal(ledger)
	if err != nil {
		return fmt.Errorf("iceberg: failed to encode reset commit-token ledger: %w", err)
	}
	if err := txn.SetProperties(iceberggo.Properties{tableCommitTokenLedgerKey: string(encoded)}); err != nil {
		return fmt.Errorf("iceberg: failed to reset commit-token ledger: %w", err)
	}
	return nil
}

func stageCDCResumeState(txn *icebergtable.Transaction, props iceberggo.Properties) error {
	state := durableCDCResumeState{Position: props[snapshotCDCResumeLSNKey]}
	if state.Position == "" {
		state.Reset = props[snapshotCDCResetKey] == "true"
		if !state.Reset {
			return nil
		}
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("iceberg: failed to encode durable CDC resume state: %w", err)
	}
	if err := txn.SetProperties(iceberggo.Properties{tableCDCResumeStateKey: string(encoded)}); err != nil {
		return fmt.Errorf("iceberg: failed to stage durable CDC resume state: %w", err)
	}
	return nil
}

func latestCDCResumeLSN(tbl *icebergtable.Table) string {
	if raw, ok := tbl.Properties()[tableCDCResumeStateKey]; ok {
		var state durableCDCResumeState
		if json.Unmarshal([]byte(raw), &state) == nil {
			return strings.TrimSpace(state.Position)
		}
	}
	var result string
	snapshotInCurrentLineage(tbl, func(snapshot *icebergtable.Snapshot) bool {
		if snapshot.Summary == nil {
			return false
		}
		result = snapshot.Summary.Properties[snapshotCDCResumeLSNKey]
		if result != "" {
			return true
		}
		operation := snapshot.Summary.Properties["ingestr.operation"]
		return snapshot.Summary.Properties[snapshotCDCResetKey] == "true" ||
			operation == "replace" || operation == "truncate" || operation == "truncate+insert"
	})
	return result
}

func (d *Destination) reconcileCommit(
	ctx context.Context,
	table string,
	token string,
	expectedIncarnation string,
	commitErr error,
) error {
	return d.reconcileCommitMatching(ctx, table, token, expectedIncarnation, commitErr, tableHasCommitToken)
}

func (d *Destination) reconcileCommitAfterSnapshot(
	ctx context.Context,
	table string,
	token string,
	baseSnapshotID int64,
	expectedIncarnation string,
	commitErr error,
) error {
	return d.reconcileCommitMatching(ctx, table, token, expectedIncarnation, commitErr, func(tbl *icebergtable.Table, token string) bool {
		return lineageHasCommitTokenAfterSnapshot(tbl, token, baseSnapshotID)
	})
}

func (d *Destination) reconcileCommitMatching(
	ctx context.Context,
	table string,
	token string,
	expectedIncarnation string,
	commitErr error,
	committed func(*icebergtable.Table, string) bool,
) error {
	if commitErr == nil || token == "" {
		return commitErr
	}

	reconcileCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), commitReconcileTimeout)
	defer cancel()
	var reconcileErr error
	for attempt := 0; ; attempt++ {
		tbl, err := d.loadIcebergTable(reconcileCtx, table)
		if err == nil {
			if err := d.validateExpectedIncarnation(reconcileCtx, tbl, expectedIncarnation); err != nil {
				return err
			}
			if committed(tbl, token) {
				return nil
			}
		}
		if err != nil {
			reconcileErr = fmt.Errorf("iceberg: failed to reconcile commit token for table %s: %w", table, err)
		}
		wait := min(25*time.Millisecond<<min(attempt, 5), 500*time.Millisecond)
		timer := time.NewTimer(wait)
		select {
		case <-reconcileCtx.Done():
			timer.Stop()
			if reconcileErr != nil {
				return errors.Join(commitErr, reconcileErr)
			}
			return commitErr
		case <-timer.C:
		}
	}
}

func (d *Destination) commitMetadataOnly(ctx context.Context, tbl *icebergtable.Table, operation string, metadata commitMetadata) error {
	if err := d.validateExpectedIncarnation(ctx, tbl, metadata.expectedIncarnation); err != nil {
		return err
	}
	if metadata.empty() || tableHasCommitToken(tbl, metadata.token) {
		return nil
	}
	if metadata.cdcResumeLSN != "" && compareCDCResumeLSN(metadata.cdcResumeLSN, latestCDCResumeLSN(tbl)) == 0 {
		return nil
	}
	if err := validateCDCResumeAdvance(tbl, metadata); err != nil {
		return err
	}

	writeSchema, err := tableWriteSchema(tbl)
	if err != nil {
		return err
	}
	reader, err := array.NewRecordReader(writeSchema, nil)
	if err != nil {
		return fmt.Errorf("iceberg: failed to create checkpoint reader: %w", err)
	}
	defer reader.Release()

	props := snapshotProps(operation, metadata)
	_, commitErr := d.commitTokenizedAppend(ctx, tbl, props, metadata.expectedIncarnation, func(txn *icebergtable.Transaction) error {
		return txn.Append(ctx, reader, props)
	})
	table := strings.Join(tbl.Identifier(), ".")
	if err := d.reconcileCommit(ctx, table, metadata.token, metadata.expectedIncarnation, commitErr); err != nil {
		return fmt.Errorf("iceberg: failed to commit %s on table %s: %w", operation, table, err)
	}
	d.afterSuccessfulCommitExpected(ctx, table, metadata.expectedIncarnation)
	return nil
}

func (d *Destination) ensureManagedCDCResumeResetExpected(
	ctx context.Context,
	tbl *icebergtable.Table,
	dataToken string,
	expectedIncarnation string,
) error {
	if err := d.validateExpectedIncarnation(ctx, tbl, expectedIncarnation); err != nil {
		return err
	}
	cursor := latestCDCResumeLSN(tbl)
	if cursor == "" {
		return nil
	}
	metadata := newCommitMetadata(struct {
		Kind      string
		DataToken string
		Cursor    string
	}{Kind: "managed-cdc-resume-reset", DataToken: dataToken, Cursor: cursor}, "")
	metadata.resetCDCResume = true
	metadata.expectedIncarnation = expectedIncarnation
	return d.commitMetadataOnly(ctx, tbl, "managed-cdc-reset", metadata)
}

// CommitWriteToken records an otherwise row-less streaming checkpoint in an
// empty Iceberg append snapshot. The snapshot changes metadata only and keeps
// the table's data files unchanged.
func (d *Destination) CommitWriteToken(ctx context.Context, table string, commitToken any, cdcResumeLSN string) error {
	if d.catalog == nil {
		return errors.New("iceberg destination not connected")
	}
	tbl, err := d.loadIcebergTable(ctx, table)
	if err != nil {
		return err
	}
	return d.commitMetadataOnly(ctx, tbl, "checkpoint", newCommitMetadata(commitToken, cdcResumeLSN))
}

// GetMaxCDCLSN reads the newest durable CDC checkpoint from snapshot metadata.
// Walking only the current snapshot lineage avoids returning a cursor from an
// abandoned branch or a superseded full refresh.
func (d *Destination) GetMaxCDCLSN(ctx context.Context, table string) (string, error) {
	if d.catalog == nil {
		return "", errors.New("iceberg destination not connected")
	}
	ident, err := parseIdentifier(table)
	if err != nil {
		return "", err
	}
	tbl, err := d.catalog.LoadTable(ctx, ident)
	if err != nil {
		if errors.Is(err, icebergcatalog.ErrNoSuchTable) || errors.Is(err, icebergcatalog.ErrNoSuchNamespace) {
			return "", nil
		}
		return "", fmt.Errorf("iceberg: failed to load table %s: %w", table, err)
	}
	return latestCDCResumeLSN(tbl), nil
}
