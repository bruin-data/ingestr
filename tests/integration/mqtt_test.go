//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	_ "github.com/bruin-data/ingestr/pkg/source/mqtt"
	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sharedMQTTURI(t *testing.T) string {
	t.Helper()
	if mqttShared.uri == "" {
		t.Skip("shared mqtt container not available")
	}
	return mqttShared.uri
}

func TestMQTTToSQLite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	mqttURI := sharedMQTTURI(t)
	topicPrefix := fmt.Sprintf("ingestr/finite/%d", time.Now().UnixNano())
	messageCount := 20

	publishMQTTMessages(t, mqttURI, topicPrefix, 1, messageCount, true)
	t.Cleanup(func() { clearRetainedMQTTMessages(t, mqttURI, topicPrefix, 1, messageCount) })

	tmpFile, err := os.CreateTemp("", "mqtt_test_*.db")
	require.NoError(t, err)
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	cfg := &config.IngestConfig{
		SourceURI:           mqttSourceURI(t, mqttURI, fmt.Sprintf("ingestr-finite-%d", time.Now().UnixNano())),
		SourceTable:         topicPrefix + "/#",
		DestURI:             fmt.Sprintf("sqlite:///%s", tmpFile.Name()),
		DestTable:           "messages",
		IncrementalStrategy: config.StrategyReplace,
		Progress:            config.ProgressLog,
	}

	require.NoError(t, pipeline.New(cfg).Run(ctx))

	db, err := sql.Open("sqlite3", tmpFile.Name())
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var count, withMessageID, withMsgID int
	err = db.QueryRow(`SELECT COUNT(*), COUNT(message_id), COUNT(msg_id) FROM messages`).Scan(&count, &withMessageID, &withMsgID)
	require.NoError(t, err)
	assert.Equal(t, messageCount, count)
	assert.Equal(t, messageCount, withMessageID, "QoS 1 messages should carry MQTT packet ids")
	assert.Equal(t, messageCount, withMsgID)
}

func TestMQTTStreamingToPostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	mqttURI := sharedMQTTURI(t)
	destURI := sharedPostgresURI(t, "dest")
	destSchema := uniqueSchemaName(t, "mqtt_stream")
	ensurePostgresSchema(t, ctx, destURI, destSchema)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, destURI, destSchema) })

	topicPrefix := fmt.Sprintf("ingestr/stream/%d", time.Now().UnixNano())
	publishMQTTMessages(t, mqttURI, topicPrefix, 1, 50, true)
	t.Cleanup(func() { clearRetainedMQTTMessages(t, mqttURI, topicPrefix, 1, 50) })

	destPool, err := pgxpool.New(ctx, destURI)
	require.NoError(t, err)
	defer destPool.Close()

	cfg := &config.IngestConfig{
		SourceURI:     mqttSourceURI(t, mqttURI, fmt.Sprintf("ingestr-stream-%d", time.Now().UnixNano())),
		SourceTable:   topicPrefix + "/#",
		DestURI:       destURI,
		DestTable:     destSchema + ".events",
		Stream:        true,
		FlushInterval: time.Second,
		FlushRecords:  20,
		Progress:      config.ProgressLog,
	}

	rowCount := func() int {
		var n int
		if err := destPool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s`, pqTable(destSchema, "events"))).Scan(&n); err != nil {
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
	}, 60*time.Second, 500*time.Millisecond, "retained MQTT messages should be ingested after subscription")

	publishMQTTMessages(t, mqttURI, topicPrefix, 51, 30, false)
	require.Eventually(t, func() bool {
		select {
		case err := <-runErr:
			t.Fatalf("streaming pipeline exited early: %v", err)
		default:
		}
		return rowCount() == 80
	}, 60*time.Second, 500*time.Millisecond, "live MQTT messages should be ingested while streaming")

	var withMessageID int
	require.NoError(t, destPool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(message_id) FROM %s`, pqTable(destSchema, "events"))).Scan(&withMessageID))
	assert.Equal(t, 80, withMessageID)

	cancelStream()
	select {
	case err := <-runErr:
		if err != nil {
			require.ErrorIs(t, err, context.Canceled)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("streaming pipeline did not exit within 30s of cancellation")
	}
}

func mqttSourceURI(t *testing.T, rawURI, clientID string) string {
	t.Helper()
	u, err := url.Parse(rawURI)
	require.NoError(t, err)
	q := u.Query()
	q.Set("client_id", clientID)
	q.Set("qos", "1")
	q.Set("batch_size", "10")
	q.Set("batch_timeout", "1s")
	u.RawQuery = q.Encode()
	return u.String()
}

func publishMQTTMessages(t *testing.T, rawURI, topicPrefix string, startID, count int, retained bool) {
	t.Helper()
	client := newMQTTTestClient(t, rawURI, fmt.Sprintf("ingestr-pub-%d", time.Now().UnixNano()))
	defer client.Disconnect(250)

	for i := 0; i < count; i++ {
		id := startID + i
		topic := fmt.Sprintf("%s/%d", topicPrefix, id)
		payload := []byte(fmt.Sprintf(`{"seq":%d,"name":"device-%d"}`, id, id))
		requireMQTTToken(t, client.Publish(topic, 1, retained, payload), 10*time.Second)
	}
	t.Logf("published %d MQTT messages (ids %d..%d) to %s", count, startID, startID+count-1, topicPrefix)
}

func clearRetainedMQTTMessages(t *testing.T, rawURI, topicPrefix string, startID, count int) {
	t.Helper()
	client := newMQTTTestClient(t, rawURI, fmt.Sprintf("ingestr-clear-%d", time.Now().UnixNano()))
	defer client.Disconnect(250)
	for i := 0; i < count; i++ {
		topic := fmt.Sprintf("%s/%d", topicPrefix, startID+i)
		requireMQTTToken(t, client.Publish(topic, 1, true, []byte{}), 10*time.Second)
	}
}

func newMQTTTestClient(t *testing.T, rawURI, clientID string) paho.Client {
	t.Helper()
	opts := paho.NewClientOptions().
		AddBroker(mqttBrokerURL(t, rawURI)).
		SetClientID(clientID).
		SetCleanSession(true).
		SetConnectTimeout(10 * time.Second)
	client := paho.NewClient(opts)
	requireMQTTToken(t, client.Connect(), 10*time.Second)
	return client
}

func mqttBrokerURL(t *testing.T, rawURI string) string {
	t.Helper()
	u, err := url.Parse(rawURI)
	require.NoError(t, err)
	require.NotEmpty(t, u.Host)
	return "tcp://" + u.Host
}

func requireMQTTToken(t *testing.T, token paho.Token, timeout time.Duration) {
	t.Helper()
	if !token.WaitTimeout(timeout) {
		t.Fatalf("MQTT token timed out after %s", timeout)
	}
	require.NoError(t, token.Error())
}
