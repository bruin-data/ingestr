//go:build integration

package integration

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	_ "github.com/bruin-data/ingestr/pkg/source/kafka"
	"github.com/jackc/pgx/v5/pgxpool"
	kafkago "github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestKafka_Streaming verifies continuous (--stream) ingestion from a Kafka
// topic: messages produced before and during the stream are appended under the
// msg_id+data envelope, offsets are committed only after a flush (a second run
// in the same group reads nothing new), and the stream exits on cancellation.
func TestKafka_Streaming(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	rp, err := redpanda.Run(ctx, "redpandadata/redpanda:v24.2.7")
	require.NoError(t, err)
	defer func() { _ = rp.Terminate(ctx) }()

	broker, err := rp.KafkaSeedBroker(ctx)
	require.NoError(t, err)

	topic := fmt.Sprintf("evt_%d", time.Now().UnixNano())
	groupID := "ingestr_test_group"
	ensureKafkaTopic(t, broker, topic)
	produceKafka(t, broker, topic, 1, 50)

	destContainer, err := postgres.Run(
		ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("destdb"),
		postgres.WithUsername("destuser"),
		postgres.WithPassword("destpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second),
		),
	)
	require.NoError(t, err)
	defer func() { _ = destContainer.Terminate(ctx) }()

	destConnString, err := destContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	destPool, err := pgxpool.New(ctx, destConnString)
	require.NoError(t, err)
	defer destPool.Close()

	sourceURI := fmt.Sprintf("kafka://?bootstrap_servers=%s&group_id=%s", broker, groupID)
	cfg := &config.IngestConfig{
		SourceURI:     sourceURI,
		SourceTable:   topic,
		DestURI:       destConnString,
		DestTable:     "public.events",
		Stream:        true,
		FlushInterval: 1 * time.Second,
		FlushRecords:  20,
		Progress:      config.ProgressLog,
	}

	rowCount := func() int {
		var n int
		if err := destPool.QueryRow(ctx, `SELECT count(*) FROM public.events`).Scan(&n); err != nil {
			return -1
		}
		return n
	}

	streamCtx, cancelStream := context.WithCancel(ctx)
	runErr := make(chan error, 1)
	go func() { runErr <- pipeline.New(cfg).Run(streamCtx) }()

	require.Eventually(t, func() bool {
		select {
		case err := <-runErr:
			t.Fatalf("streaming pipeline exited early: %v", err)
		default:
		}
		return rowCount() == 50
	}, 90*time.Second, 500*time.Millisecond, "50 pre-produced messages should be ingested")

	// Envelope schema.
	var cols int
	require.NoError(t, destPool.QueryRow(ctx,
		`SELECT count(*) FROM information_schema.columns WHERE table_name='events' AND column_name IN ('msg_id','data')`).Scan(&cols))
	assert.Equal(t, 2, cols, "envelope should have msg_id and data columns")

	// Produce more while streaming.
	produceKafka(t, broker, topic, 51, 30)
	require.Eventually(t, func() bool {
		select {
		case err := <-runErr:
			t.Fatalf("streaming pipeline exited early: %v", err)
		default:
		}
		return rowCount() == 80
	}, 90*time.Second, 500*time.Millisecond, "messages produced during streaming should be ingested")

	cancelStream()
	select {
	case err := <-runErr:
		if err != nil {
			require.ErrorIs(t, err, context.Canceled)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("streaming pipeline did not exit within 30s of cancellation")
	}

	// Offsets were committed after flush: a second run in the same group reads
	// nothing new (merge would dedup anyway, so assert the count is unchanged).
	before := rowCount()
	ctx2, cancel2 := context.WithTimeout(ctx, 10*time.Second)
	defer cancel2()
	_ = pipeline.New(cfg).Run(ctx2)
	assert.Equal(t, before, rowCount(), "committed offsets should prevent reprocessing")
}

func ensureKafkaTopic(t *testing.T, broker, topic string) {
	t.Helper()
	conn, err := kafkago.Dial("tcp", broker)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	controller, err := conn.Controller()
	require.NoError(t, err)
	ctrlConn, err := kafkago.Dial("tcp", net.JoinHostPort(controller.Host, strconv.Itoa(controller.Port)))
	require.NoError(t, err)
	defer func() { _ = ctrlConn.Close() }()

	err = ctrlConn.CreateTopics(kafkago.TopicConfig{Topic: topic, NumPartitions: 1, ReplicationFactor: 1})
	require.NoError(t, err)

	// Wait until the topic's partition leader is available.
	require.Eventually(t, func() bool {
		parts, err := conn.ReadPartitions(topic)
		return err == nil && len(parts) == 1 && parts[0].Leader.Host != ""
	}, 30*time.Second, 500*time.Millisecond, "topic %s should become available", topic)
}

func produceKafka(t *testing.T, broker, topic string, startID, count int) {
	t.Helper()
	w := &kafkago.Writer{
		Addr:                   kafkago.TCP(broker),
		Topic:                  topic,
		Balancer:               &kafkago.LeastBytes{},
		AllowAutoTopicCreation: true,
	}
	defer func() { _ = w.Close() }()

	msgs := make([]kafkago.Message, count)
	for i := 0; i < count; i++ {
		id := startID + i
		msgs[i] = kafkago.Message{
			Key:   []byte(fmt.Sprintf("k-%d", id)),
			Value: []byte(fmt.Sprintf(`{"seq":%d}`, id)),
		}
	}
	// Retry briefly: auto topic creation can lag on first write.
	var err error
	for attempt := 0; attempt < 10; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err = w.WriteMessages(ctx, msgs...)
		cancel()
		if err == nil {
			break
		}
		if !strings.Contains(err.Error(), "Unknown Topic") && !strings.Contains(err.Error(), "Leader Not Available") {
			break
		}
		time.Sleep(time.Second)
	}
	require.NoError(t, err)
	t.Logf("produced %d messages (ids %d..%d) to topic %s", count, startID, startID+count-1, topic)
}
