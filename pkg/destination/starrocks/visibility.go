package starrocks

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
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
	var txnID sql.NullInt64
	err := d.db.QueryRowContext(ctx, "SELECT get_json_int(RUNTIME_DETAILS, '$.txn_id') FROM information_schema.loads WHERE DB_NAME = ? AND LABEL = ?", database, label).Scan(&txnID)
	if err != nil {
		return fmt.Errorf("look up StarRocks insert %s transaction: %w", label, err)
	}
	if !txnID.Valid || txnID.Int64 <= 0 {
		return fmt.Errorf("StarRocks insert %s committed but visibility cannot be verified", label)
	}
	return d.waitForPublish(ctx, database, txnID.Int64)
}
