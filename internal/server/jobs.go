package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/pipeline"
	"github.com/google/uuid"
)

type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
}

type Job struct {
	ID        string     `json:"id"`
	Status    string     `json:"status"`
	StartedAt time.Time  `json:"startedAt"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`
	Error     string     `json:"error,omitempty"`
}

type JobManager struct {
	jobs        sync.Map
	logSubs     sync.Map
	logBuffers  sync.Map // jobID -> []LogEntry
	jobFinished sync.Map // jobID -> bool
	logFiles    sync.Map // jobID -> *os.File
	logsDir     string
	repo        RunRepository
	configs     sync.Map // jobID -> *config.IngestConfig (for storing run metadata)
}

func NewJobManager(logsDir string, repo RunRepository) *JobManager {
	if logsDir == "" {
		logsDir = "logs"
	}
	_ = os.MkdirAll(logsDir, 0o755)
	return &JobManager{logsDir: logsDir, repo: repo}
}

func (jm *JobManager) StartJob(ctx context.Context, cfg *config.IngestConfig) (string, error) {
	jobID := uuid.NewString()
	now := time.Now()

	job := &Job{
		ID:        jobID,
		Status:    "running",
		StartedAt: now,
	}
	jm.jobs.Store(jobID, job)
	jm.configs.Store(jobID, cfg)

	// Store run in repository
	if jm.repo != nil {
		run := &RunRecord{
			ID:          jobID,
			Status:      "running",
			SourceURI:   maskURI(cfg.SourceURI),
			DestURI:     maskURI(cfg.DestURI),
			SourceTable: cfg.SourceTable,
			DestTable:   cfg.DestTable,
			Strategy:    string(cfg.IncrementalStrategy),
			StartedAt:   now,
		}
		_ = jm.repo.CreateRun(ctx, run)
	}

	// Create log file for this job
	logPath := filepath.Join(jm.logsDir, jobID+".jsonl")
	logFile, err := os.Create(logPath)
	if err == nil {
		jm.logFiles.Store(jobID, logFile)
	}

	// Use a background context for the job since the HTTP request context
	// will be canceled when the response is sent
	go jm.runJob(context.Background(), jobID, cfg)

	return jobID, nil
}

func (jm *JobManager) runJob(ctx context.Context, jobID string, cfg *config.IngestConfig) {
	defer func() {
		if r := recover(); r != nil {
			jm.updateJob(jobID, "failed", fmt.Sprintf("panic: %v", r))
			jm.broadcastLog(jobID, LogEntry{
				Timestamp: time.Now(),
				Level:     "error",
				Message:   fmt.Sprintf("Job panicked: %v", r),
			})
		}
		jm.closeLogSubscribers(jobID)
	}()

	jm.broadcastLog(jobID, LogEntry{
		Timestamp: time.Now(),
		Level:     "info",
		Message:   fmt.Sprintf("Starting ingestion from %s to %s", maskURI(cfg.SourceURI), maskURI(cfg.DestURI)),
	})
	jm.broadcastLog(jobID, LogEntry{
		Timestamp: time.Now(),
		Level:     "info",
		Message:   fmt.Sprintf("Source table: %s, Destination table: %s", cfg.SourceTable, cfg.DestTable),
	})
	jm.broadcastLog(jobID, LogEntry{
		Timestamp: time.Now(),
		Level:     "info",
		Message:   fmt.Sprintf("Strategy: %s", cfg.IncrementalStrategy),
	})

	cfg.Progress = config.ProgressLog

	logWriter := &jobLogWriter{
		jobID:   jobID,
		manager: jm,
	}

	p := pipeline.New(cfg)
	p.SetLogWriter(logWriter)

	if err := p.Run(ctx); err != nil {
		jm.updateJob(jobID, "failed", err.Error())
		jm.broadcastLog(jobID, LogEntry{
			Timestamp: time.Now(),
			Level:     "error",
			Message:   fmt.Sprintf("Job failed: %v", err),
		})
		return
	}

	jm.updateJob(jobID, "completed", "")
	jm.broadcastLog(jobID, LogEntry{
		Timestamp: time.Now(),
		Level:     "info",
		Message:   "Ingestion completed successfully!",
	})
}

func (jm *JobManager) updateJob(jobID string, status string, errMsg string) {
	var endedAt *time.Time
	if val, ok := jm.jobs.Load(jobID); ok {
		job := val.(*Job)
		job.Status = status
		if status == "completed" || status == "failed" {
			now := time.Now()
			job.EndedAt = &now
			endedAt = &now
		}
		if errMsg != "" {
			job.Error = errMsg
		}
	}

	// Update repository
	if jm.repo != nil {
		run := &RunRecord{
			ID:      jobID,
			Status:  status,
			Error:   errMsg,
			EndedAt: endedAt,
		}
		_ = jm.repo.UpdateRun(context.Background(), run)
	}
}

func (jm *JobManager) GetJob(id string) (*Job, bool) {
	if val, ok := jm.jobs.Load(id); ok {
		return val.(*Job), true
	}
	return nil, false
}

func (jm *JobManager) SubscribeLogs(jobID string) chan LogEntry {
	ch := make(chan LogEntry, 100)

	// Send buffered logs first
	if val, ok := jm.logBuffers.Load(jobID); ok {
		buffer := val.([]LogEntry)
		for _, entry := range buffer {
			select {
			case ch <- entry:
			default:
			}
		}
	}

	// If job already finished, close channel immediately after sending buffered logs
	if _, finished := jm.jobFinished.Load(jobID); finished {
		close(ch)
		return ch
	}

	var subs []chan LogEntry
	if val, ok := jm.logSubs.Load(jobID); ok {
		subs = val.([]chan LogEntry)
	}
	subs = append(subs, ch)
	jm.logSubs.Store(jobID, subs)

	return ch
}

func (jm *JobManager) UnsubscribeLogs(jobID string, ch chan LogEntry) {
	if val, ok := jm.logSubs.Load(jobID); ok {
		subs := val.([]chan LogEntry)
		newSubs := make([]chan LogEntry, 0, len(subs))
		for _, s := range subs {
			if s != ch {
				newSubs = append(newSubs, s)
			}
		}
		jm.logSubs.Store(jobID, newSubs)
	}
}

func (jm *JobManager) broadcastLog(jobID string, entry LogEntry) {
	// Always buffer the log first
	var buffer []LogEntry
	if val, ok := jm.logBuffers.Load(jobID); ok {
		buffer = val.([]LogEntry)
	}
	buffer = append(buffer, entry)
	jm.logBuffers.Store(jobID, buffer)

	// Write to log file
	if val, ok := jm.logFiles.Load(jobID); ok {
		if f, ok := val.(*os.File); ok {
			line, _ := json.Marshal(entry)
			_, _ = f.Write(append(line, '\n'))
		}
	}

	// Store in repository
	if jm.repo != nil {
		logRecord := &LogRecord{
			RunID:     jobID,
			Timestamp: entry.Timestamp,
			Level:     entry.Level,
			Message:   entry.Message,
		}
		_ = jm.repo.AddLog(context.Background(), logRecord)
	}

	// Then broadcast to any existing subscribers
	if val, ok := jm.logSubs.Load(jobID); ok {
		subs := val.([]chan LogEntry)
		for _, ch := range subs {
			select {
			case ch <- entry:
			default:
			}
		}
	}
}

func (jm *JobManager) closeLogSubscribers(jobID string) {
	// Mark job as finished first
	jm.jobFinished.Store(jobID, true)

	if val, ok := jm.logSubs.Load(jobID); ok {
		subs := val.([]chan LogEntry)
		for _, ch := range subs {
			close(ch)
		}
		jm.logSubs.Delete(jobID)
	}

	// Close the log file
	if val, ok := jm.logFiles.LoadAndDelete(jobID); ok {
		if f, ok := val.(*os.File); ok {
			_ = f.Close()
		}
	}

	// Clean up in-memory state after a delay to allow late subscribers to read buffered logs.
	// The persistent data is still available via the repository.
	go func() {
		time.Sleep(5 * time.Minute)
		jm.jobs.Delete(jobID)
		jm.configs.Delete(jobID)
		jm.logBuffers.Delete(jobID)
		jm.jobFinished.Delete(jobID)
	}()
}

func (jm *JobManager) ListRuns(ctx context.Context, limit, offset int) ([]*RunRecord, error) {
	if jm.repo == nil {
		return nil, nil
	}
	return jm.repo.ListRuns(ctx, limit, offset)
}

func (jm *JobManager) ListRunsPaginated(ctx context.Context, limit, offset int) ([]*RunRecord, int, error) {
	if jm.repo == nil {
		return nil, 0, nil
	}
	return jm.repo.ListRunsPaginated(ctx, limit, offset)
}

func (jm *JobManager) GetRunLogs(ctx context.Context, runID string) ([]*LogRecord, error) {
	if jm.repo == nil {
		return nil, nil
	}
	return jm.repo.GetLogs(ctx, runID)
}

func (jm *JobManager) GetRun(ctx context.Context, id string) (*RunRecord, error) {
	if jm.repo == nil {
		return nil, nil
	}
	return jm.repo.GetRun(ctx, id)
}

type jobLogWriter struct {
	jobID   string
	manager *JobManager
}

func (w *jobLogWriter) Write(p []byte) (n int, err error) {
	msg := string(p)
	if msg != "" && msg != "\n" {
		w.manager.broadcastLog(w.jobID, LogEntry{
			Timestamp: time.Now(),
			Level:     "info",
			Message:   msg,
		})
	}
	return len(p), nil
}

var _ io.Writer = (*jobLogWriter)(nil)

func maskURI(uri string) string {
	return uri
}
