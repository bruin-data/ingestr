//go:build integration

package integration

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"testing"
	"time"

	pulsargo "github.com/apache/pulsar-client-go/pulsar"
	"github.com/bruin-data/ingestr/internal/config"
	sourcepkg "github.com/bruin-data/ingestr/pkg/source"
	natssource "github.com/bruin-data/ingestr/pkg/source/nats"
	pulsarsource "github.com/bruin-data/ingestr/pkg/source/pulsar"
	redissource "github.com/bruin-data/ingestr/pkg/source/redis"
	natsgo "github.com/nats-io/nats.go"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestRedis_BatchCutoff(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	redisURI := startRedisContainer(t, ctx)
	client := newRedisClient(t, redisURI)
	defer func() { _ = client.Close() }()

	stream := fmt.Sprintf("events:%d", time.Now().UnixNano())
	addRedisMessages(t, ctx, client, stream, 1, 50)

	src := redissource.NewRedisSource()
	require.NoError(t, src.Connect(ctx, redisURI+"?batch_timeout=1"))
	defer func() { _ = src.Close(ctx) }()
	table, err := src.GetTable(ctx, sourcepkg.TableRequest{Name: stream})
	require.NoError(t, err)
	records, err := table.Read(ctx, sourcepkg.ReadOptions{PageSize: 30})
	require.NoError(t, err)

	addRedisMessages(t, ctx, client, stream, 51, 30)
	require.Equal(t, int64(50), drainRecordCount(t, records), "batch read must stop at the startup cutoff")
}

func TestRedis_Streaming(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	redisURI := startRedisContainer(t, ctx)
	client := newRedisClient(t, redisURI)
	defer func() { _ = client.Close() }()

	stream := fmt.Sprintf("events:%d", time.Now().UnixNano())
	addRedisMessages(t, ctx, client, stream, 1, 20)
	seedRedisPending(t, ctx, client, stream, "ingestr", "previous-consumer", 4)

	destURI, destSchema, destPool := streamingPostgresDest(t, ctx, "redis")
	cfg := &config.IngestConfig{
		SourceURI:     redisURI + "?batch_timeout=1&batch_size=3&claim_min_idle=0",
		SourceTable:   stream,
		DestURI:       destURI,
		DestTable:     destSchema + ".events",
		Stream:        true,
		FlushInterval: time.Second,
		FlushRecords:  10,
		Progress:      config.ProgressLog,
	}
	runErr, cancel := runStreamingPipeline(ctx, cfg)
	rowCount := func() int { return postgresTableCount(ctx, destPool, destSchema, "events") }

	require.Eventually(t, func() bool {
		assertStreamStillRunning(t, runErr)
		return rowCount() == 20
	}, 60*time.Second, 500*time.Millisecond)

	addRedisMessages(t, ctx, client, stream, 21, 10)
	require.Eventually(t, func() bool {
		assertStreamStillRunning(t, runErr)
		return rowCount() == 30
	}, 60*time.Second, 500*time.Millisecond)
	require.Eventually(t, func() bool {
		pending, err := client.XPending(ctx, stream, "ingestr").Result()
		return err == nil && pending.Count == 0
	}, 30*time.Second, 500*time.Millisecond)

	cancel()
	assertStreamStopped(t, runErr)
}

func TestNATS_BatchCutoff(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	natsURI := startNATSContainer(t, ctx)
	nc, js := newNATSClient(t, natsURI)
	defer nc.Close()

	stream := fmt.Sprintf("EVENTS_%d", time.Now().UnixNano())
	subject := "events.cutoff"
	createNATSStream(t, js, stream, subject)
	publishNATSMessages(t, js, subject, 1, 50)

	sourceURI := natsURI + "?subject=" + url.QueryEscape(subject) + "&batch_timeout=1"
	src := natssource.NewNATSSource()
	require.NoError(t, src.Connect(ctx, sourceURI))
	defer func() { _ = src.Close(ctx) }()
	table, err := src.GetTable(ctx, sourcepkg.TableRequest{Name: stream})
	require.NoError(t, err)
	records, err := table.Read(ctx, sourcepkg.ReadOptions{PageSize: 30})
	require.NoError(t, err)

	publishNATSMessages(t, js, subject, 51, 30)
	require.Equal(t, int64(50), drainRecordCount(t, records), "batch read must stop at the startup cutoff")
}

func TestNATS_Streaming(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	ctx := context.Background()
	natsURI := startNATSContainer(t, ctx)
	nc, js := newNATSClient(t, natsURI)
	defer nc.Close()

	stream := fmt.Sprintf("EVENTS_%d", time.Now().UnixNano())
	subject := "events.stream"
	createNATSStream(t, js, stream, subject)
	publishNATSMessages(t, js, subject, 1, 20)

	destURI, destSchema, destPool := streamingPostgresDest(t, ctx, "nats")
	sourceURI := natsURI + "?subject=" + url.QueryEscape(subject) + "&durable=ingestr_" + stream + "&batch_timeout=1"
	cfg := &config.IngestConfig{
		SourceURI:     sourceURI,
		SourceTable:   stream,
		DestURI:       destURI,
		DestTable:     destSchema + ".events",
		Stream:        true,
		FlushInterval: time.Second,
		FlushRecords:  10,
		Progress:      config.ProgressLog,
	}
	runErr, cancel := runStreamingPipeline(ctx, cfg)
	rowCount := func() int { return postgresTableCount(ctx, destPool, destSchema, "events") }

	require.Eventually(t, func() bool {
		assertStreamStillRunning(t, runErr)
		return rowCount() == 20
	}, 60*time.Second, 500*time.Millisecond)

	publishNATSMessages(t, js, subject, 21, 10)
	require.Eventually(t, func() bool {
		assertStreamStillRunning(t, runErr)
		return rowCount() == 30
	}, 60*time.Second, 500*time.Millisecond)

	cancel()
	assertStreamStopped(t, runErr)
}

func TestPulsar_BatchCutoff(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if os.Getenv("INGESTR_TEST_PULSAR") == "" {
		t.Skip("set INGESTR_TEST_PULSAR=1 to run the heavier Pulsar standalone container test")
	}
	ctx := context.Background()
	pulsarURI, container := startPulsarContainer(t, ctx)
	client := newPulsarClient(t, pulsarURI)
	defer client.Close()

	topic := fmt.Sprintf("persistent://public/default/events-%d", time.Now().UnixNano())
	createPulsarTopic(t, ctx, container, topic)
	publishPulsarMessages(t, client, topic, 1, 50)

	src := pulsarsource.NewPulsarSource()
	require.NoError(t, src.Connect(ctx, pulsarURI+"?batch_timeout=1"))
	defer func() { _ = src.Close(ctx) }()
	table, err := src.GetTable(ctx, sourcepkg.TableRequest{Name: topic})
	require.NoError(t, err)
	records, err := table.Read(ctx, sourcepkg.ReadOptions{PageSize: 10})
	require.NoError(t, err)

	publishPulsarMessages(t, client, topic, 51, 30)
	require.Equal(t, int64(50), drainRecordCount(t, records), "batch read must stop at the startup cutoff")
}

func TestPulsar_Streaming(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if os.Getenv("INGESTR_TEST_PULSAR") == "" {
		t.Skip("set INGESTR_TEST_PULSAR=1 to run the heavier Pulsar standalone container test")
	}
	ctx := context.Background()
	pulsarURI, container := startPulsarContainer(t, ctx)
	client := newPulsarClient(t, pulsarURI)
	defer client.Close()

	topic := fmt.Sprintf("persistent://public/default/events-%d", time.Now().UnixNano())
	createPulsarTopic(t, ctx, container, topic)
	publishPulsarMessages(t, client, topic, 1, 20)

	destURI, destSchema, destPool := streamingPostgresDest(t, ctx, "pulsar")
	cfg := &config.IngestConfig{
		SourceURI:     pulsarURI + "?subscription=ingestr_test&batch_timeout=1",
		SourceTable:   topic,
		DestURI:       destURI,
		DestTable:     destSchema + ".events",
		Stream:        true,
		FlushInterval: time.Second,
		FlushRecords:  10,
		Progress:      config.ProgressLog,
	}
	runErr, cancel := runStreamingPipeline(ctx, cfg)
	rowCount := func() int { return postgresTableCount(ctx, destPool, destSchema, "events") }

	require.Eventually(t, func() bool {
		assertStreamStillRunning(t, runErr)
		return rowCount() == 20
	}, 90*time.Second, 500*time.Millisecond)

	publishPulsarMessages(t, client, topic, 21, 10)
	require.Eventually(t, func() bool {
		assertStreamStillRunning(t, runErr)
		return rowCount() == 30
	}, 90*time.Second, 500*time.Millisecond)

	cancel()
	assertStreamStopped(t, runErr)
}

func startRedisContainer(t *testing.T, ctx context.Context) string {
	t.Helper()
	req := testcontainers.ContainerRequest{
		Image:        "redis:7-alpine",
		ExposedPorts: []string{"6379/tcp"},
		WaitingFor:   wait.ForListeningPort("6379/tcp").WithStartupTimeout(60 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "6379")
	require.NoError(t, err)
	return fmt.Sprintf("redis://%s:%s/0", host, port.Port())
}

func newRedisClient(t *testing.T, redisURI string) *goredis.Client {
	t.Helper()
	opts, err := goredis.ParseURL(redisURI)
	require.NoError(t, err)
	return goredis.NewClient(opts)
}

func addRedisMessages(t *testing.T, ctx context.Context, client *goredis.Client, stream string, startID, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		id := startID + i
		err := client.XAdd(ctx, &goredis.XAddArgs{
			Stream: stream,
			Values: map[string]any{"seq": id, "payload": fmt.Sprintf(`{"seq":%d}`, id)},
		}).Err()
		require.NoError(t, err)
	}
}

func seedRedisPending(t *testing.T, ctx context.Context, client *goredis.Client, stream, group, consumer string, count int64) {
	t.Helper()
	require.NoError(t, client.XGroupCreateMkStream(ctx, stream, group, "0").Err())
	streams, err := client.XReadGroup(ctx, &goredis.XReadGroupArgs{
		Group:    group,
		Consumer: consumer,
		Streams:  []string{stream, ">"},
		Count:    count,
		Block:    -1,
	}).Result()
	require.NoError(t, err)
	var delivered int64
	for _, stream := range streams {
		delivered += int64(len(stream.Messages))
	}
	require.Equal(t, count, delivered)
}

func startNATSContainer(t *testing.T, ctx context.Context) string {
	t.Helper()
	req := testcontainers.ContainerRequest{
		Image:        "nats:2.10-alpine",
		ExposedPorts: []string{"4222/tcp"},
		Cmd:          []string{"-js"},
		WaitingFor:   wait.ForListeningPort("4222/tcp").WithStartupTimeout(60 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "4222")
	require.NoError(t, err)
	return fmt.Sprintf("nats://%s:%s", host, port.Port())
}

func newNATSClient(t *testing.T, natsURI string) (*natsgo.Conn, natsgo.JetStreamContext) {
	t.Helper()
	nc, err := natsgo.Connect(natsURI)
	require.NoError(t, err)
	js, err := nc.JetStream()
	require.NoError(t, err)
	return nc, js
}

func createNATSStream(t *testing.T, js natsgo.JetStreamContext, stream, subject string) {
	t.Helper()
	_, err := js.AddStream(&natsgo.StreamConfig{
		Name:     stream,
		Subjects: []string{subject},
	})
	require.NoError(t, err)
}

func publishNATSMessages(t *testing.T, js natsgo.JetStreamContext, subject string, startID, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		id := startID + i
		_, err := js.Publish(subject, []byte(fmt.Sprintf(`{"seq":%d}`, id)))
		require.NoError(t, err)
	}
}

func startPulsarContainer(t *testing.T, ctx context.Context) (string, testcontainers.Container) {
	t.Helper()
	req := testcontainers.ContainerRequest{
		Image:        "apachepulsar/pulsar:3.3.0",
		ExposedPorts: []string{"6650/tcp", "8080/tcp"},
		Cmd:          []string{"bin/pulsar", "standalone", "-nss", "-nfw"},
		WaitingFor: wait.ForAll(
			wait.ForListeningPort("6650/tcp"),
			wait.ForHTTP("/admin/v2/clusters").WithPort("8080/tcp"),
		).WithDeadline(180 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "6650")
	require.NoError(t, err)
	return fmt.Sprintf("pulsar://%s:%s", host, port.Port()), container
}

func createPulsarTopic(t *testing.T, ctx context.Context, container testcontainers.Container, topic string) {
	t.Helper()
	exitCode, output, err := container.Exec(ctx, []string{"bin/pulsar-admin", "topics", "create", topic})
	require.NoError(t, err)
	if exitCode == 0 {
		return
	}
	out, _ := io.ReadAll(output)
	require.Failf(t, "failed to create Pulsar topic", "exit code %d: %s", exitCode, string(out))
}

func newPulsarClient(t *testing.T, pulsarURI string) pulsargo.Client {
	t.Helper()
	client, err := pulsargo.NewClient(pulsargo.ClientOptions{URL: pulsarURI, OperationTimeout: 30 * time.Second})
	require.NoError(t, err)
	return client
}

func publishPulsarMessages(t *testing.T, client pulsargo.Client, topic string, startID, count int) {
	t.Helper()
	producer, err := client.CreateProducer(pulsargo.ProducerOptions{Topic: topic})
	require.NoError(t, err)
	defer producer.Close()
	for i := 0; i < count; i++ {
		id := startID + i
		_, err := producer.Send(context.Background(), &pulsargo.ProducerMessage{
			Payload: []byte(fmt.Sprintf(`{"seq":%d}`, id)),
			Key:     fmt.Sprintf("k-%d", id),
		})
		require.NoError(t, err)
	}
}

func drainRecordCount(t *testing.T, records <-chan sourcepkg.RecordBatchResult) int64 {
	t.Helper()
	var total int64
	for res := range records {
		require.NoError(t, res.Err)
		if res.Batch == nil {
			continue
		}
		total += res.Batch.NumRows()
		res.Batch.Release()
	}
	return total
}
