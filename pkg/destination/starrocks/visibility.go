package starrocks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

func (d *StarRocksDestination) waitForPublish(ctx context.Context, database string, txnID int64) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		state, err := d.transactionState(ctx, database, txnID)
		if err != nil {
			return fmt.Errorf("wait for StarRocks transaction %d visibility: %w", txnID, err)
		}
		switch state {
		case "VISIBLE":
			return nil
		case "ABORTED":
			return fmt.Errorf("StarRocks transaction %d was aborted", txnID)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("StarRocks transaction %d is not yet visible: %w", txnID, ctx.Err())
		case <-ticker.C:
		}
	}
}

func (d *StarRocksDestination) transactionState(ctx context.Context, database string, txnID int64) (string, error) {
	status, err := d.showStatus(ctx, fmt.Sprintf("SHOW TRANSACTION FROM %s WHERE ID = %d", quoteColumn(database), txnID))
	if err != nil {
		return "", err
	}
	for col, value := range status {
		if strings.EqualFold(col, "TransactionStatus") && value != "" {
			return value, nil
		}
	}
	return "", fmt.Errorf("transaction %d has no readable status", txnID)
}

// insertVisible runs INSERT INTO table WITH LABEL ... body and waits until the
// load is query-visible. StarRocks reports a publish timeout as a successful
// COMMITTED insert, which later reads would not yet observe.
func (d *StarRocksDestination) insertVisible(ctx context.Context, table, body string) error {
	database, name := d.tableParts(table)
	label := d.nextLabel(name)
	if _, err := d.db.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s WITH LABEL %s %s", d.tableRef(table), quoteColumn(label), body)); err != nil {
		return err
	}
	txnID, err := d.insertTransactionID(ctx, database, label)
	if err != nil {
		return fmt.Errorf("look up StarRocks insert %s transaction: %w", label, err)
	}
	return d.waitForPublish(ctx, database, txnID)
}

func (d *StarRocksDestination) insertTransactionID(ctx context.Context, database, label string) (int64, error) {
	if !d.legacyLoadMetadata.Load() {
		var txnID sql.NullInt64
		err := d.db.QueryRowContext(ctx, "SELECT get_json_int(RUNTIME_DETAILS, '$.txn_id') FROM information_schema.loads WHERE DB_NAME = ? AND LABEL = ?", database, label).Scan(&txnID)
		if err == nil {
			if !txnID.Valid || txnID.Int64 <= 0 {
				return 0, fmt.Errorf("StarRocks insert %s committed but visibility cannot be verified", label)
			}
			return txnID.Int64, nil
		}
		if !isLegacyLoadMetadataError(err) {
			return 0, err
		}
		d.legacyLoadMetadata.Store(true)
	}

	dbID, err := d.procID(ctx, "SHOW PROC '/transactions'", "DbName", "DbId", database)
	if err != nil {
		return 0, fmt.Errorf("resolve StarRocks transaction database (SHOW PROC requires OPERATE on older servers): %w", err)
	}
	// Search running before finished so publishing during the lookup cannot
	// move the transaction into a list we have already searched.
	for _, state := range []string{"running", "finished"} {
		query := fmt.Sprintf("SHOW PROC '/transactions/%d/%s'", dbID, state)
		txnID, err := d.procID(ctx, query, "Label", "TransactionId", label)
		if err == nil {
			return txnID, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("read StarRocks %s transactions: %w", state, err)
		}
	}
	return 0, fmt.Errorf("StarRocks insert %s is absent from the limited SHOW PROC transaction history; visibility cannot be verified (upgrade to StarRocks 3.3.6 or newer): %w", label, sql.ErrNoRows)
}

func isLegacyLoadMetadataError(err error) bool {
	var mysqlErr *mysql.MySQLError
	if !errors.As(err, &mysqlErr) || (mysqlErr.Number != 1054 && mysqlErr.Number != 1064) {
		return false
	}
	message := strings.ToLower(mysqlErr.Message)
	missingColumn := strings.Contains(message, "cannot be resolved") || strings.Contains(message, "unknown column")
	return missingColumn && (strings.Contains(message, "column 'db_name'") || strings.Contains(message, "column 'runtime_details'"))
}

func (d *StarRocksDestination) procID(ctx context.Context, query, nameColumn, idColumn, name string) (int64, error) {
	rows, err := d.db.QueryContext(ctx, query)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()
	columns, err := rows.Columns()
	if err != nil {
		return 0, err
	}
	nameIndex, idIndex := -1, -1
	for i, column := range columns {
		if strings.EqualFold(column, nameColumn) {
			nameIndex = i
		}
		if strings.EqualFold(column, idColumn) {
			idIndex = i
		}
	}
	if nameIndex < 0 || idIndex < 0 {
		return 0, fmt.Errorf("StarRocks SHOW PROC result lacks %s or %s", nameColumn, idColumn)
	}
	values, pointers := make([]sql.NullString, len(columns)), make([]any, len(columns))
	for i := range values {
		pointers[i] = &values[i]
	}
	for rows.Next() {
		if err := rows.Scan(pointers...); err != nil {
			return 0, err
		}
		if values[nameIndex].String != name {
			continue
		}
		id, err := strconv.ParseInt(values[idIndex].String, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse StarRocks %s: %w", idColumn, err)
		}
		if id <= 0 {
			return 0, fmt.Errorf("StarRocks %s must be positive, got %d", idColumn, id)
		}
		return id, nil
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return 0, sql.ErrNoRows
}
