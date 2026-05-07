package kafka

import (
	"context"
	"crypto/tls"
	"encoding/base64"
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
}

type KafkaSource struct {
	cfg kafkaConfig
}

func NewKafkaSource() *KafkaSource {
	return &KafkaSource{}
}

func (s *KafkaSource) HandlesIncrementality() bool {
	return false
}

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

	if s.cfg.SASLMechanism != "" && s.cfg.SASLUsername != "" {
		mechanism, err := buildSASLMechanism(s.cfg.SASLMechanism, s.cfg.SASLUsername, s.cfg.SASLPassword)
		if err != nil {
			return nil, err
		}
		dialer.SASLMechanism = mechanism
	}

	return dialer, nil
}

func buildSASLMechanism(mechanism, username, password string) (sasl.Mechanism, error) {
	switch strings.ToUpper(mechanism) {
	case "PLAIN":
		return &plain.Mechanism{
			Username: username,
			Password: password,
		}, nil
	case "SCRAM-SHA-256":
		m, err := scram.Mechanism(scram.SHA256, username, password)
		if err != nil {
			return nil, fmt.Errorf("failed to create SCRAM-SHA-256 mechanism: %w", err)
		}
		return m, nil
	case "SCRAM-SHA-512":
		m, err := scram.Mechanism(scram.SHA512, username, password)
		if err != nil {
			return nil, fmt.Errorf("failed to create SCRAM-SHA-512 mechanism: %w", err)
		}
		return m, nil
	default:
		return nil, fmt.Errorf("unsupported SASL mechanism: %s (supported: PLAIN, SCRAM-SHA-256, SCRAM-SHA-512)", mechanism)
	}
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
		fmt.Printf("Warning: could not resolve offset for time %v: %v, falling back to last offset\n", t, err)
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
		BootstrapServers: bootstrapServers,
		GroupID:          groupID,
		SecurityProtocol: values.Get("security_protocol"),
		SASLMechanism:    values.Get("sasl_mechanisms"),
		SASLUsername:     values.Get("sasl_username"),
		SASLPassword:     values.Get("sasl_password"),
		BatchSize:        batchSize,
		BatchTimeout:     batchTimeout,
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
