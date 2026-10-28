//go:build integration

package postgres_cdc

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestStandbyBatchAndStreamResumeExactly reads CDC from a hot standby and
// checks that positions derived from walsender keepalives neither lose nor
// replay published changes across batch resumes and a stream restart, while
// unpublished writes advance WAL in between.
func TestStandbyBatchAndStreamResumeExactly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	primary, standbyURI := startStandbyPair(t, ctx)
	_, err := primary.Exec(ctx, `
		CREATE TABLE public.items (id bigint PRIMARY KEY, value text);
		CREATE TABLE public.events (kind text);
		ALTER TABLE public.events REPLICA IDENTITY FULL;
		CREATE TABLE public.unpublished (id bigserial PRIMARY KEY, value text);
		INSERT INTO public.items SELECT i, 'initial' FROM generate_series(1, 10) i;
		INSERT INTO public.events SELECT 'seed-' || i FROM generate_series(1, 10) i;
		CREATE PUBLICATION standby_pub FOR TABLE public.items, public.events;
	`)
	require.NoError(t, err)
	waitForStandbyReplay(t, ctx, primary, standbyURI)

	// A standby builds a new slot's snapshot from running-transactions records
	// the primary logs only while active; log them continuously instead.
	nudgeCtx, stopNudge := context.WithCancel(ctx)
	defer stopNudge()
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-nudgeCtx.Done():
				return
			case <-ticker.C:
				_, _ = primary.Exec(nudgeCtx, "SELECT pg_log_standby_snapshot()")
			}
		}
	}()

	tables := []struct {
		name, column, slot string
		insert             func(i int) string
	}{
		{"public.items", "id", "standby_items", func(i int) string {
			return fmt.Sprintf("INSERT INTO public.items VALUES (%d, 'new')", i)
		}},
		{"public.events", "kind", "standby_events", func(i int) string {
			return fmt.Sprintf("INSERT INTO public.events VALUES ('new-%d')", i)
		}},
	}
	positions := make(map[string]string)
	for _, table := range tables {
		values, position := runStandbyBatch(t, ctx, standbyURI, table.name, table.column, table.slot, "")
		require.Len(t, values, 10, "%s snapshot", table.name)
		positions[table.name] = position
	}

	for i := 11; i <= 15; i++ {
		for _, table := range tables {
			_, err := primary.Exec(ctx, table.insert(i))
			require.NoError(t, err)
		}
		writeUnpublished(t, ctx, primary, 20)
	}
	waitForStandbyReplay(t, ctx, primary, standbyURI)
	for _, table := range tables {
		values, position := runStandbyBatch(t, ctx, standbyURI, table.name, table.column, table.slot, positions[table.name])
		require.Len(t, values, 5, "%s resumed batch must emit exactly the new rows", table.name)
		require.Greater(t, mustParseLSN(t, position), mustParseLSN(t, positions[table.name]))
		positions[table.name] = position
	}

	// Only unpublished WAL: the run must emit nothing and still move forward.
	writeUnpublished(t, ctx, primary, 200)
	waitForStandbyReplay(t, ctx, primary, standbyURI)
	for _, table := range tables {
		values, position := runStandbyBatch(t, ctx, standbyURI, table.name, table.column, table.slot, positions[table.name])
		require.Empty(t, values, "%s keepalive-only batch", table.name)
		require.Greater(t, mustParseLSN(t, position), mustParseLSN(t, positions[table.name]), "%s keepalive-only batch must advance", table.name)
		positions[table.name] = position
	}

	for _, table := range tables {
		_, err := primary.Exec(ctx, table.insert(16))
		require.NoError(t, err)
	}
	waitForStandbyReplay(t, ctx, primary, standbyURI)
	for _, table := range tables {
		values, position := runStandbyBatch(t, ctx, standbyURI, table.name, table.column, table.slot, positions[table.name])
		require.Len(t, values, 1, "%s batch after keepalive-only progress", table.name)
		positions[table.name] = position
	}

	// Stream the keyless table, where any replay at the resume boundary shows
	// up as a duplicate row, then restart from a keepalive-advanced checkpoint.
	stream := startStandbyStream(t, ctx, standbyURI, "public.events", "kind", "standby_events", positions["public.events"])
	_, err = primary.Exec(ctx, "INSERT INTO public.events VALUES ('stream-1')")
	require.NoError(t, err)
	require.Eventually(t, func() bool { return stream.count("stream-1") == 1 }, time.Minute, 50*time.Millisecond)
	var beforeUnpublished string
	require.NoError(t, primary.QueryRow(ctx, "SELECT pg_current_wal_lsn()::text").Scan(&beforeUnpublished))
	writeUnpublished(t, ctx, primary, 200)
	require.Eventually(t, func() bool {
		return stream.committed() > mustParseLSN(t, beforeUnpublished)
	}, time.Minute, 50*time.Millisecond, "keepalives must advance the stream checkpoint across unpublished WAL")
	resume := stream.stop(t)

	_, err = primary.Exec(ctx, "INSERT INTO public.events VALUES ('stream-2')")
	require.NoError(t, err)
	restarted := startStandbyStream(t, ctx, standbyURI, "public.events", "kind", "standby_events", resume)
	require.Eventually(t, func() bool { return restarted.count("stream-2") == 1 }, time.Minute, 50*time.Millisecond)
	time.Sleep(2 * time.Second)
	restarted.stop(t)
	require.Equal(t, []string{"stream-2"}, restarted.all(), "restarted stream must neither replay nor skip rows")
}

func startStandbyPair(t *testing.T, ctx context.Context) (*pgxpool.Pool, string) {
	t.Helper()
	net, err := network.New(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = net.Remove(context.Background()) })

	settings := []string{"-c", "wal_level=logical", "-c", "max_replication_slots=10", "-c", "max_wal_senders=10", "-c", "hot_standby_feedback=on"}
	primaryContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: "postgres:17-alpine",
			Env: map[string]string{
				"POSTGRES_USER": "testuser", "POSTGRES_PASSWORD": "testpass", "POSTGRES_DB": "testdb",
			},
			ExposedPorts:   []string{"5432/tcp"},
			Networks:       []string{net.Name},
			NetworkAliases: map[string][]string{net.Name: {"primary"}},
			Cmd:            append([]string{"postgres"}, settings...),
			WaitingFor: wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(time.Minute),
		},
		Started: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = primaryContainer.Terminate(context.Background()) })

	code, _, err := primaryContainer.Exec(ctx, []string{"sh", "-c", "echo 'host replication all all scram-sha-256' >> \"$PGDATA/pg_hba.conf\""})
	require.NoError(t, err)
	require.Zero(t, code)
	primary, err := pgxpool.New(ctx, containerURI(t, ctx, primaryContainer))
	require.NoError(t, err)
	t.Cleanup(primary.Close)
	_, err = primary.Exec(ctx, "SELECT pg_reload_conf(), pg_create_physical_replication_slot('standby1')")
	require.NoError(t, err)

	standbyContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:17-alpine",
			Env:          map[string]string{"PGPASSWORD": "testpass"},
			ExposedPorts: []string{"5432/tcp"},
			Networks:     []string{net.Name},
			Entrypoint: []string{"sh", "-c", `set -e
as_postgres=$(command -v su-exec || command -v gosu)
mkdir -p "$PGDATA" && chown postgres:postgres "$PGDATA" && chmod 700 "$PGDATA"
"$as_postgres" postgres pg_basebackup -h primary -U testuser -D "$PGDATA" -R -X stream -S standby1
exec "$as_postgres" postgres postgres "$@"`, "standby"},
			Cmd: append(settings, "-c", "hot_standby=on"),
			WaitingFor: wait.ForLog("database system is ready to accept read-only connections").
				WithStartupTimeout(time.Minute),
		},
		Started: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = standbyContainer.Terminate(context.Background()) })

	standbyURI := strings.Replace(containerURI(t, ctx, standbyContainer), "postgres://", "postgres+cdc://", 1) + "&publication=standby_pub"
	return primary, standbyURI
}

func containerURI(t *testing.T, ctx context.Context, container testcontainers.Container) string {
	t.Helper()
	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)
	return fmt.Sprintf("postgres://testuser:testpass@%s:%s/testdb?sslmode=disable", host, port.Port())
}

func waitForStandbyReplay(t *testing.T, ctx context.Context, primary *pgxpool.Pool, standbyURI string) {
	t.Helper()
	var target string
	require.NoError(t, primary.QueryRow(ctx, "SELECT pg_current_wal_lsn()::text").Scan(&target))
	standby, err := pgxpool.New(ctx, strings.Replace(strings.Split(standbyURI, "&publication=")[0], "postgres+cdc://", "postgres://", 1))
	require.NoError(t, err)
	defer standby.Close()
	require.Eventually(t, func() bool {
		var replayed bool
		return standby.QueryRow(ctx, "SELECT pg_last_wal_replay_lsn() >= $1::pg_lsn", target).Scan(&replayed) == nil && replayed
	}, time.Minute, 50*time.Millisecond)
}

func writeUnpublished(t *testing.T, ctx context.Context, primary *pgxpool.Pool, n int) {
	t.Helper()
	for range n {
		_, err := primary.Exec(ctx, "INSERT INTO public.unpublished (value) VALUES (md5(random()::text))")
		require.NoError(t, err)
	}
}

func connectStandbySource(t *testing.T, ctx context.Context, uri, slot string) *PostgresCDCSource {
	t.Helper()
	src := NewPostgresCDCSource()
	require.NoError(t, src.Connect(ctx, uri+"&slot="+slot))
	require.True(t, src.standby, "the source must detect the standby")
	return src
}

func runStandbyBatch(t *testing.T, ctx context.Context, uri, tableName, column, slot, resume string) ([]string, string) {
	t.Helper()
	src := connectStandbySource(t, ctx, uri, slot)
	defer func() { _ = src.Close(context.Background()) }()
	table, err := src.GetTable(ctx, source.TableRequest{Name: tableName})
	require.NoError(t, err)
	tableSchema, err := table.GetSchema(ctx)
	require.NoError(t, err)
	records, err := table.Read(ctx, source.ReadOptions{PageSize: 100, Schema: tableSchema, CDCResumeLSN: resume})
	require.NoError(t, err)
	var values []string
	for result := range records {
		require.NoError(t, result.Err)
		values = append(values, batchColumn(t, result, column)...)
	}
	position := src.CDCState().Position
	require.NotEmpty(t, position)
	require.NoError(t, src.FinalizeBatch(ctx))
	return values, position
}

func batchColumn(t *testing.T, result source.RecordBatchResult, column string) []string {
	t.Helper()
	if result.Batch == nil {
		return nil
	}
	defer result.Batch.Release()
	indices := result.Batch.Schema().FieldIndices(column)
	require.Len(t, indices, 1)
	values := result.Batch.Column(indices[0])
	out := make([]string, values.Len())
	for i := range out {
		out[i] = values.ValueStr(i)
	}
	return out
}

func mustParseLSN(t *testing.T, raw string) pglogrepl.LSN {
	t.Helper()
	lsn, err := parseStoredPostgresLSN(raw)
	require.NoError(t, err)
	return lsn
}

type standbyStream struct {
	src    *PostgresCDCSource
	cancel context.CancelFunc
	done   chan struct{}

	mu       sync.Mutex
	values   []string
	position string
	lsn      pglogrepl.LSN
	err      error
}

// startStandbyStream consumes a stream the way the pipeline does: rows are
// durable once received, so every commit token is confirmed immediately.
func startStandbyStream(t *testing.T, ctx context.Context, uri, tableName, column, slot, resume string) *standbyStream {
	t.Helper()
	src := connectStandbySource(t, ctx, uri, slot)
	table, err := src.GetTable(ctx, source.TableRequest{Name: tableName})
	require.NoError(t, err)
	tableSchema, err := table.GetSchema(ctx)
	require.NoError(t, err)
	streamCtx, cancel := context.WithCancel(ctx)
	records, err := table.Read(streamCtx, source.ReadOptions{PageSize: 100, Schema: tableSchema, CDCResumeLSN: resume, Streaming: true})
	require.NoError(t, err)

	s := &standbyStream{src: src, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(s.done)
		for result := range records {
			if result.Err != nil {
				if streamCtx.Err() == nil {
					s.mu.Lock()
					s.err = result.Err
					s.mu.Unlock()
				}
				continue
			}
			values := batchColumn(t, result, column)
			token, ok := result.CommitToken.(source.CDCStateCommitToken)
			s.mu.Lock()
			s.values = append(s.values, values...)
			if ok && token.Position != "" {
				if lsn, ok := token.SourceCommitToken.(pglogrepl.LSN); ok && lsn >= s.lsn {
					s.lsn = lsn
					s.position = token.Position
				}
			}
			s.mu.Unlock()
			if ok {
				_ = src.CommitStream(streamCtx, token)
			}
		}
	}()
	return s
}

func (s *standbyStream) count(value string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, v := range s.values {
		if v == value {
			n++
		}
	}
	return n
}

func (s *standbyStream) all() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.values...)
}

func (s *standbyStream) committed() pglogrepl.LSN {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lsn
}

// stop ends the stream and returns its last committed position.
func (s *standbyStream) stop(t *testing.T) string {
	t.Helper()
	s.cancel()
	<-s.done
	require.NoError(t, s.src.Close(context.Background()))
	s.mu.Lock()
	defer s.mu.Unlock()
	require.NoError(t, s.err)
	require.NotEmpty(t, s.position)
	return s.position
}
