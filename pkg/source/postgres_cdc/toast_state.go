package postgres_cdc

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/gob"
	"errors"
	"fmt"
	"os"

	"github.com/jackc/pglogrepl"
	_ "modernc.org/sqlite"
)

type toastStateRow struct {
	table, key string
	values     []interface{}
	lsn        pglogrepl.LSN
	bytes      int64
}

// toastState retains values until their destination merge is durable. Evicting
// on Arrow batch boundaries would lose values when staging deduplicates batches.
// The spill file is disposable: a restart reconstructs it by replaying WAL.
type toastState struct {
	memory       map[string]toastStateRow
	bytes, limit int64
	pruned       pglogrepl.LSN
	db           *sql.DB
	tx           *sql.Tx
	path         string
}

func newToastState() *toastState {
	return &toastState{memory: make(map[string]toastStateRow), limit: defaultTransactionMemoryBytes}
}

func (s *toastState) get(ctx context.Context, table, key string) ([]interface{}, error) {
	if s.tx == nil {
		return s.memory[encodeKeyParts([]string{table, key})].values, nil
	}
	var data []byte
	err := s.tx.QueryRowContext(ctx, `SELECT value FROM toast_state WHERE table_name = ? AND row_key = ?`, table, key).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var values []interface{}
	err = gob.NewDecoder(bytes.NewReader(data)).Decode(&values)
	return values, err
}

func (s *toastState) put(ctx context.Context, table, key string, values []interface{}, lsn pglogrepl.LSN) error {
	if s.tx == nil {
		mapKey := encodeKeyParts([]string{table, key})
		size := int64(len(mapKey)+len(table)+len(key)+128) + estimateChangeBytes(Change{Values: values})
		s.bytes += size - s.memory[mapKey].bytes
		s.memory[mapKey] = toastStateRow{table: table, key: key, values: values, lsn: lsn, bytes: size}
		if s.bytes <= s.limit {
			return nil
		}
		return s.spill(ctx)
	}
	var data bytes.Buffer
	if err := gob.NewEncoder(&data).Encode(values); err != nil {
		return err
	}
	_, err := s.tx.ExecContext(ctx, `INSERT INTO toast_state VALUES (?, ?, ?, ?) ON CONFLICT(table_name, row_key) DO UPDATE SET value = excluded.value, lsn = excluded.lsn`, table, key, data.Bytes(), FormatLSN(lsn))
	return err
}

func (s *toastState) delete(ctx context.Context, table, key string) error {
	if s.tx == nil {
		mapKey := encodeKeyParts([]string{table, key})
		s.bytes -= s.memory[mapKey].bytes
		delete(s.memory, mapKey)
		return nil
	}
	_, err := s.tx.ExecContext(ctx, `DELETE FROM toast_state WHERE table_name = ? AND row_key = ?`, table, key)
	return err
}

func (s *toastState) truncate(ctx context.Context, table string) error {
	if s.tx == nil {
		for key, row := range s.memory {
			if row.table == table {
				s.bytes -= row.bytes
				delete(s.memory, key)
			}
		}
		return nil
	}
	_, err := s.tx.ExecContext(ctx, `DELETE FROM toast_state WHERE table_name = ?`, table)
	return err
}

func (s *toastState) prune(ctx context.Context, durable pglogrepl.LSN) error {
	if durable <= s.pruned {
		return nil
	}
	if s.tx == nil {
		for key, row := range s.memory {
			if row.lsn <= durable {
				s.bytes -= row.bytes
				delete(s.memory, key)
			}
		}
	} else if _, err := s.tx.ExecContext(ctx, `DELETE FROM toast_state WHERE lsn <= ?`, FormatLSN(durable)); err != nil {
		return err
	}
	s.pruned = durable
	return nil
}

func (s *toastState) spill(ctx context.Context) error {
	file, err := os.CreateTemp("", "ingestr-postgres-toast-*.sqlite")
	if err != nil {
		return err
	}
	s.path = file.Name()
	if err := file.Close(); err != nil {
		return err
	}
	s.db, err = sql.Open("sqlite", s.path)
	if err != nil {
		return err
	}
	s.db.SetMaxOpenConns(1)
	// WAL is the durable copy. Keep SQLite's cache bounded and avoid a second
	// journal or fsync for this private scratch database.
	for _, query := range []string{
		`PRAGMA journal_mode = OFF`, `PRAGMA synchronous = OFF`,
		`PRAGMA cache_size = -8192`, `PRAGMA mmap_size = 0`,
		`CREATE TABLE toast_state (table_name TEXT, row_key TEXT, value BLOB, lsn TEXT, PRIMARY KEY(table_name, row_key)) WITHOUT ROWID`,
		`CREATE INDEX toast_state_lsn ON toast_state(lsn)`,
	} {
		if _, err := s.db.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	s.tx, err = s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, row := range s.memory {
		if err := s.put(ctx, row.table, row.key, row.values, row.lsn); err != nil {
			return err
		}
	}
	s.memory = nil
	s.bytes = 0
	return nil
}

func (s *toastState) close() error {
	var err error
	if s.tx != nil {
		rollbackErr := s.tx.Rollback()
		if !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = rollbackErr
		}
	}
	if s.db != nil {
		err = errors.Join(err, s.db.Close())
	}
	if s.path != "" {
		err = errors.Join(err, os.Remove(s.path))
		s.path = ""
	}
	if err != nil {
		return fmt.Errorf("failed to close PostgreSQL TOAST state: %w", err)
	}
	return nil
}
