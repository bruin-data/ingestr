package redshift

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/schemaevolution"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pgx/v5"
)

const cdcEvolutionJournalTableName = "cdc_evolution_journal"

type evolutionJournalEntry struct {
	targetSchema        string
	targetTable         string
	guardTable          string
	expectedIncarnation string
}

func (d *RedshiftDestination) MaxDecimalPrecision() int {
	return 38
}

func (d *RedshiftDestination) markEvolutionGuardInFlight(guardName string) {
	d.evolveMu.Lock()
	defer d.evolveMu.Unlock()
	if d.inFlightGuards == nil {
		d.inFlightGuards = make(map[string]struct{})
	}
	d.inFlightGuards[guardName] = struct{}{}
}

func (d *RedshiftDestination) unmarkEvolutionGuardInFlight(guardName string) {
	d.evolveMu.Lock()
	defer d.evolveMu.Unlock()
	delete(d.inFlightGuards, guardName)
}

func (d *RedshiftDestination) isEvolutionGuardInFlight(guardName string) bool {
	d.evolveMu.Lock()
	defer d.evolveMu.Unlock()
	_, ok := d.inFlightGuards[guardName]
	return ok
}

func (d *RedshiftDestination) ensureEvolutionJournalTable(ctx context.Context, queryer redshiftQueryer) (string, error) {
	if err := ensureSchemaExists(ctx, queryer, defaultManagedStagingSchema); err != nil {
		return "", err
	}
	journalRef := quoteTable(defaultManagedStagingSchema, cdcEvolutionJournalTableName)
	createSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			"target_schema" VARCHAR(1024) NOT NULL,
			"target_table" VARCHAR(1024) NOT NULL,
			"guard_table" VARCHAR(1024) NOT NULL,
			"expected_incarnation" VARCHAR(128) NOT NULL,
			"owner_token" VARCHAR(128) NOT NULL,
			"expires_at" TIMESTAMP WITHOUT TIME ZONE NOT NULL,
			"created_at" TIMESTAMP WITHOUT TIME ZONE NOT NULL
		)
	`, journalRef)
	if _, err := queryer.Exec(ctx, createSQL); err != nil {
		return "", fmt.Errorf("failed to prepare Redshift schema evolution journal table: %w", err)
	}
	if err := ensureEvolutionJournalColumns(ctx, queryer, journalRef); err != nil {
		return "", err
	}
	return journalRef, nil
}

func ensureEvolutionJournalColumns(ctx context.Context, queryer redshiftQueryer, journalRef string) error {
	hasOwnerToken, err := columnExists(ctx, queryer, defaultManagedStagingSchema, cdcEvolutionJournalTableName, "owner_token")
	if err != nil {
		return fmt.Errorf("failed to inspect owner_token column on %s: %w", journalRef, err)
	}
	if !hasOwnerToken {
		addOwnerSQL := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN "owner_token" VARCHAR(128) NOT NULL DEFAULT ''`, journalRef)
		if _, err := queryer.Exec(ctx, addOwnerSQL); err != nil {
			return fmt.Errorf("failed to add owner_token column to %s: %w", journalRef, err)
		}
	}
	hasExpiresAt, err := columnExists(ctx, queryer, defaultManagedStagingSchema, cdcEvolutionJournalTableName, "expires_at")
	if err != nil {
		return fmt.Errorf("failed to inspect expires_at column on %s: %w", journalRef, err)
	}
	if !hasExpiresAt {
		addExpiresSQL := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN "expires_at" TIMESTAMP WITHOUT TIME ZONE NOT NULL DEFAULT '1970-01-01 00:00:00'`, journalRef)
		if _, err := queryer.Exec(ctx, addExpiresSQL); err != nil {
			return fmt.Errorf("failed to add expires_at column to %s: %w", journalRef, err)
		}
	}
	return nil
}

func evolutionJournalEntryExists(ctx context.Context, queryer redshiftQueryer, journalRef, targetSchema, targetTable, guardTable string) (bool, error) {
	var expectedInc string
	err := queryer.QueryRow(ctx, fmt.Sprintf(
		`SELECT "expected_incarnation" FROM %s WHERE "target_schema" = $1 AND "target_table" = $2 AND "guard_table" = $3 LIMIT 1`,
		journalRef,
	), targetSchema, targetTable, guardTable).Scan(&expectedInc)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to query Redshift schema evolution journal for %s.%s: %w", targetSchema, targetTable, err)
	}
	return true, nil
}

func (d *RedshiftDestination) evolutionLeaseTiming() (int, time.Duration) {
	ttl := d.leaseTTL
	if ttl <= 0 {
		ttl = defaultRedshiftLeaseTTL
	}
	overdueAfter := max(time.Millisecond, ttl/2)
	renewEvery := d.leaseRenewInterval
	if renewEvery <= 0 || renewEvery >= overdueAfter {
		renewEvery = max(time.Millisecond, ttl/6)
	}
	ttlSeconds := max(1, int(math.Ceil(ttl.Seconds())))
	return ttlSeconds, renewEvery
}

func renewEvolutionJournalLease(
	ctx context.Context,
	db redshiftDB,
	journalRef, targetSchema, targetTable, guardTable, ownerToken string,
	ttlSeconds int,
) (bool, error) {
	updateSQL := fmt.Sprintf(
		`UPDATE %s SET "expires_at" = DATEADD(second, %d, CURRENT_TIMESTAMP) WHERE "target_schema" = $1 AND "target_table" = $2 AND "guard_table" = $3 AND "owner_token" = $4 AND "expires_at" > CURRENT_TIMESTAMP`,
		journalRef, ttlSeconds,
	)
	tag, err := db.Exec(ctx, updateSQL, targetSchema, targetTable, guardTable, ownerToken)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func expireEvolutionJournalLeaseBestEffort(db redshiftDB, journalRef, targetSchema, targetTable, guardTable, ownerToken string) {
	expCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	expireSQL := fmt.Sprintf(
		`UPDATE %s SET "expires_at" = CURRENT_TIMESTAMP WHERE "target_schema" = $1 AND "target_table" = $2 AND "guard_table" = $3 AND "owner_token" = $4`,
		journalRef,
	)
	_, _ = db.Exec(expCtx, expireSQL, targetSchema, targetTable, guardTable, ownerToken)
}

func startEvolutionJournalHeartbeat(
	db redshiftDB,
	journalRef, targetSchema, targetTable, guardTable, ownerToken string,
	ttlSeconds int,
	renewEvery time.Duration,
) func() {
	stop := make(chan struct{})
	var once sync.Once
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(renewEvery)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				renewCtx, cancel := context.WithTimeout(context.Background(), renewEvery)
				owned, err := renewEvolutionJournalLease(renewCtx, db, journalRef, targetSchema, targetTable, guardTable, ownerToken, ttlSeconds)
				cancel()
				if err == nil && !owned {
					return
				}
			}
		}
	}()
	return func() {
		once.Do(func() {
			close(stop)
			wg.Wait()
		})
	}
}

func (d *RedshiftDestination) recoverInterruptedSchemaEvolutions(ctx context.Context, db redshiftDB) error {
	if db == nil {
		return nil
	}
	exists, err := tableExists(ctx, db, defaultManagedStagingSchema, cdcEvolutionJournalTableName)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	journalRef := quoteTable(defaultManagedStagingSchema, cdcEvolutionJournalTableName)
	if err := ensureEvolutionJournalColumns(ctx, db, journalRef); err != nil {
		return err
	}
	rows, err := db.Query(ctx, fmt.Sprintf(
		`SELECT "target_schema", "target_table", "guard_table", "expected_incarnation" FROM %s WHERE "expires_at" <= CURRENT_TIMESTAMP ORDER BY "created_at" DESC`,
		journalRef,
	))
	if err != nil {
		return fmt.Errorf("failed to list Redshift schema evolution journal entries: %w", err)
	}
	var entries []evolutionJournalEntry
	for rows.Next() {
		var e evolutionJournalEntry
		if scanErr := rows.Scan(&e.targetSchema, &e.targetTable, &e.guardTable, &e.expectedIncarnation); scanErr != nil {
			rows.Close()
			return fmt.Errorf("failed to scan Redshift schema evolution journal entry: %w", scanErr)
		}
		entries = append(entries, e)
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		return fmt.Errorf("failed to iterate Redshift schema evolution journal entries: %w", rowsErr)
	}

	for _, entry := range entries {
		if d.isEvolutionGuardInFlight(entry.guardTable) {
			continue
		}
		if err := d.recoverJournalEntry(ctx, entry); err != nil {
			return err
		}
	}
	return nil
}

func (d *RedshiftDestination) recoverJournalEntry(ctx context.Context, entry evolutionJournalEntry) error {
	journalRef := quoteTable(defaultManagedStagingSchema, cdcEvolutionJournalTableName)
	guardRef := quoteTable(entry.targetSchema, entry.guardTable)

	return d.withTx(ctx, func(tx redshiftTx) error {
		if _, err := tx.Exec(ctx, "LOCK "+journalRef); err != nil {
			return fmt.Errorf("failed to lock Redshift schema evolution journal %s: %w", journalRef, err)
		}

		var currentExpectedInc string
		var expired bool
		err := tx.QueryRow(ctx, fmt.Sprintf(
			`SELECT "expected_incarnation", ("expires_at" <= CURRENT_TIMESTAMP) FROM %s WHERE "target_schema" = $1 AND "target_table" = $2 AND "guard_table" = $3 LIMIT 1`,
			journalRef,
		), entry.targetSchema, entry.targetTable, entry.guardTable).Scan(&currentExpectedInc, &expired)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to inspect Redshift schema evolution journal entry for %s.%s: %w", entry.targetSchema, entry.targetTable, err)
		}
		if !expired {
			return nil
		}

		deleteEntry := func() error {
			delSQL := fmt.Sprintf(
				`DELETE FROM %s WHERE "target_schema" = $1 AND "target_table" = $2 AND "guard_table" = $3`,
				journalRef,
			)
			if _, delErr := tx.Exec(ctx, delSQL, entry.targetSchema, entry.targetTable, entry.guardTable); delErr != nil {
				return fmt.Errorf("failed to delete Redshift schema evolution journal entry for %s.%s: %w", entry.targetSchema, entry.targetTable, delErr)
			}
			return nil
		}

		_, guardExists, err := d.targetIncarnationInTx(ctx, tx, entry.targetSchema, entry.guardTable)
		if err != nil {
			return err
		}
		if !guardExists {
			return deleteEntry()
		}

		if _, err := tx.Exec(ctx, "LOCK "+guardRef); err != nil {
			return fmt.Errorf("failed to lock staged Redshift table %s during recovery: %w", guardRef, err)
		}
		guardInc, guardExists, err := d.targetIncarnationInTx(ctx, tx, entry.targetSchema, entry.guardTable)
		if err != nil {
			return err
		}
		if !guardExists {
			return deleteEntry()
		}

		_, targetExists, err := d.targetIncarnationInTx(ctx, tx, entry.targetSchema, entry.targetTable)
		if err != nil {
			return err
		}
		if targetExists {
			return deleteEntry()
		}

		if currentExpectedInc != "" && guardInc != currentExpectedInc {
			return deleteEntry()
		}

		renameSQL := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", guardRef, destination.QuoteIdentifier(entry.targetTable))
		if _, err := tx.Exec(ctx, renameSQL); err != nil {
			return fmt.Errorf("failed to recover interrupted Redshift schema evolution table %s to %s.%s: %w", guardRef, entry.targetSchema, entry.targetTable, err)
		}
		return deleteEntry()
	})
}

// ApplySchemaEvolution renders the abstract schema-change plan into this
// destination's DDL using the local dialect and applies each statement.
func (d *RedshiftDestination) ApplySchemaEvolution(ctx context.Context, table string, comparison *schemaevolution.SchemaComparison) ([]string, error) {
	if db, err := d.requireDB(); err == nil {
		if recErr := d.recoverInterruptedSchemaEvolutions(ctx, db); recErr != nil {
			return nil, recErr
		}
	}
	return destination.ApplyEvolution(ctx, d, &Dialect{}, table, comparison)
}

func newRedshiftEvolutionGuardName() (string, error) {
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", fmt.Errorf("failed to generate Redshift schema evolution guard name: %w", err)
	}
	return "__ingestr_evolve_" + hex.EncodeToString(token[:]), nil
}

func (d *RedshiftDestination) ApplySchemaEvolutionIfIncarnation(
	ctx context.Context,
	table string,
	comparison *schemaevolution.SchemaComparison,
	expectedIncarnation string,
) ([]string, string, error) {
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return nil, "", err
	}
	if expectedIncarnation == "" {
		return nil, "", fmt.Errorf("cannot conditionally evolve %s without a destination incarnation", table)
	}
	db, err := d.requireDB()
	if err != nil {
		return nil, "", err
	}
	if err := d.recoverInterruptedSchemaEvolutions(ctx, db); err != nil {
		return nil, "", err
	}

	_, warnings, err := destination.RenderEvolution(&Dialect{}, table, comparison)
	if err != nil {
		return nil, "", err
	}

	schemaName, tableName, err := d.resolveSchemaTable(ctx, db, table)
	if err != nil {
		return nil, "", err
	}
	targetRef := quoteTable(schemaName, tableName)
	statements, _, err := destination.RenderEvolution(&Dialect{}, targetRef, comparison)
	if err != nil {
		return nil, "", err
	}

	hasAlterType := false
	alterColPrefix := "ALTER TABLE " + targetRef + " ALTER COLUMN "
	for _, stmt := range statements {
		if strings.HasPrefix(stmt, alterColPrefix) {
			hasAlterType = true
			break
		}
	}

	if !hasAlterType {
		var resultInc string
		err = d.withTx(ctx, func(tx redshiftTx) error {
			lockedSchema, lockedTable, lockedTargetRef, lockErr := d.lockAndValidateCDCIncarnation(ctx, tx, table, expectedIncarnation)
			if lockErr != nil {
				return fmt.Errorf("redshift CDC target %q physical incarnation changed before schema evolution: %w", table, lockErr)
			}
			txStmts := statements
			if lockedTargetRef != targetRef {
				var renderErr error
				txStmts, _, renderErr = destination.RenderEvolution(&Dialect{}, lockedTargetRef, comparison)
				if renderErr != nil {
					return renderErr
				}
			}
			for _, stmt := range txStmts {
				if leaseErr := source.ConnectorLeaseLoss(ctx); leaseErr != nil {
					return leaseErr
				}
				if _, execErr := tx.Exec(ctx, stmt); execErr != nil {
					return fmt.Errorf("schema evolution failed (%s): %w", stmt, execErr)
				}
			}
			inc, exists, incErr := d.targetIncarnationInTx(ctx, tx, lockedSchema, lockedTable)
			if incErr != nil {
				return incErr
			}
			if !exists || inc != expectedIncarnation {
				return fmt.Errorf("redshift CDC target %q physical incarnation changed during schema evolution", table)
			}
			if leaseErr := source.ConnectorLeaseLoss(ctx); leaseErr != nil {
				return leaseErr
			}
			resultInc = inc
			return nil
		})
		if err != nil {
			return nil, "", err
		}
		return warnings, resultInc, nil
	}

	// Redshift disallows ALTER COLUMN TYPE inside explicit transactions.
	// Persist the guard mapping in _bruin_staging.cdc_evolution_journal in the same
	// transaction that renames the verified target under its private guard name so
	// interrupted changes or failed restores can always be recovered on restart.
	journalRef, err := d.ensureEvolutionJournalTable(ctx, db)
	if err != nil {
		return nil, "", err
	}
	guardName, err := newRedshiftEvolutionGuardName()
	if err != nil {
		return nil, "", err
	}
	var ownerTokenBytes [16]byte
	if _, err := rand.Read(ownerTokenBytes[:]); err != nil {
		return nil, "", fmt.Errorf("failed to generate Redshift schema evolution owner token: %w", err)
	}
	ownerToken := hex.EncodeToString(ownerTokenBytes[:])
	ttlSeconds, renewEvery := d.evolutionLeaseTiming()

	d.markEvolutionGuardInFlight(guardName)
	defer d.unmarkEvolutionGuardInFlight(guardName)

	var lockedSchema, lockedTable, guardRef string
	err = d.withTx(ctx, func(tx redshiftTx) error {
		if _, lockErr := tx.Exec(ctx, "LOCK "+journalRef); lockErr != nil {
			return fmt.Errorf("failed to lock Redshift schema evolution journal %s: %w", journalRef, lockErr)
		}
		var lockedTargetRef string
		var lockErr error
		lockedSchema, lockedTable, lockedTargetRef, lockErr = d.lockAndValidateCDCIncarnation(ctx, tx, table, expectedIncarnation)
		if lockErr != nil {
			return fmt.Errorf("redshift CDC target %q physical incarnation changed before schema evolution: %w", table, lockErr)
		}
		exists, existsErr := tableExists(ctx, tx, lockedSchema, guardName)
		if existsErr != nil {
			return existsErr
		}
		if exists {
			return fmt.Errorf("temporary Redshift schema evolution table %s.%s already exists", lockedSchema, guardName)
		}
		if leaseErr := source.ConnectorLeaseLoss(ctx); leaseErr != nil {
			return leaseErr
		}
		insertJournalSQL := fmt.Sprintf(
			`INSERT INTO %s ("target_schema", "target_table", "guard_table", "expected_incarnation", "owner_token", "expires_at", "created_at") VALUES ($1, $2, $3, $4, $5, DATEADD(second, %d, CURRENT_TIMESTAMP), CURRENT_TIMESTAMP)`,
			journalRef, ttlSeconds,
		)
		if _, execErr := tx.Exec(ctx, insertJournalSQL, lockedSchema, lockedTable, guardName, expectedIncarnation, ownerToken); execErr != nil {
			return fmt.Errorf("failed to persist Redshift schema evolution journal for %q: %w", table, execErr)
		}
		renameSQL := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", lockedTargetRef, destination.QuoteIdentifier(guardName))
		if _, execErr := tx.Exec(ctx, renameSQL); execErr != nil {
			return fmt.Errorf("failed to stage Redshift CDC target %q for schema evolution: %w", table, execErr)
		}
		guardRef = quoteTable(lockedSchema, guardName)
		return nil
	})
	if err != nil {
		return nil, "", err
	}

	stopHeartbeat := startEvolutionJournalHeartbeat(db, journalRef, lockedSchema, lockedTable, guardName, ownerToken, ttlSeconds, renewEvery)
	defer stopHeartbeat()

	guardStmts, _, applyErr := destination.RenderEvolution(&Dialect{}, guardRef, comparison)
	if applyErr == nil {
		for _, stmt := range guardStmts {
			if leaseErr := source.ConnectorLeaseLoss(ctx); leaseErr != nil {
				applyErr = leaseErr
				break
			}
			owned, renewErr := renewEvolutionJournalLease(ctx, db, journalRef, lockedSchema, lockedTable, guardName, ownerToken, ttlSeconds)
			if renewErr != nil {
				applyErr = fmt.Errorf("failed to renew Redshift schema evolution journal lease for %q: %w", table, renewErr)
				break
			}
			if !owned {
				applyErr = fmt.Errorf("redshift schema evolution journal lease for %q expired before standalone DDL", table)
				break
			}
			inc, exists, incErr := d.targetIncarnationInTx(ctx, db, lockedSchema, guardName)
			if incErr != nil {
				applyErr = incErr
				break
			}
			if !exists || inc != expectedIncarnation {
				applyErr = fmt.Errorf("redshift CDC target %q physical incarnation changed during schema evolution", table)
				break
			}
			if _, execErr := db.Exec(ctx, stmt); execErr != nil {
				applyErr = fmt.Errorf("schema evolution failed (%s): %w", stmt, execErr)
				break
			}
		}
	}

	stopHeartbeat()

	restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()

	var resultInc string
	var postRestoreErr error
	restoreErr := d.withTx(restoreCtx, func(tx redshiftTx) error {
		if _, lockErr := tx.Exec(restoreCtx, "LOCK "+journalRef); lockErr != nil {
			return fmt.Errorf("failed to lock Redshift schema evolution journal %s: %w", journalRef, lockErr)
		}
		journalPending, jErr := evolutionJournalEntryExists(restoreCtx, tx, journalRef, lockedSchema, lockedTable, guardName)
		if jErr != nil {
			return jErr
		}
		deleteJournal := func() error {
			if !journalPending {
				return nil
			}
			delSQL := fmt.Sprintf(
				`DELETE FROM %s WHERE "target_schema" = $1 AND "target_table" = $2 AND "guard_table" = $3`,
				journalRef,
			)
			if _, delErr := tx.Exec(restoreCtx, delSQL, lockedSchema, lockedTable, guardName); delErr != nil {
				return fmt.Errorf("failed to clear Redshift schema evolution journal for %q: %w", table, delErr)
			}
			return nil
		}

		inc, guardExists, incErr := d.targetIncarnationInTx(restoreCtx, tx, lockedSchema, guardName)
		if incErr != nil {
			return incErr
		}
		if !guardExists {
			if !journalPending {
				finalInc, finalExists, finalErr := d.targetIncarnationInTx(restoreCtx, tx, lockedSchema, lockedTable)
				if finalErr != nil {
					return finalErr
				}
				if finalExists && finalInc == expectedIncarnation {
					resultInc = finalInc
					return nil
				}
			}
			if err := deleteJournal(); err != nil {
				return err
			}
			postRestoreErr = fmt.Errorf("redshift CDC target %q physical incarnation changed during schema evolution", table)
			return nil
		}
		if inc != expectedIncarnation {
			if err := deleteJournal(); err != nil {
				return err
			}
			postRestoreErr = fmt.Errorf("redshift CDC target %q physical incarnation changed during schema evolution", table)
			return nil
		}

		if _, lockErr := tx.Exec(restoreCtx, "LOCK "+guardRef); lockErr != nil {
			return fmt.Errorf("failed to lock staged Redshift CDC target %s: %w", guardRef, lockErr)
		}
		inc, guardExists, incErr = d.targetIncarnationInTx(restoreCtx, tx, lockedSchema, guardName)
		if incErr != nil {
			return incErr
		}
		if !guardExists || inc != expectedIncarnation {
			if err := deleteJournal(); err != nil {
				return err
			}
			postRestoreErr = fmt.Errorf("redshift CDC target %q physical incarnation changed during schema evolution", table)
			return nil
		}

		_, targetExists, targetErr := d.targetIncarnationInTx(restoreCtx, tx, lockedSchema, lockedTable)
		if targetErr != nil {
			return targetErr
		}
		if targetExists {
			if err := deleteJournal(); err != nil {
				return err
			}
			postRestoreErr = fmt.Errorf("redshift CDC target %q was replaced during schema evolution (original preserved as %s)", table, guardRef)
			return nil
		}

		renameBackSQL := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", guardRef, destination.QuoteIdentifier(lockedTable))
		if _, execErr := tx.Exec(restoreCtx, renameBackSQL); execErr != nil {
			return fmt.Errorf("failed to restore Redshift CDC target %q after schema evolution: %w", table, execErr)
		}
		if err := deleteJournal(); err != nil {
			return err
		}

		finalInc, finalExists, finalErr := d.targetIncarnationInTx(restoreCtx, tx, lockedSchema, lockedTable)
		if finalErr != nil {
			return finalErr
		}
		if !finalExists || finalInc != expectedIncarnation {
			return fmt.Errorf("redshift CDC target %q physical incarnation changed during schema evolution", table)
		}
		resultInc = finalInc
		return nil
	})

	if restoreErr != nil {
		expireEvolutionJournalLeaseBestEffort(db, journalRef, lockedSchema, lockedTable, guardName, ownerToken)
	}
	if applyErr != nil || restoreErr != nil || postRestoreErr != nil {
		return nil, "", errors.Join(applyErr, restoreErr, postRestoreErr)
	}
	if err := source.ConnectorLeaseLoss(ctx); err != nil {
		return nil, "", err
	}
	return warnings, resultInc, nil
}

// SupportsColumnTypeChanges reports whether this destination can change a column's type.
func (d *RedshiftDestination) SupportsColumnTypeChanges() bool {
	return (&Dialect{}).SupportsAlterType()
}
