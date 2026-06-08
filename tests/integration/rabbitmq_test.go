//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	_ "github.com/bruin-data/ingestr/pkg/source/rabbitmq"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sharedRabbitMQURI(t *testing.T) string {
	t.Helper()
	if rabbitmqShared.uri == "" {
		t.Skip("shared rabbitmq container not available")
	}
	return rabbitmqShared.uri
}

func TestRabbitMQToSQLite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	rmqURI := sharedRabbitMQURI(t)

	queueName := fmt.Sprintf("test_queue_%d", time.Now().UnixNano())
	messageCount := 50

	publishTestMessages(t, rmqURI, queueName, messageCount)

	tmpFile, err := os.CreateTemp("", "rabbitmq_test_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	destURI := fmt.Sprintf("sqlite:///%s", tmpFile.Name())

	cfg := &config.IngestConfig{
		SourceURI:           rmqURI,
		SourceTable:         queueName,
		DestURI:             destURI,
		DestTable:           "messages",
		IncrementalStrategy: config.StrategyReplace,
	}

	p := pipeline.New(cfg)
	err = runPipeline(t, ctx, p)
	require.NoError(t, err)

	db, err := sql.Open("sqlite3", tmpFile.Name())
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM messages").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, messageCount, count, "should have ingested all messages")

	t.Logf("Validated %d rows in destination SQLite from RabbitMQ", count)
}

func TestRabbitMQToPostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	rmqURI := sharedRabbitMQURI(t)
	destURI := sharedPostgresURI(t, "dest")
	destSchema := uniqueSchemaName(t, "rmq")
	ensurePostgresSchema(t, ctx, destURI, destSchema)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, destURI, destSchema) })

	queueName := fmt.Sprintf("test_queue_pg_%d", time.Now().UnixNano())
	messageCount := 100

	publishTestMessages(t, rmqURI, queueName, messageCount)

	cfg := &config.IngestConfig{
		SourceURI:           rmqURI,
		SourceTable:         queueName,
		DestURI:             destURI,
		DestTable:           destSchema + ".messages",
		IncrementalStrategy: config.StrategyReplace,
	}

	p := pipeline.New(cfg)
	err := runPipeline(t, ctx, p)
	require.NoError(t, err)

	validatePostgresResults(t, ctx, destURI, destSchema, "messages", messageCount)
}

func TestRabbitMQEmptyQueue(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	rmqURI := sharedRabbitMQURI(t)

	queueName := fmt.Sprintf("test_empty_queue_%d", time.Now().UnixNano())

	conn, err := amqp.Dial(rmqURI)
	require.NoError(t, err)
	ch, err := conn.Channel()
	require.NoError(t, err)
	_, err = ch.QueueDeclare(queueName, false, false, false, false, nil)
	require.NoError(t, err)
	_ = ch.Close()
	_ = conn.Close()

	tmpFile, err := os.CreateTemp("", "rabbitmq_empty_test_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	destURI := fmt.Sprintf("sqlite:///%s", tmpFile.Name())

	cfg := &config.IngestConfig{
		SourceURI:           rmqURI,
		SourceTable:         queueName,
		DestURI:             destURI,
		DestTable:           "messages",
		IncrementalStrategy: config.StrategyReplace,
	}

	p := pipeline.New(cfg)
	err = runPipeline(t, ctx, p)
	require.NoError(t, err)

	t.Log("Empty queue ingestion completed without error")
}

func publishTestMessages(t *testing.T, uri string, queueName string, count int) {
	t.Helper()

	conn, err := amqp.Dial(uri)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	ch, err := conn.Channel()
	require.NoError(t, err)
	defer func() { _ = ch.Close() }()

	_, err = ch.QueueDeclare(
		queueName,
		false, // durable
		false, // auto-delete
		false, // exclusive
		false, // no-wait
		nil,   // args
	)
	require.NoError(t, err)

	for i := 0; i < count; i++ {
		body := map[string]interface{}{
			"id":    i + 1,
			"name":  fmt.Sprintf("User %d", i+1),
			"email": fmt.Sprintf("user%d@example.com", i+1),
			"age":   18 + (i % 50),
		}
		bodyBytes, err := json.Marshal(body)
		require.NoError(t, err)

		err = ch.PublishWithContext(
			context.Background(),
			"",        // exchange
			queueName, // routing key
			false,     // mandatory
			false,     // immediate
			amqp.Publishing{
				ContentType: "application/json",
				Body:        bodyBytes,
				MessageId:   fmt.Sprintf("msg-%d", i+1),
				Timestamp:   time.Now(),
			},
		)
		require.NoError(t, err)
	}

	t.Logf("Published %d test messages to queue %s", count, queueName)
}
