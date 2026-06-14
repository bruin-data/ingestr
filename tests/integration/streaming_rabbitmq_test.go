//go:build integration

package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	_ "github.com/bruin-data/ingestr/pkg/source/rabbitmq"
	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestRabbitMQ_Streaming verifies continuous (--stream) ingestion from a queue:
// messages published before and during the stream are appended to the
// destination under the fixed msg_id+data envelope schema, deliveries are
// acked only after a flush (queue drains, no redelivery on a second run), and
// the stream exits cleanly on cancellation.
func TestRabbitMQ_Streaming(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	rmqURI := sharedRabbitMQURI(t)

	queueName := fmt.Sprintf("stream_queue_%d", time.Now().UnixNano())
	// Unique message IDs across batches (real brokers assign unique ids).
	publishStreamMessages(t, rmqURI, queueName, 1, 50)

	destContainer, err := postgres.Run(
		ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("destdb"),
		postgres.WithUsername("destuser"),
		postgres.WithPassword("destpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(30*time.Second),
		),
	)
	require.NoError(t, err)
	defer func() { _ = destContainer.Terminate(ctx) }()

	destConnString, err := destContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	destPool, err := pgxpool.New(ctx, destConnString)
	require.NoError(t, err)
	defer destPool.Close()

	cfg := &config.IngestConfig{
		SourceURI:     rmqURI,
		SourceTable:   queueName,
		DestURI:       destConnString,
		DestTable:     "public.events",
		Stream:        true,
		FlushInterval: 1 * time.Second,
		FlushRecords:  20,
		Progress:      config.ProgressLog,
	}

	streamCtx, cancelStream := context.WithCancel(ctx)
	runErr := make(chan error, 1)
	go func() { runErr <- pipeline.New(cfg).Run(streamCtx) }()

	rowCount := func() int {
		var n int
		if err := destPool.QueryRow(ctx, `SELECT count(*) FROM public.events`).Scan(&n); err != nil {
			return -1
		}
		return n
	}

	// First 50 messages land.
	require.Eventually(t, func() bool { return rowCount() == 50 }, 60*time.Second, 500*time.Millisecond,
		"50 pre-published messages should be appended")

	// Envelope schema: msg_id (PK) + data columns.
	cols := map[string]bool{}
	rows, err := destPool.Query(ctx, `SELECT column_name FROM information_schema.columns WHERE table_name='events'`)
	require.NoError(t, err)
	for rows.Next() {
		var c string
		require.NoError(t, rows.Scan(&c))
		cols[c] = true
	}
	rows.Close()
	assert.True(t, cols["msg_id"], "envelope should have msg_id column")
	assert.True(t, cols["data"], "envelope should have data column")

	// 30 more published while streaming, with fresh unique ids.
	publishStreamMessages(t, rmqURI, queueName, 51, 30)
	require.Eventually(t, func() bool {
		select {
		case err := <-runErr:
			t.Fatalf("streaming pipeline exited early: %v", err)
		default:
		}
		return rowCount() == 80
	}, 60*time.Second, 500*time.Millisecond,
		"messages published during streaming should also be appended")

	// Graceful shutdown.
	cancelStream()
	select {
	case err := <-runErr:
		if err != nil {
			require.ErrorIs(t, err, context.Canceled)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("streaming pipeline did not exit within 30s of cancellation")
	}

	// Ack-after-flush: the queue should be drained (all acked).
	require.Eventually(t, func() bool { return queueDepth(t, rmqURI, queueName) == 0 }, 15*time.Second, 500*time.Millisecond,
		"queue should be empty after acked flushes")

	// No redelivery: a second short run appends nothing.
	before := rowCount()
	ctx2, cancel2 := context.WithTimeout(ctx, 8*time.Second)
	defer cancel2()
	_ = pipeline.New(cfg).Run(ctx2)
	assert.Equal(t, before, rowCount(), "no messages should be redelivered after acknowledgment")
}

// TestRabbitMQ_StreamingShutdownFlushesPending verifies that cancelling a
// stream that still has un-flushed buffered data exits cleanly and acks during
// the final flush (regression test: the consumer channel must outlive its
// goroutine so CommitStream can ack at shutdown).
func TestRabbitMQ_StreamingShutdownFlushesPending(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	rmqURI := sharedRabbitMQURI(t)
	queueName := fmt.Sprintf("stream_shutdown_%d", time.Now().UnixNano())
	publishStreamMessages(t, rmqURI, queueName, 1, 40)

	destContainer, err := postgres.Run(
		ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("destdb"), postgres.WithUsername("destuser"), postgres.WithPassword("destpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(30*time.Second),
		),
	)
	require.NoError(t, err)
	defer func() { _ = destContainer.Terminate(ctx) }()
	destConnString, err := destContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	destPool, err := pgxpool.New(ctx, destConnString)
	require.NoError(t, err)
	defer destPool.Close()

	rowCount := func() int {
		var n int
		if err := destPool.QueryRow(ctx, `SELECT count(*) FROM public.events`).Scan(&n); err != nil {
			return -1
		}
		return n
	}

	// Long interval + high record cap so nothing flushes until shutdown.
	cfg := &config.IngestConfig{
		SourceURI: rmqURI, SourceTable: queueName, DestURI: destConnString, DestTable: "public.events",
		Stream: true, FlushInterval: 60 * time.Second, FlushRecords: 1_000_000, Progress: config.ProgressLog,
	}

	streamCtx, cancelStream := context.WithCancel(ctx)
	runErr := make(chan error, 1)
	go func() { runErr <- pipeline.New(cfg).Run(streamCtx) }()

	// Let the consumer drain the queue into its buffer (un-acked, un-flushed).
	require.Eventually(t, func() bool { return queueDepth(t, rmqURI, queueName) == 0 }, 30*time.Second, 500*time.Millisecond,
		"messages should be consumed into the buffer")
	time.Sleep(2 * time.Second)
	require.Equal(t, 0, rowCount(), "no flush should have happened yet (long interval)")

	// Cancel with data still buffered: the final flush must write and ack.
	cancelStream()
	select {
	case err := <-runErr:
		if err != nil {
			require.ErrorIs(t, err, context.Canceled, "shutdown with pending data must exit cleanly, not error on commit")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("pipeline did not exit within 30s of cancellation")
	}

	assert.Equal(t, 40, rowCount(), "final flush should have written all buffered messages")
	require.Eventually(t, func() bool { return queueDepth(t, rmqURI, queueName) == 0 }, 15*time.Second, 500*time.Millisecond,
		"queue must stay drained — the final flush acked the messages")

	// No redelivery on a fresh run: confirms the shutdown ack actually committed.
	ctx2, cancel2 := context.WithTimeout(ctx, 8*time.Second)
	defer cancel2()
	_ = pipeline.New(cfg).Run(ctx2)
	assert.Equal(t, 40, rowCount(), "no messages should be redelivered after the shutdown ack")
}

// publishStreamMessages publishes count messages with unique, sequential
// message IDs starting at startID (msg-<startID>..msg-<startID+count-1>).
func publishStreamMessages(t *testing.T, uri, queueName string, startID, count int) {
	t.Helper()
	conn, err := amqp.Dial(uri)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	ch, err := conn.Channel()
	require.NoError(t, err)
	defer func() { _ = ch.Close() }()
	_, err = ch.QueueDeclare(queueName, false, false, false, false, nil)
	require.NoError(t, err)
	for i := 0; i < count; i++ {
		id := startID + i
		err = ch.PublishWithContext(context.Background(), "", queueName, false, false, amqp.Publishing{
			ContentType: "application/json",
			Body:        []byte(fmt.Sprintf(`{"seq":%d}`, id)),
			MessageId:   fmt.Sprintf("msg-%d", id),
			Timestamp:   time.Now(),
		})
		require.NoError(t, err)
	}
	t.Logf("published %d messages (ids %d..%d) to %s", count, startID, startID+count-1, queueName)
}

func queueDepth(t *testing.T, uri, queue string) int {
	t.Helper()
	conn, err := amqp.Dial(uri)
	if err != nil {
		return -1
	}
	defer func() { _ = conn.Close() }()
	ch, err := conn.Channel()
	if err != nil {
		return -1
	}
	defer func() { _ = ch.Close() }()
	q, err := ch.QueueDeclarePassive(queue, false, false, false, false, nil)
	if err != nil {
		return -1
	}
	return q.Messages
}
