package server

import (
	"context"
	"time"
)

// RunRecord represents a stored run in the database
type RunRecord struct {
	ID           string     `json:"id"`
	Status       string     `json:"status"`
	SourceURI    string     `json:"sourceUri"`
	DestURI      string     `json:"destUri"`
	SourceTable  string     `json:"sourceTable"`
	DestTable    string     `json:"destTable"`
	Strategy     string     `json:"strategy"`
	Error        string     `json:"error,omitempty"`
	StartedAt    time.Time  `json:"startedAt"`
	EndedAt      *time.Time `json:"endedAt,omitempty"`
	RowsIngested int64      `json:"rowsIngested"`
}

// LogRecord represents a log entry in the database
type LogRecord struct {
	ID        int64     `json:"id"`
	RunID     string    `json:"runId"`
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
}

// RunRepository defines the interface for run storage
type RunRepository interface {
	// CreateRun creates a new run record
	CreateRun(ctx context.Context, run *RunRecord) error

	// UpdateRun updates an existing run record
	UpdateRun(ctx context.Context, run *RunRecord) error

	// GetRun retrieves a run by ID
	GetRun(ctx context.Context, id string) (*RunRecord, error)

	// ListRuns retrieves all runs, ordered by startedAt desc
	ListRuns(ctx context.Context, limit, offset int) ([]*RunRecord, error)

	// ListRunsPaginated retrieves runs with total count for pagination
	ListRunsPaginated(ctx context.Context, limit, offset int) ([]*RunRecord, int, error)

	// DeleteRun deletes a run and its logs
	DeleteRun(ctx context.Context, id string) error

	// AddLog adds a log entry for a run
	AddLog(ctx context.Context, log *LogRecord) error

	// GetLogs retrieves logs for a run
	GetLogs(ctx context.Context, runID string) ([]*LogRecord, error)

	// Close closes the repository connection
	Close() error
}
