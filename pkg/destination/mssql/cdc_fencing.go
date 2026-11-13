package mssql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	mssqlAcquireAppLockSQL = `DECLARE @rc int; EXEC @rc = sys.sp_getapplock @Resource = @p1, @LockMode = 'Exclusive', @LockOwner = 'Session', @LockTimeout = 0; SELECT @rc;`
	mssqlCheckAppLockSQL   = `SELECT APPLOCK_MODE('public', @p1, 'Session')`
	mssqlReleaseAppLockSQL = `DECLARE @rc int; EXEC @rc = sys.sp_releaseapplock @Resource = @p1, @LockOwner = 'Session'; SELECT @rc;`
)

func (d *MSSQLDestination) SupportsCDCConditionalMerge() bool { return true }

func (d *MSSQLDestination) SupportsCDCConditionalSwap() bool { return true }

func (d *MSSQLDestination) TruncateCDCTable(ctx context.Context, table string) error {
	return d.TruncateTable(ctx, table)
}

func (d *MSSQLDestination) lockAndValidateCDCIncarnation(ctx context.Context, tx *sql.Tx, table, expectedIncarnation string) (mssqlTargetIdentity, error) {
	if expectedIncarnation == "" {
		return mssqlTargetIdentity{}, fmt.Errorf("SQL Server CDC target %q requires a non-empty expected incarnation", table)
	}
	identity, err := d.resolveTargetIdentityWith(ctx, tx, table)
	if err != nil {
		return mssqlTargetIdentity{}, err
	}
	resolvedTable := identity.qualifiedTable(d.server)
	lockSQL := buildTableIsEmptyForUpdateSQL(resolvedTable)
	var rowExists int
	if err := tx.QueryRowContext(ctx, lockSQL).Scan(&rowExists); err != nil && !errors.Is(err, sql.ErrNoRows) {
		config.LogFailedQuery(lockSQL, err)
		return mssqlTargetIdentity{}, fmt.Errorf("failed to lock SQL Server CDC target %q: %w", table, err)
	}
	current, exists, err := d.cdcTargetIncarnationWith(ctx, tx, resolvedTable)
	if err != nil {
		return mssqlTargetIdentity{}, err
	}
	if !exists || current != expectedIncarnation {
		return mssqlTargetIdentity{}, fmt.Errorf("SQL Server CDC target %q physical incarnation changed", table)
	}
	return identity, nil
}

func (d *MSSQLDestination) TruncateCDCTableIfIncarnation(ctx context.Context, table, expectedIncarnation string) error {
	if expectedIncarnation == "" {
		return fmt.Errorf("cannot conditionally truncate %s without a destination incarnation", table)
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin SQL Server conditional truncate transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	identity, err := d.lockAndValidateCDCIncarnation(ctx, tx, table, expectedIncarnation)
	if err != nil {
		return err
	}
	truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s", quoteTable(identity.qualifiedTable(d.server)))
	if _, err := tx.ExecContext(ctx, truncateSQL); err != nil {
		config.LogFailedQuery(truncateSQL, err)
		return fmt.Errorf("failed to truncate SQL Server CDC target %s: %w", table, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit SQL Server conditional truncate: %w", err)
	}
	return nil
}

func (d *MSSQLDestination) CDCConditionalSwapIncarnations(ctx context.Context, targetTable, stagingTable string) (string, string, error) {
	target, err := d.resolveTargetIdentity(ctx, targetTable)
	if err != nil {
		return "", "", err
	}
	staging, err := d.resolveTargetIdentity(ctx, stagingTable)
	if err != nil {
		return "", "", err
	}
	if !strings.EqualFold(target.server, staging.server) || !strings.EqualFold(target.database, staging.database) {
		return "", "", fmt.Errorf("cannot atomically swap SQL Server tables across catalogs: %q and %q", targetTable, stagingTable)
	}
	stagingIncarnation, exists, err := d.CDCTargetIncarnation(ctx, stagingTable)
	if err != nil {
		return "", "", err
	}
	if !exists || stagingIncarnation == "" {
		return "", "", fmt.Errorf("SQL Server CDC staging table %q has no durable physical incarnation", stagingTable)
	}
	// ALTER SCHEMA TRANSFER and sp_rename both preserve object_id and create_date,
	// so the staging incarnation becomes the target incarnation after swap.
	return stagingIncarnation, stagingIncarnation, nil
}

func (d *MSSQLDestination) ClaimAndPrepareEmptyCDCTarget(
	ctx context.Context,
	claimTable string,
	claim destination.CDCTargetClaim,
	opts destination.PrepareOptions,
) (string, error) {
	if opts.Schema == nil {
		return "", errors.New("schema is required")
	}
	ownerID, err := claim.OwnerID()
	if err != nil {
		return "", err
	}
	identity, err := d.resolveTargetIdentity(ctx, claim.DestinationTable)
	if err != nil {
		return "", err
	}
	if err := d.ensureTargetSchemaExists(ctx, identity); err != nil {
		return "", fmt.Errorf("failed to ensure schema exists: %w", err)
	}
	resolvedTable := identity.qualifiedTable(d.server)
	canonicalTarget, err := d.canonicalCDCTarget(ctx, claim.DestinationTable)
	if err != nil {
		return "", err
	}

	tx, err := d.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()

	var owner string
	selectSQL := fmt.Sprintf("SELECT [connector_id] FROM %s WITH (UPDLOCK, HOLDLOCK) WHERE [destination_table] = @p1", quoteTable(claimTable))
	err = tx.QueryRowContext(ctx, selectSQL, canonicalTarget).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		insertSQL := fmt.Sprintf("INSERT INTO %s ([destination_table], [connector_id], [claimed_at]) VALUES (@p1, @p2, SYSUTCDATETIME())", quoteTable(claimTable))
		if _, err := tx.ExecContext(ctx, insertSQL, canonicalTarget, ownerID); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	} else if owner != ownerID {
		return "", fmt.Errorf("destination table %q is already claimed by CDC connector %q", canonicalTarget, owner)
	}

	var createSQL string
	if identity.database == "" || strings.EqualFold(identity.database, d.database) {
		createSQL = buildExclusiveCreateTableSQLWithDeferredPrimaryKeys(resolvedTable, opts.Schema.Columns, opts.PrimaryKeys, opts.DeferredPrimaryKeys)
	} else {
		localCreate := buildExclusiveCreateTableSQLWithDeferredPrimaryKeys(identity.schemaQualifiedTable(), opts.Schema.Columns, opts.PrimaryKeys, opts.DeferredPrimaryKeys)
		createSQL, err = d.databaseScopedSQL(identity, localCreate)
		if err != nil {
			return "", err
		}
	}
	if _, err := tx.ExecContext(ctx, createSQL); err != nil {
		config.LogFailedQuery(createSQL, err)
		return "", fmt.Errorf("failed to exclusively create SQL Server CDC target: %w", err)
	}

	incarnation, exists, err := d.cdcTargetIncarnationWith(ctx, tx, resolvedTable)
	if err != nil {
		return "", err
	}
	if !exists || incarnation == "" {
		return "", errors.New("created SQL Server CDC target has no durable physical incarnation")
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return incarnation, nil
}

func (d *MSSQLDestination) MergeCDCTablesAtomically(ctx context.Context, merges []destination.CDCAtomicTableMerge) error {
	if len(merges) == 0 {
		return nil
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin multi-table SQL Server CDC transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, merge := range merges {
		resolvedTable := merge.Options.TargetTable
		if merge.Options.CDCExpectedIncarnation != "" {
			identity, err := d.lockAndValidateCDCIncarnation(ctx, tx, merge.Options.TargetTable, merge.Options.CDCExpectedIncarnation)
			if err != nil {
				return err
			}
			resolvedTable = identity.qualifiedTable(d.server)
		}
		if merge.Truncate {
			truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s", quoteTable(resolvedTable))
			if _, err := tx.ExecContext(ctx, truncateSQL); err != nil {
				config.LogFailedQuery(truncateSQL, err)
				return fmt.Errorf("failed to reset SQL Server CDC target %s: %w", merge.Options.TargetTable, err)
			}
		}
		if err := d.mergeTableInTx(ctx, tx, merge.Options); err != nil {
			return fmt.Errorf("failed to merge SQL Server CDC target %s: %w", merge.Options.TargetTable, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit multi-table SQL Server CDC transaction: %w", err)
	}
	return nil
}

type mssqlManagedCDCRunLease struct {
	conn        *sql.Conn
	resource    string
	done        chan struct{}
	stop        chan struct{}
	stopped     chan struct{}
	doneOnce    sync.Once
	releaseOnce sync.Once
	mu          sync.Mutex
	err         error
	releaseErr  error
}

// AcquireManagedCDCRunLease serializes target mutations for one managed CDC
// connector via a session-scoped sp_getapplock held on a dedicated connection.
func (d *MSSQLDestination) AcquireManagedCDCRunLease(ctx context.Context, connectorID string) (source.ConnectorLease, error) {
	if connectorID == "" {
		return nil, errors.New("managed CDC connector ID is empty")
	}
	conn, err := d.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to reserve SQL Server CDC lease connection: %w", err)
	}
	resource := mssqlCDCRunLeaseResource(connectorID)
	var resultCode int
	if err := conn.QueryRowContext(ctx, mssqlAcquireAppLockSQL, resource).Scan(&resultCode); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to acquire SQL Server CDC run lease: %w", err)
	}
	if resultCode == -1 {
		_ = conn.Close()
		return nil, fmt.Errorf("another SQL Server CDC run already owns connector %s", connectorID)
	}
	if resultCode < 0 {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to acquire SQL Server CDC run lease (sp_getapplock returned %d)", resultCode)
	}
	lease := &mssqlManagedCDCRunLease{
		conn:     conn,
		resource: resource,
		done:     make(chan struct{}),
		stop:     make(chan struct{}),
		stopped:  make(chan struct{}),
	}
	go lease.monitor()
	return lease, nil
}

func mssqlCDCRunLeaseResource(connectorID string) string {
	// sp_getapplock restricts @Resource to nvarchar(255).
	sum := sha256.Sum256([]byte(connectorID))
	return "ingestr_cdc_" + hex.EncodeToString(sum[:])
}

func (l *mssqlManagedCDCRunLease) monitor() {
	defer close(l.stopped)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-l.stop:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			var mode sql.NullString
			err := l.conn.QueryRowContext(ctx, mssqlCheckAppLockSQL, l.resource).Scan(&mode)
			cancel()
			if err == nil && mode.Valid && strings.EqualFold(mode.String, "Exclusive") {
				continue
			}
			if err == nil {
				err = errors.New("application lock is no longer held by this session")
			}
			l.mu.Lock()
			l.err = errors.Join(source.ErrConnectorLeaseLost, fmt.Errorf("SQL Server CDC run lease lost: %w", err))
			l.mu.Unlock()
			l.doneOnce.Do(func() { close(l.done) })
			return
		}
	}
}

func (l *mssqlManagedCDCRunLease) Done() <-chan struct{} {
	return l.done
}

func (l *mssqlManagedCDCRunLease) Err() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.err
}

func (l *mssqlManagedCDCRunLease) Release() error {
	l.releaseOnce.Do(func() {
		close(l.stop)
		<-l.stopped
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var resultCode int
		if err := l.conn.QueryRowContext(ctx, mssqlReleaseAppLockSQL, l.resource).Scan(&resultCode); err != nil {
			l.releaseErr = fmt.Errorf("failed to release SQL Server CDC run lease: %w", err)
		} else if resultCode < 0 {
			l.releaseErr = fmt.Errorf("failed to release SQL Server CDC run lease (sp_releaseapplock returned %d)", resultCode)
		}
		if err := l.conn.Close(); err != nil && l.releaseErr == nil {
			l.releaseErr = err
		}
		l.doneOnce.Do(func() { close(l.done) })
	})
	return l.releaseErr
}
