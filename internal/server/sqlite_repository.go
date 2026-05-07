package server

import (
	"context"
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

// SQLiteRepository implements RunRepository using SQLite
type SQLiteRepository struct {
	db *sql.DB
}

// NewSQLiteRepository creates a new SQLite repository
func NewSQLiteRepository(dbPath string) (*SQLiteRepository, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	repo := &SQLiteRepository{db: db}
	if err := repo.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}

	return repo, nil
}

func (r *SQLiteRepository) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS runs (
		id TEXT PRIMARY KEY,
		status TEXT NOT NULL,
		source_uri TEXT NOT NULL,
		dest_uri TEXT NOT NULL,
		source_table TEXT NOT NULL,
		dest_table TEXT NOT NULL,
		strategy TEXT NOT NULL,
		error TEXT,
		started_at DATETIME NOT NULL,
		ended_at DATETIME,
		rows_ingested INTEGER DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		run_id TEXT NOT NULL,
		timestamp DATETIME NOT NULL,
		level TEXT NOT NULL,
		message TEXT NOT NULL,
		FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_logs_run_id ON logs(run_id);
	CREATE INDEX IF NOT EXISTS idx_runs_started_at ON runs(started_at DESC);
	`
	_, err := r.db.Exec(schema)
	return err
}

func (r *SQLiteRepository) CreateRun(ctx context.Context, run *RunRecord) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO runs (id, status, source_uri, dest_uri, source_table, dest_table, strategy, error, started_at, ended_at, rows_ingested)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, run.ID, run.Status, run.SourceURI, run.DestURI, run.SourceTable, run.DestTable, run.Strategy, run.Error, run.StartedAt, run.EndedAt, run.RowsIngested)
	return err
}

func (r *SQLiteRepository) UpdateRun(ctx context.Context, run *RunRecord) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE runs SET status = ?, error = ?, ended_at = ?, rows_ingested = ?
		WHERE id = ?
	`, run.Status, run.Error, run.EndedAt, run.RowsIngested, run.ID)
	return err
}

func (r *SQLiteRepository) GetRun(ctx context.Context, id string) (*RunRecord, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, status, source_uri, dest_uri, source_table, dest_table, strategy, error, started_at, ended_at, rows_ingested
		FROM runs WHERE id = ?
	`, id)

	run := &RunRecord{}
	var endedAt sql.NullTime
	var errStr sql.NullString
	err := row.Scan(&run.ID, &run.Status, &run.SourceURI, &run.DestURI, &run.SourceTable, &run.DestTable, &run.Strategy, &errStr, &run.StartedAt, &endedAt, &run.RowsIngested)
	if err != nil {
		return nil, err
	}
	if endedAt.Valid {
		run.EndedAt = &endedAt.Time
	}
	if errStr.Valid {
		run.Error = errStr.String
	}
	return run, nil
}

func (r *SQLiteRepository) ListRuns(ctx context.Context, limit, offset int) ([]*RunRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, status, source_uri, dest_uri, source_table, dest_table, strategy, error, started_at, ended_at, rows_ingested
		FROM runs ORDER BY started_at DESC LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var runs []*RunRecord
	for rows.Next() {
		run := &RunRecord{}
		var endedAt sql.NullTime
		var errStr sql.NullString
		err := rows.Scan(&run.ID, &run.Status, &run.SourceURI, &run.DestURI, &run.SourceTable, &run.DestTable, &run.Strategy, &errStr, &run.StartedAt, &endedAt, &run.RowsIngested)
		if err != nil {
			return nil, err
		}
		if endedAt.Valid {
			run.EndedAt = &endedAt.Time
		}
		if errStr.Valid {
			run.Error = errStr.String
		}
		runs = append(runs, run)
	}
	return runs, nil
}

func (r *SQLiteRepository) ListRunsPaginated(ctx context.Context, limit, offset int) ([]*RunRecord, int, error) {
	// Get total count
	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM runs").Scan(&total); err != nil {
		return nil, 0, err
	}

	// Get paginated runs
	runs, err := r.ListRuns(ctx, limit, offset)
	if err != nil {
		return nil, 0, err
	}

	return runs, total, nil
}

func (r *SQLiteRepository) DeleteRun(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM runs WHERE id = ?", id)
	return err
}

func (r *SQLiteRepository) AddLog(ctx context.Context, log *LogRecord) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO logs (run_id, timestamp, level, message)
		VALUES (?, ?, ?, ?)
	`, log.RunID, log.Timestamp, log.Level, log.Message)
	return err
}

func (r *SQLiteRepository) GetLogs(ctx context.Context, runID string) ([]*LogRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, run_id, timestamp, level, message
		FROM logs WHERE run_id = ? ORDER BY timestamp ASC
	`, runID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var logs []*LogRecord
	for rows.Next() {
		log := &LogRecord{}
		err := rows.Scan(&log.ID, &log.RunID, &log.Timestamp, &log.Level, &log.Message)
		if err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}
	return logs, nil
}

func (r *SQLiteRepository) Close() error {
	return r.db.Close()
}
