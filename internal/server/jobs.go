package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	internaluri "github.com/bruin-data/ingestr/internal/uri"
	"github.com/google/uuid"
)

type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
}

const (
	maxBufferedLogEntries       = 10_000
	maxPendingPersistLogEntries = 10_000
	logChannelHeadroom          = 100
	logStreamLagWarning         = "Log stream fell behind; some live log lines were skipped. Reconnect to replay recent buffered logs."
	logPersistenceLagWarning    = "Log persistence fell behind; %d log line(s) were skipped in file/database logs."
)

type Job struct {
	ID        string     `json:"id"`
	Status    string     `json:"status"`
	StartedAt time.Time  `json:"startedAt"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`
	Error     string     `json:"error,omitempty"`
}

type JobSpec struct {
	Args        []string
	SourceURI   string
	DestURI     string
	SourceTable string
	DestTable   string
	Strategy    string
}

type logSink struct {
	mu        sync.Mutex
	cond      *sync.Cond
	entries   []LogEntry
	head      int
	dropped   int
	droppedAt time.Time
	closed    bool
	done      chan struct{}
	file      *os.File
}

type JobManager struct {
	jobs        sync.Map
	logSubs     sync.Map
	logBuffers  sync.Map // jobID -> []LogEntry
	jobFinished sync.Map // jobID -> bool
	logSinks    sync.Map // jobID -> *logSink
	logMu       sync.Mutex
	cancels     sync.Map // jobID -> context.CancelFunc
	logsDir     string
	repo        RunRepository
	binaryPath  string
}

func NewJobManager(logsDir string, repo RunRepository, binaryPath string) *JobManager {
	if logsDir == "" {
		logsDir = "logs"
	}
	_ = os.MkdirAll(logsDir, 0o755)
	return &JobManager{logsDir: logsDir, repo: repo, binaryPath: binaryPath}
}

func (jm *JobManager) StartJob(ctx context.Context, spec JobSpec) (string, error) {
	if len(spec.Args) == 0 {
		return "", fmt.Errorf("job args are required")
	}

	jobID := uuid.NewString()
	now := time.Now()

	job := &Job{
		ID:        jobID,
		Status:    "running",
		StartedAt: now,
	}
	jm.jobs.Store(jobID, job)

	if jm.repo != nil {
		run := &RunRecord{
			ID:          jobID,
			Status:      "running",
			SourceURI:   maskURI(spec.SourceURI),
			DestURI:     maskURI(spec.DestURI),
			SourceTable: spec.SourceTable,
			DestTable:   spec.DestTable,
			Strategy:    spec.Strategy,
			StartedAt:   now,
		}
		_ = jm.repo.CreateRun(ctx, run)
	}

	logPath := filepath.Join(jm.logsDir, jobID+".jsonl")
	logFile, err := os.Create(logPath)
	if err == nil || jm.repo != nil {
		jm.startLogSink(jobID, logFile)
	}
	if err != nil {
		jm.broadcastLog(jobID, LogEntry{
			Timestamp: time.Now(),
			Level:     "warning",
			Message:   fmt.Sprintf("Failed to create log file: %v", err),
		})
	}

	jobCtx, cancel := context.WithCancel(context.Background())
	jm.cancels.Store(jobID, cancel)
	go func() {
		defer cancel()
		jm.runJob(jobCtx, jobID, spec)
	}()

	return jobID, nil
}

func (jm *JobManager) runJob(ctx context.Context, jobID string, spec JobSpec) {
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
		Message:   fmt.Sprintf("Starting ingestion from %s to %s", maskURI(spec.SourceURI), maskURI(spec.DestURI)),
	})
	jm.broadcastLog(jobID, LogEntry{
		Timestamp: time.Now(),
		Level:     "info",
		Message:   fmt.Sprintf("Source table: %s, Destination table: %s", spec.SourceTable, spec.DestTable),
	})

	binary, err := jm.ingestrBinary()
	if err != nil {
		jm.updateJob(jobID, "failed", err.Error())
		jm.broadcastLog(jobID, LogEntry{
			Timestamp: time.Now(),
			Level:     "error",
			Message:   fmt.Sprintf("Job failed: %v", err),
		})
		return
	}

	commandArgs := append([]string{"ingest"}, omitURIArgs(spec.Args)...)
	message := fmt.Sprintf("Executing: %s %s", binary, strings.Join(maskCLIArgs(commandArgs), " "))
	if spec.SourceURI != "" || spec.DestURI != "" {
		message += " (source/destination URI supplied via environment)"
	}
	jm.broadcastLog(jobID, LogEntry{
		Timestamp: time.Now(),
		Level:     "info",
		Message:   message,
	})

	cmd := exec.CommandContext(ctx, binary, commandArgs...)
	cmd.Cancel = gracefulCancel(cmd)
	cmd.WaitDelay = 10 * time.Second
	cmd.Env = commandEnv(spec)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		jm.updateJob(jobID, "failed", err.Error())
		jm.broadcastLog(jobID, LogEntry{
			Timestamp: time.Now(),
			Level:     "error",
			Message:   fmt.Sprintf("Job failed: %v", err),
		})
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		jm.updateJob(jobID, "failed", err.Error())
		jm.broadcastLog(jobID, LogEntry{
			Timestamp: time.Now(),
			Level:     "error",
			Message:   fmt.Sprintf("Job failed: %v", err),
		})
		return
	}

	if err := cmd.Start(); err != nil {
		jm.updateJob(jobID, "failed", err.Error())
		jm.broadcastLog(jobID, LogEntry{
			Timestamp: time.Now(),
			Level:     "error",
			Message:   fmt.Sprintf("Job failed: %v", err),
		})
		return
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go jm.streamCommandOutput(ctx, jobID, stdout, "info", spec, &wg)
	go jm.streamCommandOutput(ctx, jobID, stderr, "info", spec, &wg)

	wg.Wait()
	err = cmd.Wait()
	if errors.Is(ctx.Err(), context.Canceled) {
		jm.updateJob(jobID, "canceled", "")
		jm.broadcastLog(jobID, LogEntry{
			Timestamp: time.Now(),
			Level:     "warning",
			Message:   "Ingestion canceled.",
		})
		return
	}
	if err != nil {
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

func (jm *JobManager) ingestrBinary() (string, error) {
	if jm.binaryPath != "" {
		return jm.binaryPath, nil
	}
	binary, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to resolve current executable: %w", err)
	}
	return binary, nil
}

func (jm *JobManager) startLogSink(jobID string, file *os.File) {
	sink := newLogSink(file)
	jm.logSinks.Store(jobID, sink)
	go jm.persistLogs(jobID, sink)
}

func newLogSink(file *os.File) *logSink {
	sink := &logSink{
		done: make(chan struct{}),
		file: file,
	}
	sink.cond = sync.NewCond(&sink.mu)
	return sink
}

func (jm *JobManager) persistLogs(jobID string, sink *logSink) {
	defer close(sink.done)
	defer func() {
		if sink.file != nil {
			_ = sink.file.Close()
		}
	}()

	for {
		entry, ok := sink.next()
		if !ok {
			return
		}
		if sink.file != nil {
			line, _ := json.Marshal(entry)
			_, _ = sink.file.Write(append(line, '\n'))
		}

		if jm.repo != nil {
			logRecord := &LogRecord{
				RunID:     jobID,
				Timestamp: entry.Timestamp,
				Level:     entry.Level,
				Message:   entry.Message,
			}
			_ = jm.repo.AddLog(context.Background(), logRecord)
		}
	}
}

func (s *logSink) enqueue(entry LogEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}
	if s.pending() >= maxPendingPersistLogEntries {
		s.dropOldest()
		s.dropped++
		s.droppedAt = entry.Timestamp
	}
	s.entries = append(s.entries, entry)
	s.cond.Signal()
}

func (s *logSink) next() (LogEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for s.pending() == 0 && s.dropped == 0 && !s.closed {
		s.cond.Wait()
	}
	if s.dropped > 0 {
		entry := LogEntry{
			Timestamp: s.droppedAt,
			Level:     "warning",
			Message:   fmt.Sprintf(logPersistenceLagWarning, s.dropped),
		}
		if entry.Timestamp.IsZero() {
			entry.Timestamp = time.Now()
		}
		s.dropped = 0
		s.droppedAt = time.Time{}
		return entry, true
	}
	if s.pending() == 0 {
		return LogEntry{}, false
	}

	entry := s.entries[s.head]
	s.entries[s.head] = LogEntry{}
	s.head++
	s.compact()
	return entry, true
}

func (s *logSink) pending() int {
	return len(s.entries) - s.head
}

func (s *logSink) dropOldest() {
	if s.pending() == 0 {
		return
	}
	s.entries[s.head] = LogEntry{}
	s.head++
	s.compact()
}

func (s *logSink) compact() {
	if s.head > 1024 && s.head*2 >= len(s.entries) {
		copy(s.entries, s.entries[s.head:])
		s.entries = s.entries[:len(s.entries)-s.head]
		s.head = 0
	}
}

func (s *logSink) close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.closed = true
	s.cond.Broadcast()
}

func (jm *JobManager) streamCommandOutput(ctx context.Context, jobID string, reader io.Reader, level string, spec JobSpec, wg *sync.WaitGroup) {
	defer wg.Done()

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		message := redactCommandOutput(strings.TrimRight(scanner.Text(), "\r"), spec)
		if strings.TrimSpace(message) == "" {
			continue
		}
		jm.broadcastLog(jobID, LogEntry{
			Timestamp: time.Now(),
			Level:     level,
			Message:   message,
		})
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		jm.broadcastLog(jobID, LogEntry{
			Timestamp: time.Now(),
			Level:     "error",
			Message:   fmt.Sprintf("failed to read command output: %v", err),
		})
	}
}

func redactCommandOutput(message string, spec JobSpec) string {
	for _, rawURI := range []string{spec.SourceURI, spec.DestURI} {
		if rawURI == "" {
			continue
		}
		message = strings.ReplaceAll(message, rawURI, maskURI(rawURI))
	}
	return message
}

func gracefulCancel(cmd *exec.Cmd) func() error {
	return func() error {
		if cmd.Process == nil {
			return nil
		}
		if runtime.GOOS == "windows" {
			return cmd.Process.Kill()
		}
		return cmd.Process.Signal(os.Interrupt)
	}
}

func commandEnv(spec JobSpec) []string {
	overrides := []struct {
		key   string
		value string
	}{
		{key: "DISABLE_TELEMETRY", value: "true"},
		{key: "INGESTR_DISABLE_TELEMETRY", value: "true"},
		{key: "SOURCE_URI", value: spec.SourceURI},
		{key: "INGESTR_SOURCE_URI", value: spec.SourceURI},
		{key: "DESTINATION_URI", value: spec.DestURI},
		{key: "INGESTR_DESTINATION_URI", value: spec.DestURI},
		{key: "NO_COLOR", value: "1"},
	}

	overrideIndex := make(map[string]int, len(overrides))
	for i, override := range overrides {
		overrideIndex[override.key] = i
	}

	seen := make([]bool, len(overrides))
	env := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if index, ok := overrideIndex[key]; ok {
			if !seen[index] {
				env = append(env, key+"="+overrides[index].value)
				seen[index] = true
			}
			continue
		}
		env = append(env, entry)
	}
	for i, override := range overrides {
		if !seen[i] {
			env = append(env, override.key+"="+override.value)
		}
	}
	return env
}

func (jm *JobManager) updateJob(jobID string, status string, errMsg string) {
	var endedAt *time.Time
	if val, ok := jm.jobs.Load(jobID); ok {
		current := val.(*Job)
		job := *current
		job.Status = status
		if status == "completed" || status == "failed" || status == "canceled" {
			now := time.Now()
			job.EndedAt = &now
			endedAt = &now
		}
		if errMsg != "" {
			job.Error = errMsg
		}
		jm.jobs.Store(jobID, &job)
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

func (jm *JobManager) CancelJob(id string) bool {
	val, ok := jm.cancels.Load(id)
	if !ok {
		return false
	}
	cancel := val.(context.CancelFunc)
	cancel()
	return true
}

func (jm *JobManager) GetJob(id string) (*Job, bool) {
	if val, ok := jm.jobs.Load(id); ok {
		job := *val.(*Job)
		return &job, true
	}
	return nil, false
}

func (jm *JobManager) SubscribeLogs(jobID string) chan LogEntry {
	jm.logMu.Lock()
	defer jm.logMu.Unlock()

	var buffer []LogEntry
	if val, ok := jm.logBuffers.Load(jobID); ok {
		buffer = val.([]LogEntry)
	}

	_, jobKnown := jm.jobs.Load(jobID)
	if _, finished := jm.jobFinished.Load(jobID); finished {
		ch := make(chan LogEntry, len(buffer))
		for _, entry := range buffer {
			ch <- entry
		}
		close(ch)
		return ch
	}
	if !jobKnown && len(buffer) == 0 {
		ch := make(chan LogEntry)
		close(ch)
		return ch
	}

	ch := make(chan LogEntry, len(buffer)+logChannelHeadroom)
	for _, entry := range buffer {
		ch <- entry
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
	jm.logMu.Lock()
	defer jm.logMu.Unlock()

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
	jm.logMu.Lock()

	var buffer []LogEntry
	if val, ok := jm.logBuffers.Load(jobID); ok {
		buffer = val.([]LogEntry)
	}
	buffer = append(buffer, entry)
	if len(buffer) > maxBufferedLogEntries {
		buffer = buffer[len(buffer)-maxBufferedLogEntries:]
	}
	jm.logBuffers.Store(jobID, buffer)

	if val, ok := jm.logSubs.Load(jobID); ok {
		subs := val.([]chan LogEntry)
		for _, ch := range subs {
			select {
			case ch <- entry:
			default:
				jm.sendLagWarning(ch, entry.Timestamp)
			}
		}
	}

	if val, ok := jm.logSinks.Load(jobID); ok {
		val.(*logSink).enqueue(entry)
	}

	jm.logMu.Unlock()
}

func (jm *JobManager) sendLagWarning(ch chan LogEntry, timestamp time.Time) {
	select {
	case <-ch:
	default:
	}

	select {
	case ch <- LogEntry{
		Timestamp: timestamp,
		Level:     "warning",
		Message:   logStreamLagWarning,
	}:
	default:
	}
}

func (jm *JobManager) closeLogSubscribers(jobID string) {
	var sink *logSink

	jm.logMu.Lock()
	jm.jobFinished.Store(jobID, true)

	if val, ok := jm.logSubs.Load(jobID); ok {
		subs := val.([]chan LogEntry)
		for _, ch := range subs {
			close(ch)
		}
		jm.logSubs.Delete(jobID)
	}

	if val, ok := jm.logSinks.LoadAndDelete(jobID); ok {
		sink = val.(*logSink)
		sink.close()
	}
	jm.logMu.Unlock()

	if sink != nil {
		<-sink.done
	}

	jm.cancels.Delete(jobID)

	// Clean up in-memory state after a delay to allow late subscribers to read buffered logs.
	// The persistent data is still available via the repository.
	go func() {
		time.Sleep(5 * time.Minute)
		jm.jobs.Delete(jobID)
		jm.logMu.Lock()
		defer jm.logMu.Unlock()
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

func maskURI(uri string) string {
	return internaluri.MaskURI(uri)
}

func omitURIArgs(args []string) []string {
	filtered := make([]string, 0, len(args))
	for _, arg := range args {
		if strings.HasPrefix(arg, "--source-uri=") || strings.HasPrefix(arg, "--dest-uri=") {
			continue
		}
		filtered = append(filtered, arg)
	}
	return filtered
}

func maskCLIArgs(args []string) []string {
	masked := make([]string, 0, len(args))
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "--source-uri="):
			masked = append(masked, "--source-uri="+maskURI(strings.TrimPrefix(arg, "--source-uri=")))
		case strings.HasPrefix(arg, "--dest-uri="):
			masked = append(masked, "--dest-uri="+maskURI(strings.TrimPrefix(arg, "--dest-uri=")))
		default:
			masked = append(masked, arg)
		}
	}
	return masked
}
