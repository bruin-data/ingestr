package server

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestJobManagerRunsIngestrCLI(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "ingestr")
	script := `#!/bin/sh
if [ "$1" != "ingest" ]; then exit 12; fi
echo fake-cli "$@"
echo legacy-source-env "$SOURCE_URI"
echo legacy-dest-env "$DESTINATION_URI"
echo source-env "$INGESTR_SOURCE_URI"
echo dest-env "$INGESTR_DESTINATION_URI"
echo stderr-line >&2
`
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}

	jm := NewJobManager(filepath.Join(dir, "logs"), nil, binary)
	t.Setenv("SOURCE_URI", "csv:///wrong.csv")
	t.Setenv("DESTINATION_URI", "duckdb:///wrong.db")
	jobID, err := jm.StartJob(context.Background(), JobSpec{
		Args:        []string{"--source-uri=csv:///tmp/in.csv", "--dest-uri=duckdb:///tmp/out.db", "--source-table=users", "--yes"},
		SourceURI:   "csv:///tmp/in.csv",
		DestURI:     "duckdb:///tmp/out.db",
		SourceTable: "users",
		DestTable:   "users",
		Strategy:    "replace",
	})
	if err != nil {
		t.Fatalf("start job: %v", err)
	}

	var job *Job
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var ok bool
		job, ok = jm.GetJob(jobID)
		if ok && job.Status != "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if job == nil {
		t.Fatal("job not found")
	}
	if job.Status != "completed" {
		t.Fatalf("status = %q, want completed; error=%q", job.Status, job.Error)
	}

	logs := jm.SubscribeLogs(jobID)
	var messages []string
	for entry := range logs {
		messages = append(messages, entry.Message)
	}
	joined := strings.Join(messages, "\n")
	if !strings.Contains(joined, "fake-cli ingest --source-table=users --yes") {
		t.Fatalf("expected fake CLI output in logs, got %#v", messages)
	}
	if strings.Contains(joined, "fake-cli ingest --source-uri") || strings.Contains(joined, "fake-cli ingest --dest-uri") {
		t.Fatalf("source/destination URI should not be exposed in argv logs: %#v", messages)
	}
	if strings.Contains(joined, "wrong") {
		t.Fatalf("inherited source/destination URI environment should be overridden, got %#v", messages)
	}
	if !strings.Contains(joined, "legacy-source-env csv:///tmp/in.csv") ||
		!strings.Contains(joined, "legacy-dest-env duckdb:///tmp/out.db") ||
		!strings.Contains(joined, "source-env csv:///tmp/in.csv") ||
		!strings.Contains(joined, "dest-env duckdb:///tmp/out.db") {
		t.Fatalf("expected source/destination URI in subprocess environment, got %#v", messages)
	}
	if !strings.Contains(joined, "stderr-line") {
		t.Fatalf("expected stderr to be captured, got %#v", messages)
	}
}

func TestJobManagerCancelJob(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "ingestr")
	script := "#!/bin/sh\nif [ \"$1\" != \"ingest\" ]; then exit 12; fi\ntrap 'echo interrupted; exit 130' INT TERM\necho started\nwhile true; do sleep 1 & wait $!; done\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}

	jm := NewJobManager(filepath.Join(dir, "logs"), nil, binary)
	jobID, err := jm.StartJob(context.Background(), JobSpec{
		Args:        []string{"--source-uri=csv:///tmp/in.csv", "--dest-uri=duckdb:///tmp/out.db", "--source-table=users", "--yes"},
		SourceURI:   "csv:///tmp/in.csv",
		DestURI:     "duckdb:///tmp/out.db",
		SourceTable: "users",
		DestTable:   "users",
		Strategy:    "replace",
	})
	if err != nil {
		t.Fatalf("start job: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	if !jm.CancelJob(jobID) {
		t.Fatal("CancelJob returned false")
	}

	job := waitForJobStatus(t, jm, jobID)
	if job.Status != "canceled" {
		t.Fatalf("status = %q, want canceled; error=%q", job.Status, job.Error)
	}
}

func TestJobManagerRedactsCommandOutputURIs(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "ingestr")
	script := "#!/bin/sh\nif [ \"$1\" != \"ingest\" ]; then exit 12; fi\necho \"$INGESTR_SOURCE_URI\"\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}

	jm := NewJobManager(filepath.Join(dir, "logs"), nil, binary)
	jobID, err := jm.StartJob(context.Background(), JobSpec{
		Args:        []string{"--source-uri=postgres://user:secret@localhost/db", "--dest-uri=duckdb:///tmp/out.db", "--source-table=users", "--yes"},
		SourceURI:   "postgres://user:secret@localhost/db",
		DestURI:     "duckdb:///tmp/out.db",
		SourceTable: "users",
		DestTable:   "users",
		Strategy:    "replace",
	})
	if err != nil {
		t.Fatalf("start job: %v", err)
	}

	job := waitForJobStatus(t, jm, jobID)
	if job.Status != "completed" {
		t.Fatalf("status = %q, want completed; error=%q", job.Status, job.Error)
	}

	logs := jm.SubscribeLogs(jobID)
	var messages []string
	for entry := range logs {
		messages = append(messages, entry.Message)
	}
	joined := strings.Join(messages, "\n")
	if strings.Contains(joined, "secret") {
		t.Fatalf("command output leaked source URI password: %#v", messages)
	}
	if !strings.Contains(joined, "postgres://user:xxxxx@localhost/db") {
		t.Fatalf("command output did not include masked source URI: %#v", messages)
	}
}

func TestJobManagerPersistsLogsToFileAndRepository(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "ingestr")
	script := "#!/bin/sh\nif [ \"$1\" != \"ingest\" ]; then exit 12; fi\necho persisted-line\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}

	repo, err := NewSQLiteRepository(filepath.Join(dir, "runs.db"))
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	defer func() { _ = repo.Close() }()

	logsDir := filepath.Join(dir, "logs")
	jm := NewJobManager(logsDir, repo, binary)
	jobID, err := jm.StartJob(context.Background(), JobSpec{
		Args:        []string{"--source-uri=csv:///tmp/in.csv", "--dest-uri=duckdb:///tmp/out.db", "--source-table=users", "--yes"},
		SourceURI:   "csv:///tmp/in.csv",
		DestURI:     "duckdb:///tmp/out.db",
		SourceTable: "users",
		DestTable:   "users",
		Strategy:    "replace",
	})
	if err != nil {
		t.Fatalf("start job: %v", err)
	}

	job := waitForJobStatus(t, jm, jobID)
	if job.Status != "completed" {
		t.Fatalf("status = %q, want completed; error=%q", job.Status, job.Error)
	}
	waitForJobCleanup(t, jm, jobID)

	fileData, err := os.ReadFile(filepath.Join(logsDir, jobID+".jsonl"))
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.Contains(string(fileData), "persisted-line") {
		t.Fatalf("expected CLI output in JSONL log file, got %s", string(fileData))
	}

	records, err := repo.GetLogs(context.Background(), jobID)
	if err != nil {
		t.Fatalf("get repo logs: %v", err)
	}
	var messages []string
	for _, record := range records {
		messages = append(messages, record.Message)
	}
	if !strings.Contains(strings.Join(messages, "\n"), "persisted-line") {
		t.Fatalf("expected CLI output in repo logs, got %#v", messages)
	}
}

func TestStreamCommandOutputSuppressesCancellationReadError(t *testing.T) {
	jm := NewJobManager(t.TempDir(), nil, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	jm.streamCommandOutput(ctx, "canceled-job", failingReader{}, "info", JobSpec{}, &wg)
	wg.Wait()

	if val, ok := jm.logBuffers.Load("canceled-job"); ok {
		if entries := val.([]LogEntry); len(entries) != 0 {
			t.Fatalf("expected no cancellation read error log, got %#v", entries)
		}
	}
}

func TestSubscribeLogsUnknownJobCloses(t *testing.T) {
	jm := NewJobManager(t.TempDir(), nil, "")
	ch := jm.SubscribeLogs("missing")
	if _, ok := <-ch; ok {
		t.Fatal("expected unknown job subscription to be closed")
	}
}

func TestSubscribeLogsReplaysFullBuffer(t *testing.T) {
	jm := NewJobManager(t.TempDir(), nil, "")
	const jobID = "job-with-many-logs"
	const logCount = 150
	for i := range logCount {
		jm.broadcastLog(jobID, LogEntry{
			Timestamp: time.Now(),
			Level:     "info",
			Message:   "line " + strconv.Itoa(i),
		})
	}
	jm.jobFinished.Store(jobID, true)

	ch := jm.SubscribeLogs(jobID)
	var got int
	for range ch {
		got++
	}
	if got != logCount {
		t.Fatalf("replayed logs = %d, want %d", got, logCount)
	}
}

func TestBroadcastLogWarnsSlowSubscriber(t *testing.T) {
	jm := NewJobManager(t.TempDir(), nil, "")
	const jobID = "slow-subscriber"

	ch := make(chan LogEntry, 1)
	ch <- LogEntry{Timestamp: time.Now(), Level: "info", Message: "already queued"}
	jm.logSubs.Store(jobID, []chan LogEntry{ch})

	jm.broadcastLog(jobID, LogEntry{
		Timestamp: time.Now(),
		Level:     "info",
		Message:   "new line",
	})

	val, ok := jm.logSubs.Load(jobID)
	if !ok {
		t.Fatal("slow subscriber was removed")
	}
	if subs := val.([]chan LogEntry); len(subs) != 1 {
		t.Fatalf("subscriber count = %d, want 1", len(subs))
	}
	got := <-ch
	if got.Message != logStreamLagWarning {
		t.Fatalf("slow subscriber message = %q, want lag warning", got.Message)
	}
}

func TestBroadcastLogLagWarningDropsOnlyOldestQueuedEntry(t *testing.T) {
	jm := NewJobManager(t.TempDir(), nil, "")
	const jobID = "slow-subscriber-with-backlog"

	ch := make(chan LogEntry, 3)
	ch <- LogEntry{Timestamp: time.Now(), Level: "info", Message: "queued 0"}
	ch <- LogEntry{Timestamp: time.Now(), Level: "info", Message: "queued 1"}
	ch <- LogEntry{Timestamp: time.Now(), Level: "info", Message: "queued 2"}
	jm.logSubs.Store(jobID, []chan LogEntry{ch})

	jm.broadcastLog(jobID, LogEntry{
		Timestamp: time.Now(),
		Level:     "info",
		Message:   "new line",
	})

	got := []string{(<-ch).Message, (<-ch).Message, (<-ch).Message}
	want := []string{"queued 1", "queued 2", logStreamLagWarning}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("messages = %#v, want %#v", got, want)
		}
	}
}

func TestLogSinkCapsPendingPersistenceQueue(t *testing.T) {
	sink := newLogSink(nil)
	for i := range maxPendingPersistLogEntries + 2 {
		sink.enqueue(LogEntry{
			Timestamp: time.Unix(int64(i), 0),
			Level:     "info",
			Message:   "line " + strconv.Itoa(i),
		})
	}

	sink.mu.Lock()
	pending := sink.pending()
	dropped := sink.dropped
	sink.mu.Unlock()

	if pending != maxPendingPersistLogEntries {
		t.Fatalf("pending entries = %d, want %d", pending, maxPendingPersistLogEntries)
	}
	if dropped != 2 {
		t.Fatalf("dropped entries = %d, want 2", dropped)
	}

	warning, ok := sink.next()
	if !ok {
		t.Fatal("expected persistence lag warning")
	}
	if !strings.Contains(warning.Message, "2 log line(s) were skipped") {
		t.Fatalf("warning = %q, want skipped count", warning.Message)
	}

	next, ok := sink.next()
	if !ok {
		t.Fatal("expected retained log entry")
	}
	if next.Message != "line 2" {
		t.Fatalf("first retained entry = %q, want line 2", next.Message)
	}
}

func TestBroadcastLogCapsReplayBuffer(t *testing.T) {
	jm := NewJobManager(t.TempDir(), nil, "")
	const jobID = "capped-buffer"

	for i := range maxBufferedLogEntries + 3 {
		jm.broadcastLog(jobID, LogEntry{
			Timestamp: time.Now(),
			Level:     "info",
			Message:   "line " + strconv.Itoa(i),
		})
	}

	val, ok := jm.logBuffers.Load(jobID)
	if !ok {
		t.Fatal("expected log buffer")
	}
	buffer := val.([]LogEntry)
	if len(buffer) != maxBufferedLogEntries {
		t.Fatalf("buffer length = %d, want %d", len(buffer), maxBufferedLogEntries)
	}
	if buffer[0].Message != "line 3" {
		t.Fatalf("oldest retained log = %q, want line 3", buffer[0].Message)
	}
}

func TestStartJobReportsLogFileCreateFailure(t *testing.T) {
	dir := t.TempDir()
	logsPath := filepath.Join(dir, "logs-file")
	if err := os.WriteFile(logsPath, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write logs path file: %v", err)
	}

	binary := filepath.Join(dir, "ingestr")
	script := "#!/bin/sh\nif [ \"$1\" != \"ingest\" ]; then exit 12; fi\necho ok\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}

	jm := NewJobManager(logsPath, nil, binary)
	jobID, err := jm.StartJob(context.Background(), JobSpec{
		Args:        []string{"--source-uri=csv:///tmp/in.csv", "--dest-uri=duckdb:///tmp/out.db", "--source-table=users", "--yes"},
		SourceURI:   "csv:///tmp/in.csv",
		DestURI:     "duckdb:///tmp/out.db",
		SourceTable: "users",
		DestTable:   "users",
		Strategy:    "replace",
	})
	if err != nil {
		t.Fatalf("start job: %v", err)
	}

	job := waitForJobStatus(t, jm, jobID)
	if job.Status != "completed" {
		t.Fatalf("status = %q, want completed; error=%q", job.Status, job.Error)
	}

	logs := jm.SubscribeLogs(jobID)
	var messages []string
	for entry := range logs {
		messages = append(messages, entry.Message)
	}
	joined := strings.Join(messages, "\n")
	if !strings.Contains(joined, "Failed to create log file:") {
		t.Fatalf("expected log file warning, got %#v", messages)
	}
}

func TestMaskURI(t *testing.T) {
	got := maskURI("postgres://user:secret@localhost/db?api_key=abc&credentials_base64=private-json&credentials_path=/tmp/key.json&sslmode=require")
	if strings.Contains(got, "secret") || strings.Contains(got, "abc") || strings.Contains(got, "private-json") || strings.Contains(got, "/tmp/key.json") {
		t.Fatalf("maskURI leaked secret data: %s", got)
	}
	if !strings.Contains(got, "xxxxx") {
		t.Fatalf("maskURI did not redact sensitive fields: %s", got)
	}

	if got := maskURI("http://[::1"); got != "<redacted-uri>" {
		t.Fatalf("invalid URI mask = %q, want redacted placeholder", got)
	}
}

func TestMaskCLIArgs(t *testing.T) {
	got := strings.Join(maskCLIArgs([]string{
		"ingest",
		"--source-uri=postgres://user:secret@localhost/db",
		"--dest-uri=duckdb:///tmp/out.db",
	}), " ")
	if strings.Contains(got, "secret") {
		t.Fatalf("maskCLIArgs leaked secret data: %s", got)
	}
	if !strings.Contains(got, "--source-uri=postgres://user:xxxxx@localhost/db") {
		t.Fatalf("maskCLIArgs did not redact source URI password: %s", got)
	}
}

func waitForJobStatus(t *testing.T, jm *JobManager, jobID string) *Job {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		job, ok := jm.GetJob(jobID)
		if ok && job.Status != "running" {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	job, _ := jm.GetJob(jobID)
	if job == nil {
		t.Fatal("job not found")
	}
	t.Fatalf("job still running after timeout")
	return nil
}

func waitForJobCleanup(t *testing.T, jm *JobManager, jobID string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := jm.cancels.Load(jobID); !ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job cleanup did not finish after timeout")
}

type failingReader struct{}

func (failingReader) Read(_ []byte) (int, error) {
	return 0, os.ErrClosed
}
