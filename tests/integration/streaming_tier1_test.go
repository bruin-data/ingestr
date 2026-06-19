//go:build integration

package integration

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	pubsubv1 "cloud.google.com/go/pubsub/apiv1"
	"cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awskinesis "github.com/aws/aws-sdk-go-v2/service/kinesis"
	kinesistypes "github.com/aws/aws-sdk-go-v2/service/kinesis/types"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/pipeline"
	_ "github.com/bruin-data/ingestr/pkg/source/kinesis"
	_ "github.com/bruin-data/ingestr/pkg/source/pubsub"
	_ "github.com/bruin-data/ingestr/pkg/source/sqs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

const (
	localAWSAccessKey = "test"
	localAWSSecretKey = "test"
	localAWSRegion    = "us-east-1"
	pubSubProjectID   = "test-project"
)

func TestSQS_Streaming(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	endpoint := startLocalStackContainer(t, ctx)
	sqsClient := newSQSClient(t, ctx, endpoint)
	queueName := fmt.Sprintf("stream-queue-%d", time.Now().UnixNano())
	queueURL := createSQSQueue(t, ctx, sqsClient, queueName)
	sendSQSMessages(t, ctx, sqsClient, queueURL, 1, 50)

	destURI, destSchema, destPool := streamingPostgresDest(t, ctx, "sqs")
	sourceURI := fmt.Sprintf("sqs://?endpoint_url=%s&access_key_id=%s&secret_access_key=%s&region=%s&visibility_timeout=30",
		endpoint, localAWSAccessKey, localAWSSecretKey, localAWSRegion)
	cfg := &config.IngestConfig{
		SourceURI:     sourceURI,
		SourceTable:   queueName,
		DestURI:       destURI,
		DestTable:     destSchema + ".events",
		Stream:        true,
		FlushInterval: time.Second,
		FlushRecords:  20,
		Progress:      config.ProgressLog,
	}

	runErr, cancel := runStreamingPipeline(ctx, cfg)
	rowCount := func() int { return postgresTableCount(ctx, destPool, destSchema, "events") }

	require.Eventually(t, func() bool {
		assertStreamStillRunning(t, runErr)
		return rowCount() == 50
	}, 90*time.Second, 500*time.Millisecond)

	sendSQSMessages(t, ctx, sqsClient, queueURL, 51, 30)
	require.Eventually(t, func() bool {
		assertStreamStillRunning(t, runErr)
		return rowCount() == 80
	}, 90*time.Second, 500*time.Millisecond)

	cancel()
	assertStreamStopped(t, runErr)
	require.Eventually(t, func() bool {
		return sqsQueueDepth(t, ctx, sqsClient, queueURL) == 0
	}, 30*time.Second, 500*time.Millisecond)

	before := rowCount()
	ctx2, cancel2 := context.WithTimeout(ctx, 8*time.Second)
	defer cancel2()
	_ = pipeline.New(cfg).Run(ctx2)
	assert.Equal(t, before, rowCount(), "acked SQS messages should not be redelivered")
}

func TestKinesis_Streaming(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	endpoint := startLocalStackContainer(t, ctx)
	client := newKinesisClient(t, ctx, endpoint)
	streamName := fmt.Sprintf("stream-%d", time.Now().UnixNano())
	createKinesisStream(t, ctx, client, streamName)
	putKinesisRecords(t, ctx, client, streamName, 1, 50)

	destURI, destSchema, destPool := streamingPostgresDest(t, ctx, "kinesis")
	sourceURI := fmt.Sprintf("kinesis://?endpoint_url=%s&aws_access_key_id=%s&aws_secret_access_key=%s&region_name=%s",
		endpoint, localAWSAccessKey, localAWSSecretKey, localAWSRegion)
	cfg := &config.IngestConfig{
		SourceURI:     sourceURI,
		SourceTable:   streamName,
		DestURI:       destURI,
		DestTable:     destSchema + ".events",
		Stream:        true,
		FlushInterval: time.Second,
		FlushRecords:  20,
		Progress:      config.ProgressLog,
	}

	runErr, cancel := runStreamingPipeline(ctx, cfg)
	rowCount := func() int { return postgresTableCount(ctx, destPool, destSchema, "events") }

	require.Eventually(t, func() bool {
		assertStreamStillRunning(t, runErr)
		return rowCount() == 50
	}, 90*time.Second, 500*time.Millisecond)

	putKinesisRecords(t, ctx, client, streamName, 51, 30)
	require.Eventually(t, func() bool {
		assertStreamStillRunning(t, runErr)
		return rowCount() == 80
	}, 90*time.Second, 500*time.Millisecond)

	cancel()
	assertStreamStopped(t, runErr)

	before := rowCount()
	ctx2, cancel2 := context.WithTimeout(ctx, 8*time.Second)
	defer cancel2()
	_ = pipeline.New(cfg).Run(ctx2)
	assert.Equal(t, before, rowCount(), "Kinesis replays should be idempotent by msg_id")
}

func TestPubSub_Streaming(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	endpoint := startPubSubEmulator(t, ctx)
	publisher, subscriber := newPubSubAdminClients(t, ctx, endpoint)
	defer func() { _ = publisher.Close() }()
	defer func() { _ = subscriber.Close() }()

	topicID := fmt.Sprintf("topic-%d", time.Now().UnixNano())
	subID := fmt.Sprintf("sub-%d", time.Now().UnixNano())
	topicName := "projects/" + pubSubProjectID + "/topics/" + topicID
	subName := "projects/" + pubSubProjectID + "/subscriptions/" + subID
	createPubSubTopicAndSubscription(t, ctx, publisher, subscriber, topicName, subName)
	publishPubSubMessages(t, ctx, publisher, topicName, 1, 50)

	destURI, destSchema, destPool := streamingPostgresDest(t, ctx, "pubsub")
	sourceURI := fmt.Sprintf("pubsub://%s?endpoint=%s&ack_deadline_seconds=5", pubSubProjectID, endpoint)
	cfg := &config.IngestConfig{
		SourceURI:     sourceURI,
		SourceTable:   subID,
		DestURI:       destURI,
		DestTable:     destSchema + ".events",
		Stream:        true,
		FlushInterval: time.Second,
		FlushRecords:  20,
		Progress:      config.ProgressLog,
	}

	runErr, cancel := runStreamingPipeline(ctx, cfg)
	rowCount := func() int { return postgresTableCount(ctx, destPool, destSchema, "events") }

	require.Eventually(t, func() bool {
		assertStreamStillRunning(t, runErr)
		return rowCount() == 50
	}, 90*time.Second, 500*time.Millisecond)

	publishPubSubMessages(t, ctx, publisher, topicName, 51, 30)
	require.Eventually(t, func() bool {
		assertStreamStillRunning(t, runErr)
		return rowCount() == 80
	}, 90*time.Second, 500*time.Millisecond)

	cancel()
	assertStreamStopped(t, runErr)
	time.Sleep(6 * time.Second)
	assert.False(t, pubSubHasAvailableMessages(t, ctx, subscriber, subName), "acked Pub/Sub messages should not redeliver")
}

func startLocalStackContainer(t *testing.T, ctx context.Context) string {
	t.Helper()
	req := testcontainers.ContainerRequest{
		Image:        "localstack/localstack:3.8.1",
		ExposedPorts: []string{"4566/tcp"},
		Env: map[string]string{
			"SERVICES":           "sqs,kinesis",
			"AWS_DEFAULT_REGION": localAWSRegion,
		},
		WaitingFor: wait.ForHTTP("/_localstack/health").
			WithPort("4566/tcp").
			WithStartupTimeout(120 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "4566")
	require.NoError(t, err)
	return fmt.Sprintf("http://%s:%s", host, port.Port())
}

func startPubSubEmulator(t *testing.T, ctx context.Context) string {
	t.Helper()
	req := testcontainers.ContainerRequest{
		Image:        "gcr.io/google.com/cloudsdktool/google-cloud-cli:emulators",
		ExposedPorts: []string{"8085/tcp"},
		Cmd: []string{
			"gcloud", "beta", "emulators", "pubsub", "start",
			"--project=" + pubSubProjectID,
			"--host-port=0.0.0.0:8085",
		},
		WaitingFor: wait.ForListeningPort("8085/tcp").WithStartupTimeout(120 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "8085")
	require.NoError(t, err)
	return fmt.Sprintf("%s:%s", host, port.Port())
}

func streamingPostgresDest(t *testing.T, ctx context.Context, prefix string) (string, string, *pgxpool.Pool) {
	t.Helper()
	destURI := sharedPostgresURI(t, "dest")
	destSchema := uniqueSchemaName(t, prefix)
	ensurePostgresSchema(t, ctx, destURI, destSchema)
	t.Cleanup(func() { dropPostgresSchema(t, ctx, destURI, destSchema) })

	pool, err := pgxpool.New(ctx, destURI)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return destURI, destSchema, pool
}

func runStreamingPipeline(ctx context.Context, cfg *config.IngestConfig) (<-chan error, context.CancelFunc) {
	streamCtx, cancel := context.WithCancel(ctx)
	runErr := make(chan error, 1)
	go func() { runErr <- pipeline.New(cfg).Run(streamCtx) }()
	return runErr, cancel
}

func assertStreamStillRunning(t *testing.T, runErr <-chan error) {
	t.Helper()
	select {
	case err := <-runErr:
		t.Fatalf("streaming pipeline exited early: %v", err)
	default:
	}
}

func assertStreamStopped(t *testing.T, runErr <-chan error) {
	t.Helper()
	select {
	case err := <-runErr:
		if err != nil {
			require.ErrorIs(t, err, context.Canceled)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("streaming pipeline did not exit within 30s of cancellation")
	}
}

func postgresTableCount(ctx context.Context, pool *pgxpool.Pool, schemaName, tableName string) int {
	var n int
	if err := pool.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s`, pqTable(schemaName, tableName))).Scan(&n); err != nil {
		return -1
	}
	return n
}

func localAWSConfig(t *testing.T, ctx context.Context) aws.Config {
	t.Helper()
	cfg, err := awsconfig.LoadDefaultConfig(
		ctx,
		awsconfig.WithRegion(localAWSRegion),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(localAWSAccessKey, localAWSSecretKey, "")),
	)
	require.NoError(t, err)
	return cfg
}

func newSQSClient(t *testing.T, ctx context.Context, endpoint string) *awssqs.Client {
	t.Helper()
	cfg := localAWSConfig(t, ctx)
	return awssqs.NewFromConfig(cfg, func(o *awssqs.Options) {
		o.BaseEndpoint = aws.String(endpoint)
	})
}

func createSQSQueue(t *testing.T, ctx context.Context, client *awssqs.Client, queueName string) string {
	t.Helper()
	out, err := client.CreateQueue(ctx, &awssqs.CreateQueueInput{QueueName: aws.String(queueName)})
	require.NoError(t, err)
	return aws.ToString(out.QueueUrl)
}

func sendSQSMessages(t *testing.T, ctx context.Context, client *awssqs.Client, queueURL string, startID, count int) {
	t.Helper()
	for offset := 0; offset < count; offset += 10 {
		end := offset + 10
		if end > count {
			end = count
		}
		entries := make([]sqstypes.SendMessageBatchRequestEntry, 0, end-offset)
		for i := offset; i < end; i++ {
			id := startID + i
			entries = append(entries, sqstypes.SendMessageBatchRequestEntry{
				Id:          aws.String(fmt.Sprintf("m%d", id)),
				MessageBody: aws.String(fmt.Sprintf(`{"seq":%d}`, id)),
			})
		}
		out, err := client.SendMessageBatch(ctx, &awssqs.SendMessageBatchInput{
			QueueUrl: aws.String(queueURL),
			Entries:  entries,
		})
		require.NoError(t, err)
		require.Empty(t, out.Failed)
	}
}

func sqsQueueDepth(t *testing.T, ctx context.Context, client *awssqs.Client, queueURL string) int {
	t.Helper()
	out, err := client.GetQueueAttributes(ctx, &awssqs.GetQueueAttributesInput{
		QueueUrl: aws.String(queueURL),
		AttributeNames: []sqstypes.QueueAttributeName{
			sqstypes.QueueAttributeNameApproximateNumberOfMessages,
			sqstypes.QueueAttributeNameApproximateNumberOfMessagesNotVisible,
		},
	})
	if err != nil {
		return -1
	}
	visible, _ := strconv.Atoi(out.Attributes[string(sqstypes.QueueAttributeNameApproximateNumberOfMessages)])
	inFlight, _ := strconv.Atoi(out.Attributes[string(sqstypes.QueueAttributeNameApproximateNumberOfMessagesNotVisible)])
	return visible + inFlight
}

func newKinesisClient(t *testing.T, ctx context.Context, endpoint string) *awskinesis.Client {
	t.Helper()
	cfg := localAWSConfig(t, ctx)
	return awskinesis.NewFromConfig(cfg, func(o *awskinesis.Options) {
		o.BaseEndpoint = aws.String(endpoint)
	})
}

func createKinesisStream(t *testing.T, ctx context.Context, client *awskinesis.Client, streamName string) {
	t.Helper()
	_, err := client.CreateStream(ctx, &awskinesis.CreateStreamInput{
		StreamName: aws.String(streamName),
		ShardCount: aws.Int32(1),
	})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		out, err := client.DescribeStreamSummary(ctx, &awskinesis.DescribeStreamSummaryInput{
			StreamName: aws.String(streamName),
		})
		return err == nil && out.StreamDescriptionSummary != nil && out.StreamDescriptionSummary.StreamStatus == kinesistypes.StreamStatusActive
	}, 60*time.Second, 500*time.Millisecond)
}

func putKinesisRecords(t *testing.T, ctx context.Context, client *awskinesis.Client, streamName string, startID, count int) {
	t.Helper()
	entries := make([]kinesistypes.PutRecordsRequestEntry, 0, count)
	for i := 0; i < count; i++ {
		id := startID + i
		entries = append(entries, kinesistypes.PutRecordsRequestEntry{
			Data:         []byte(fmt.Sprintf(`{"seq":%d}`, id)),
			PartitionKey: aws.String(fmt.Sprintf("pk-%d", id)),
		})
	}
	out, err := client.PutRecords(ctx, &awskinesis.PutRecordsInput{
		StreamName: aws.String(streamName),
		Records:    entries,
	})
	require.NoError(t, err)
	require.Zero(t, aws.ToInt32(out.FailedRecordCount))
}

func pubSubOptions(endpoint string) []option.ClientOption {
	return []option.ClientOption{
		option.WithEndpoint(endpoint),
		option.WithoutAuthentication(),
		option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
	}
}

func newPubSubAdminClients(t *testing.T, ctx context.Context, endpoint string) (*pubsubv1.PublisherClient, *pubsubv1.SubscriberClient) {
	t.Helper()
	opts := pubSubOptions(endpoint)
	publisher, err := pubsubv1.NewPublisherClient(ctx, opts...)
	require.NoError(t, err)
	subscriber, err := pubsubv1.NewSubscriberClient(ctx, opts...)
	require.NoError(t, err)
	return publisher, subscriber
}

func createPubSubTopicAndSubscription(t *testing.T, ctx context.Context, publisher *pubsubv1.PublisherClient, subscriber *pubsubv1.SubscriberClient, topicName, subName string) {
	t.Helper()
	_, err := publisher.CreateTopic(ctx, &pubsubpb.Topic{Name: topicName})
	require.NoError(t, err)
	_, err = subscriber.CreateSubscription(ctx, &pubsubpb.Subscription{
		Name:               subName,
		Topic:              topicName,
		AckDeadlineSeconds: 10,
	})
	require.NoError(t, err)
}

func publishPubSubMessages(t *testing.T, ctx context.Context, publisher *pubsubv1.PublisherClient, topicName string, startID, count int) {
	t.Helper()
	msgs := make([]*pubsubpb.PubsubMessage, 0, count)
	for i := 0; i < count; i++ {
		id := startID + i
		msgs = append(msgs, &pubsubpb.PubsubMessage{
			Data:       []byte(fmt.Sprintf(`{"seq":%d}`, id)),
			Attributes: map[string]string{"seq": strconv.Itoa(id)},
		})
	}
	out, err := publisher.Publish(ctx, &pubsubpb.PublishRequest{
		Topic:    topicName,
		Messages: msgs,
	})
	require.NoError(t, err)
	require.Len(t, out.MessageIds, count)
}

func pubSubHasAvailableMessages(t *testing.T, ctx context.Context, subscriber *pubsubv1.SubscriberClient, subName string) bool {
	t.Helper()
	pullCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	resp, err := subscriber.Pull(pullCtx, &pubsubpb.PullRequest{
		Subscription: subName,
		MaxMessages:  1,
	})
	if status.Code(err) == codes.DeadlineExceeded {
		return false
	}
	require.NoError(t, err)
	if len(resp.ReceivedMessages) == 0 {
		return false
	}
	ackIDs := make([]string, 0, len(resp.ReceivedMessages))
	for _, msg := range resp.ReceivedMessages {
		ackIDs = append(ackIDs, msg.GetAckId())
	}
	_ = subscriber.Acknowledge(ctx, &pubsubpb.AcknowledgeRequest{
		Subscription: subName,
		AckIds:       ackIDs,
	})
	return true
}
