package kafka

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	kafkago "github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl"
	"github.com/segmentio/kafka-go/sasl/plain"
	"github.com/segmentio/kafka-go/sasl/scram"
	"golang.org/x/crypto/sha3"
)

const (
	defaultBatchSize    = 3000
	defaultBatchTimeout = 3 * time.Second
	maxReadRetries      = 3
	retryBackoff        = 500 * time.Millisecond
	defaultParallelism  = 5
)

type kafkaConfig struct {
	BootstrapServers string
	GroupID          string
	SecurityProtocol string
	SASLMechanism    string
	SASLUsername     string
	SASLPassword     string
	BatchSize        int
	BatchTimeout     time.Duration

	// AWS MSK IAM (SASL OAUTHBEARER) settings.
	AWSRegion          string
	AWSRoleArn         string
	AWSRoleSessionName string
	AWSProfile         string
	AWSAccessKeyID     string
	AWSSecretAccessKey string
	AWSSessionToken    string
}

type KafkaSource struct {
	cfg kafkaConfig

	// streamReader holds the live consumer-group reader in streaming mode so
	// CommitStream can commit offsets after the pipeline has flushed.
	mu           sync.Mutex
	streamReader *kafkago.Reader
}

func NewKafkaSource() *KafkaSource {
	return &KafkaSource{}
}

func (s *KafkaSource) HandlesIncrementality() bool {
	return false
}

// SupportsStreaming reports that Kafka can be consumed continuously.
func (s *KafkaSource) SupportsStreaming() bool {
	return true
}

// DefaultStreamingStrategy returns merge keyed on the envelope msg_id so
// at-least-once redeliveries (e.g. after a group rebalance) are idempotent
// rather than producing duplicate rows or violating the msg_id primary key.
func (s *KafkaSource) DefaultStreamingStrategy() config.IncrementalStrategy {
	return config.StrategyMerge
}

// CommitStream commits the per-partition offsets captured in the token after
// the pipeline has durably written the corresponding messages.
func (s *KafkaSource) CommitStream(ctx context.Context, token any) error {
	msgs, ok := token.(map[int]kafkago.Message)
	if !ok {
		return fmt.Errorf("kafka: unexpected commit token type %T", token)
	}
	s.mu.Lock()
	reader := s.streamReader
	s.mu.Unlock()
	if reader == nil {
		// The reader was already closed (e.g. a final flush racing shutdown).
		// The data is durably written, so skipping the offset commit is safe
		// under at-least-once: the group re-reads from its last committed
		// position on the next start.
		config.Debug("[KAFKA] CommitStream: no active reader, skipping offset commit")
		return nil
	}
	flat := make([]kafkago.Message, 0, len(msgs))
	for _, m := range msgs {
		flat = append(flat, m)
	}
	if len(flat) == 0 {
		return nil
	}
	if err := reader.CommitMessages(ctx, flat...); err != nil {
		return fmt.Errorf("kafka: failed to commit offsets: %w", err)
	}
	return nil
}

// streamingEnvelopeSchema is the fixed schema for streaming Kafka ingestion:
// a primary-key message id plus a JSON column with the key, value, and metadata.
func streamingEnvelopeSchema(topic string) *schema.TableSchema {
	return &schema.TableSchema{
		Name: topic,
		Columns: []schema.Column{
			{Name: "msg_id", DataType: schema.TypeString, Nullable: false, IsPrimaryKey: true},
			{Name: "data", DataType: schema.TypeJSON, Nullable: true},
			// Monotonic per-key source position (Kafka offset). Used as the merge
			// incremental key so the latest record per msg_id wins within a flush
			// cycle. Same key => same partition => offsets are ordered.
			{Name: streamOrderColumn, DataType: schema.TypeInt64, Nullable: false},
		},
		PrimaryKeys:    []string{"msg_id"},
		IncrementalKey: streamOrderColumn,
	}
}

const streamOrderColumn = "_ingestr_order"

func (s *KafkaSource) Schemes() []string {
	return []string{"kafka"}
}

func (s *KafkaSource) Connect(ctx context.Context, uri string) error {
	cfg, err := parseKafkaURI(uri)
	if err != nil {
		return err
	}
	s.cfg = cfg
	config.Debug("[KAFKA] Connected to %s, group_id=%s", cfg.BootstrapServers, cfg.GroupID)
	return nil
}

func (s *KafkaSource) Close(ctx context.Context) error {
	// The streaming reader outlives its fetch goroutine so the pipeline's final
	// flush can commit offsets; it is closed here, after Run has returned.
	s.mu.Lock()
	r := s.streamReader
	s.streamReader = nil
	s.mu.Unlock()
	if r != nil {
		return r.Close()
	}
	return nil
}

func (s *KafkaSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	topicName := req.Name
	if topicName == "" {
		return nil, fmt.Errorf("kafka source requires a topic name as source-table")
	}

	strategy := req.Strategy
	if strategy == "" {
		strategy = config.StrategyReplace
	}

	// Streaming mode has no inferable schema (the stream never ends), so every
	// message is projected into a fixed msg_id + data envelope.
	if req.Streaming {
		primaryKeys := req.PrimaryKeys
		if len(primaryKeys) == 0 {
			primaryKeys = []string{"msg_id"}
		}
		return &source.DynamicSourceTable{
			TableName:           topicName,
			TablePrimaryKeys:    primaryKeys,
			TableIncrementalKey: req.IncrementalKey,
			TableStrategy:       strategy,
			KnownSchema:         true,
			SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
				return streamingEnvelopeSchema(topicName), nil
			},
			ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
				return s.readStreaming(ctx, topicName, opts)
			},
		}, nil
	}

	return &source.DynamicSourceTable{
		TableName:           topicName,
		TablePrimaryKeys:    req.PrimaryKeys,
		TableIncrementalKey: req.IncrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("kafka source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, topicName, opts)
		},
	}, nil
}

// readStreaming consumes the topic continuously via a consumer-group reader,
// committing offsets only after the pipeline durably writes each flush (so
// delivery is at-least-once). It uses FetchMessage (not ReadMessage) so offsets
// are never auto-committed before the data is durable.
func (s *KafkaSource) readStreaming(ctx context.Context, topic string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	dialer, err := s.buildDialer()
	if err != nil {
		return nil, fmt.Errorf("failed to build kafka dialer: %w", err)
	}

	if opts.IntervalStart != nil {
		output.Warnf("Warning: --interval-start is ignored for streaming Kafka; the consumer group's committed offsets determine the start position\n")
	}

	reader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:     strings.Split(s.cfg.BootstrapServers, ","),
		GroupID:     s.cfg.GroupID,
		Topic:       topic,
		Dialer:      dialer,
		MinBytes:    1,
		MaxBytes:    10e6,
		StartOffset: kafkago.FirstOffset, // only used when the group has no committed offset
	})

	s.mu.Lock()
	s.streamReader = reader
	s.mu.Unlock()

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		// Only close the results channel here; the reader is kept open (and
		// s.streamReader set) so the pipeline's final flush after cancellation
		// can still commit offsets via CommitStream. Close() closes the reader.
		defer close(results)

		envelopeCols := streamingEnvelopeSchema(topic).Columns
		batch := make([]map[string]interface{}, 0, s.cfg.BatchSize)
		latest := make(map[int]kafkago.Message) // per-partition highest message seen, for committing

		flush := func() bool {
			if len(batch) == 0 {
				return true
			}
			record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, envelopeCols, opts.ExcludeColumns)
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to convert kafka messages to Arrow: %w", err)}
				return false
			}
			// Cumulative token: a copy of the per-partition offsets emitted so far.
			token := make(map[int]kafkago.Message, len(latest))
			for p, m := range latest {
				token[p] = kafkago.Message{Topic: m.Topic, Partition: m.Partition, Offset: m.Offset}
			}
			results <- source.RecordBatchResult{Batch: record, CommitToken: token}
			batch = batch[:0]
			return true
		}

		for {
			fetchCtx, cancel := context.WithTimeout(ctx, s.cfg.BatchTimeout)
			msg, err := reader.FetchMessage(fetchCtx)
			cancel()

			if err != nil {
				if ctx.Err() != nil {
					flush()
					return
				}
				if errors.Is(err, context.DeadlineExceeded) {
					// Idle: flush any partial batch and keep consuming.
					if !flush() {
						return
					}
					continue
				}
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to fetch from topic %s: %w", topic, err)}
				return
			}

			batch = append(batch, messageToEnvelope(msg))
			latest[msg.Partition] = msg

			if len(batch) >= s.cfg.BatchSize {
				if !flush() {
					return
				}
			}
		}
	}()

	return results, nil
}

func (s *KafkaSource) read(ctx context.Context, topic string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	dialer, err := s.buildDialer()
	if err != nil {
		return nil, fmt.Errorf("failed to build kafka dialer: %w", err)
	}

	brokers := strings.Split(s.cfg.BootstrapServers, ",")

	partitions, err := s.getPartitions(ctx, dialer, brokers, topic)
	if err != nil {
		return nil, fmt.Errorf("failed to get partitions for topic %s: %w", topic, err)
	}
	config.Debug("[KAFKA] Topic %s has %d partitions", topic, len(partitions))

	type partitionOffsets struct {
		partition int
		start     int64
		end       int64
	}

	var assignments []partitionOffsets
	for _, p := range partitions {
		conn, err := dialLeaderWithFailover(ctx, dialer, brokers, topic, p)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to partition %d: %w", p, err)
		}

		var startOffset int64
		if opts.IntervalStart != nil {
			startOffset, err = s.offsetForTime(ctx, conn, *opts.IntervalStart)
			if err != nil {
				_ = conn.Close()
				return nil, fmt.Errorf("failed to get offset for interval start on partition %d: %w", p, err)
			}
		} else {
			first, err := conn.ReadFirstOffset()
			if err != nil {
				_ = conn.Close()
				return nil, fmt.Errorf("failed to read first offset for partition %d: %w", p, err)
			}
			startOffset = first
		}

		lastOffset, err := conn.ReadLastOffset()
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("failed to read last offset for partition %d: %w", p, err)
		}
		_ = conn.Close()

		if startOffset < lastOffset {
			assignments = append(assignments, partitionOffsets{
				partition: p,
				start:     startOffset,
				end:       lastOffset,
			})
			config.Debug("[KAFKA] Partition %d: offsets %d -> %d (%d messages)", p, startOffset, lastOffset, lastOffset-startOffset)
		}
	}

	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = defaultParallelism
	}
	if parallelism > len(assignments) {
		parallelism = len(assignments)
	}

	pCtx, pCancel := context.WithCancel(ctx)
	results := make(chan source.RecordBatchResult, 8)
	go func() {
		defer close(results)
		defer pCancel()

		sem := make(chan struct{}, parallelism)
		var wg sync.WaitGroup

		for _, a := range assignments {
			select {
			case <-pCtx.Done():
				wg.Wait()
				return
			case sem <- struct{}{}:
			}

			wg.Add(1)
			go func(a partitionOffsets) {
				defer wg.Done()
				defer func() { <-sem }()
				err := s.readPartition(pCtx, dialer, brokers, topic, a.partition, a.start, a.end, opts, results)
				if err != nil {
					pCancel()
					results <- source.RecordBatchResult{Err: err}
				}
			}(a)
		}

		wg.Wait()
	}()

	return results, nil
}

func (s *KafkaSource) readPartition(
	ctx context.Context,
	dialer *kafkago.Dialer,
	brokers []string,
	topic string,
	partition int,
	startOffset, endOffset int64,
	opts source.ReadOptions,
	results chan<- source.RecordBatchResult,
) error {
	reader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:   brokers,
		Topic:     topic,
		Partition: partition,
		Dialer:    dialer,
		MinBytes:  1,
		MaxBytes:  10e6,
	})
	defer func() { _ = reader.Close() }()

	if err := reader.SetOffset(startOffset); err != nil {
		return fmt.Errorf("failed to set offset for partition %d: %w", partition, err)
	}

	batch := make([]map[string]interface{}, 0, s.cfg.BatchSize)
	totalRead := int64(0)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var msg kafkago.Message
		var lastErr error
		ok := false
		for attempt := 0; attempt <= maxReadRetries; attempt++ {
			readCtx, cancel := context.WithTimeout(ctx, s.cfg.BatchTimeout)
			msg, lastErr = reader.ReadMessage(readCtx)
			cancel()

			if lastErr == nil {
				ok = true
				break
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if readCtx.Err() != nil {
				// batch timeout — flush remaining and stop this partition
				break
			}
			if !isRetriable(lastErr) {
				return fmt.Errorf("failed to read message from partition %d: %w", partition, lastErr)
			}
			config.Debug("[KAFKA] Retriable error on partition %d (attempt %d/%d): %v", partition, attempt+1, maxReadRetries, lastErr)
			time.Sleep(retryBackoff * time.Duration(attempt+1))
		}
		if !ok {
			if ctx.Err() == nil && errors.Is(lastErr, context.DeadlineExceeded) {
				break
			}
			return fmt.Errorf("failed to read message from partition %d after %d retries: %w", partition, maxReadRetries, lastErr)
		}

		if msg.Offset >= endOffset {
			break
		}

		item := messageToItem(msg)
		batch = append(batch, item)
		totalRead++

		if len(batch) >= s.cfg.BatchSize {
			if err := sendBatch(batch, opts, results); err != nil {
				return err
			}
			config.Debug("[KAFKA] Partition %d: sent %d messages (total: %d)", partition, len(batch), totalRead)
			batch = make([]map[string]interface{}, 0, s.cfg.BatchSize)
		}
	}

	if len(batch) > 0 {
		if err := sendBatch(batch, opts, results); err != nil {
			return err
		}
		config.Debug("[KAFKA] Partition %d: sent final %d messages (total: %d)", partition, len(batch), totalRead)
	}

	return nil
}

var (
	_ source.Source          = (*KafkaSource)(nil)
	_ source.StreamingSource = (*KafkaSource)(nil)
	_ source.StreamCommitter = (*KafkaSource)(nil)
)

func messageToItem(msg kafkago.Message) map[string]interface{} {
	var keyStr interface{}
	if msg.Key != nil {
		keyStr = string(msg.Key)
	}

	tsType := 0
	if !msg.Time.IsZero() {
		tsType = 1 // TIMESTAMP_CREATE_TIME
	}

	kafkaMeta := map[string]interface{}{
		"partition": msg.Partition,
		"topic":     msg.Topic,
		"key":       keyStr,
		"offset":    msg.Offset,
		"ts": map[string]interface{}{
			"type":  tsType,
			"value": msg.Time.Format(time.RFC3339Nano),
		},
		"data": string(msg.Value),
	}

	msgID := generateMsgID(msg.Topic, msg.Partition, msg.Offset, keyStr)

	return map[string]interface{}{
		"_kafka":        kafkaMeta,
		"_kafka_msg_id": msgID,
	}
}

// messageToEnvelope projects a Kafka message into the streaming envelope:
// a primary-key msg_id and a JSON data column holding the key, value, and
// metadata. The value is decoded as JSON so it nests as a structured object
// (parity with the RabbitMQ envelope); non-JSON values fall back to a string.
func messageToEnvelope(msg kafkago.Message) map[string]interface{} {
	item := messageToItem(msg)
	msgID := streamMsgID(msg)

	meta, _ := item["_kafka"].(map[string]interface{})
	if meta != nil {
		var body any
		dec := json.NewDecoder(bytes.NewReader(msg.Value))
		dec.UseNumber()
		if err := dec.Decode(&body); err != nil {
			body = string(msg.Value)
		}
		meta["data"] = body
	}

	encoded, err := json.Marshal(meta)
	if err != nil {
		encoded = fmt.Appendf(nil, "%q", string(msg.Value))
	}
	return map[string]interface{}{
		"msg_id":          msgID,
		"data":            string(encoded),
		streamOrderColumn: msg.Offset,
	}
}

// streamMsgID derives the streaming envelope's primary key. For keyed messages
// it is stable per key (so the merge keeps the latest record per key —
// changelog/compaction semantics). For keyless messages it includes the offset
// so every message gets a distinct id; otherwise all keyless messages on a
// partition would collide on one id and the merge would keep only the last per
// flush window (silent data loss). topic/partition/offset is stable across
// redeliveries, so at-least-once dedup still works.
func streamMsgID(msg kafkago.Message) string {
	if len(msg.Key) > 0 {
		return generateMsgID(msg.Topic, msg.Partition, msg.Offset, string(msg.Key))
	}
	return fmt.Sprintf("%s:%d:%d", msg.Topic, msg.Partition, msg.Offset)
}

func generateMsgID(topic string, partition int, offset int64, key interface{}) string {
	keyStr := "None"
	if key != nil {
		keyStr = fmt.Sprintf("%v", key)
	}
	input := fmt.Sprintf("%s%d%s", topic, partition, keyStr)
	h := sha3.NewShake128()
	h.Write([]byte(input))
	digest := make([]byte, 15)
	_, _ = h.Read(digest)
	return strings.TrimRight(base64.StdEncoding.EncodeToString(digest), "=")
}

func sendBatch(items []map[string]interface{}, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert kafka messages to Arrow: %w", err)
	}
	results <- source.RecordBatchResult{Batch: record}
	return nil
}

func (s *KafkaSource) buildDialer() (*kafkago.Dialer, error) {
	dialer := &kafkago.Dialer{
		Timeout:   10 * time.Second,
		DualStack: true,
	}

	if s.cfg.SecurityProtocol == "SASL_SSL" || s.cfg.SecurityProtocol == "SSL" {
		dialer.TLS = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}

	if s.cfg.SASLMechanism != "" {
		mechanism, err := buildSASLMechanism(s.cfg)
		if err != nil {
			return nil, err
		}
		dialer.SASLMechanism = mechanism

		// MSK IAM serves OAUTHBEARER only over TLS and the token is a sensitive
		// presigned URL, so enable TLS unless the user explicitly opted out.
		if strings.EqualFold(s.cfg.SASLMechanism, "OAUTHBEARER") &&
			dialer.TLS == nil && !strings.EqualFold(s.cfg.SecurityProtocol, "SASL_PLAINTEXT") {
			dialer.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
			config.Debug("[KAFKA] OAUTHBEARER: TLS auto-enabled (MSK IAM requires TLS)")
		}
	}

	return dialer, nil
}

func buildSASLMechanism(cfg kafkaConfig) (sasl.Mechanism, error) {
	mechanism := strings.ToUpper(cfg.SASLMechanism)
	switch mechanism {
	case "PLAIN":
		if err := validateSASLCredentials(mechanism, cfg); err != nil {
			return nil, err
		}
		return &plain.Mechanism{
			Username: cfg.SASLUsername,
			Password: cfg.SASLPassword,
		}, nil
	case "SCRAM-SHA-256":
		if err := validateSASLCredentials(mechanism, cfg); err != nil {
			return nil, err
		}
		m, err := scram.Mechanism(scram.SHA256, cfg.SASLUsername, cfg.SASLPassword)
		if err != nil {
			return nil, fmt.Errorf("failed to create SCRAM-SHA-256 mechanism: %w", err)
		}
		return m, nil
	case "SCRAM-SHA-512":
		if err := validateSASLCredentials(mechanism, cfg); err != nil {
			return nil, err
		}
		m, err := scram.Mechanism(scram.SHA512, cfg.SASLUsername, cfg.SASLPassword)
		if err != nil {
			return nil, fmt.Errorf("failed to create SCRAM-SHA-512 mechanism: %w", err)
		}
		return m, nil
	case "OAUTHBEARER":
		provider, err := newOAuthBearerTokenProvider(cfg)
		if err != nil {
			return nil, err
		}
		return oauthBearerMechanism{provider: provider}, nil
	default:
		return nil, fmt.Errorf("unsupported SASL mechanism: %s (supported: PLAIN, SCRAM-SHA-256, SCRAM-SHA-512, OAUTHBEARER)", cfg.SASLMechanism)
	}
}

func validateSASLCredentials(mechanism string, cfg kafkaConfig) error {
	if cfg.SASLUsername == "" || cfg.SASLPassword == "" {
		return fmt.Errorf("kafka SASL %s requires sasl_username and sasl_password", mechanism)
	}
	return nil
}

func (s *KafkaSource) getPartitions(ctx context.Context, dialer *kafkago.Dialer, brokers []string, topic string) ([]int, error) {
	var lastErr error
	for _, broker := range brokers {
		conn, err := dialer.DialContext(ctx, "tcp", broker)
		if err != nil {
			lastErr = err
			config.Debug("[KAFKA] Failed to connect to broker %s: %v", broker, err)
			continue
		}

		parts, err := conn.ReadPartitions(topic)
		_ = conn.Close()
		if err != nil {
			lastErr = err
			config.Debug("[KAFKA] Failed to read partitions from broker %s: %v", broker, err)
			continue
		}

		ids := make([]int, len(parts))
		for i, p := range parts {
			ids[i] = p.ID
		}
		return ids, nil
	}
	return nil, fmt.Errorf("failed to get partitions from any broker: %w", lastErr)
}

func (s *KafkaSource) offsetForTime(_ context.Context, conn *kafkago.Conn, t time.Time) (int64, error) {
	offset, err := conn.ReadOffset(t)
	if err != nil {
		output.Warnf("Warning: could not resolve offset for time %v: %v, falling back to last offset\n", t, err)
		last, lastErr := conn.ReadLastOffset()
		if lastErr != nil {
			return 0, fmt.Errorf("failed to read offset for time %v: %w", t, err)
		}
		if last > 0 {
			return last - 1, nil
		}
		return 0, nil
	}
	return offset, nil
}

func parseKafkaURI(uri string) (kafkaConfig, error) {
	if !strings.HasPrefix(uri, "kafka://") {
		return kafkaConfig{}, fmt.Errorf("invalid kafka URI: must start with kafka://")
	}

	rest := strings.TrimPrefix(uri, "kafka://")
	if rest == "" || rest == "?" {
		return kafkaConfig{}, fmt.Errorf("bootstrap_servers is required in kafka URI")
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return kafkaConfig{}, fmt.Errorf("failed to parse kafka URI query: %w", err)
	}

	bootstrapServers := values.Get("bootstrap_servers")
	if bootstrapServers == "" {
		return kafkaConfig{}, fmt.Errorf("bootstrap_servers is required in kafka URI")
	}

	groupID := values.Get("group_id")
	if groupID == "" {
		return kafkaConfig{}, fmt.Errorf("group_id is required in kafka URI")
	}

	batchSize := defaultBatchSize
	if bs := values.Get("batch_size"); bs != "" {
		parsed, err := strconv.Atoi(bs)
		if err != nil {
			return kafkaConfig{}, fmt.Errorf("invalid batch_size: %w", err)
		}
		if parsed <= 0 {
			return kafkaConfig{}, fmt.Errorf("batch_size must be positive")
		}
		batchSize = parsed
	}

	batchTimeout := defaultBatchTimeout
	if bt := values.Get("batch_timeout"); bt != "" {
		parsed, err := strconv.Atoi(bt)
		if err != nil {
			return kafkaConfig{}, fmt.Errorf("invalid batch_timeout: %w", err)
		}
		if parsed <= 0 {
			return kafkaConfig{}, fmt.Errorf("batch_timeout must be positive")
		}
		batchTimeout = time.Duration(parsed) * time.Second
	}

	return kafkaConfig{
		BootstrapServers:   bootstrapServers,
		GroupID:            groupID,
		SecurityProtocol:   values.Get("security_protocol"),
		SASLMechanism:      values.Get("sasl_mechanisms"),
		SASLUsername:       values.Get("sasl_username"),
		SASLPassword:       values.Get("sasl_password"),
		BatchSize:          batchSize,
		BatchTimeout:       batchTimeout,
		AWSRegion:          values.Get("aws_region"),
		AWSRoleArn:         values.Get("aws_role_arn"),
		AWSRoleSessionName: values.Get("aws_role_session_name"),
		AWSProfile:         values.Get("aws_profile"),
		AWSAccessKeyID:     values.Get("aws_access_key_id"),
		AWSSecretAccessKey: values.Get("aws_secret_access_key"),
		AWSSessionToken:    values.Get("aws_session_token"),
	}, nil
}

// Tries each broker in order until one succeeds.
func dialLeaderWithFailover(ctx context.Context, dialer *kafkago.Dialer, brokers []string, topic string, partition int) (*kafkago.Conn, error) {
	var lastErr error
	for _, broker := range brokers {
		conn, err := dialer.DialLeader(ctx, "tcp", broker, topic, partition)
		if err != nil {
			lastErr = err
			config.Debug("[KAFKA] Failed to dial leader via broker %s for partition %d: %v", broker, partition, err)
			continue
		}
		return conn, nil
	}
	return nil, fmt.Errorf("failed to dial leader for partition %d from any broker: %w", partition, lastErr)
}

func isRetriable(err error) bool {
	var kafkaErr kafkago.Error
	if errors.As(err, &kafkaErr) {
		return kafkaErr.Temporary()
	}
	return errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE)
}
