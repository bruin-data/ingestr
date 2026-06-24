package pulsar

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	pulsargo "github.com/apache/pulsar-client-go/pulsar"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	defaultBatchSize     = 3000
	defaultBatchTimeout  = 5 * time.Second
	defaultClientTimeout = 30 * time.Second
	streamOrderColumn    = "_ingestr_order"
)

type pulsarConfig struct {
	ServiceURL        string
	Subscription      string
	Token             string
	BatchSize         int
	BatchTimeout      time.Duration
	OperationTimeout  time.Duration
	ConnectionTimeout time.Duration
	TLSAllowInsecure  bool
	TLSValidateHost   bool
}

type pulsarCommitToken struct {
	MaxSeq int64
}

type trackedMessageID struct {
	seq int64
	id  pulsargo.MessageID
}

type PulsarSource struct {
	cfg    pulsarConfig
	client pulsargo.Client

	mu             sync.Mutex
	streamConsumer pulsargo.Consumer
	nextSeq        int64
	pending        []trackedMessageID
}

func NewPulsarSource() *PulsarSource {
	return &PulsarSource{}
}

func (s *PulsarSource) Schemes() []string {
	return []string{"pulsar", "pulsar+ssl"}
}

func (s *PulsarSource) Connect(_ context.Context, raw string) error {
	cfg, err := parsePulsarURI(raw)
	if err != nil {
		return err
	}
	opts := pulsargo.ClientOptions{
		URL:                        cfg.ServiceURL,
		OperationTimeout:           cfg.OperationTimeout,
		ConnectionTimeout:          cfg.ConnectionTimeout,
		TLSAllowInsecureConnection: cfg.TLSAllowInsecure,
		TLSValidateHostname:        cfg.TLSValidateHost,
	}
	if cfg.Token != "" {
		opts.Authentication = pulsargo.NewAuthenticationToken(cfg.Token)
	}
	client, err := pulsargo.NewClient(opts)
	if err != nil {
		return fmt.Errorf("failed to connect to Pulsar: %w", err)
	}
	s.cfg = cfg
	s.client = client
	config.Debug("[PULSAR] Connected to %s", cfg.ServiceURL)
	return nil
}

func (s *PulsarSource) Close(_ context.Context) error {
	s.mu.Lock()
	consumer := s.streamConsumer
	s.streamConsumer = nil
	s.mu.Unlock()
	if consumer != nil {
		consumer.Close()
	}
	if s.client != nil {
		s.client.Close()
	}
	return nil
}

func (s *PulsarSource) HandlesIncrementality() bool {
	return false
}

func (s *PulsarSource) SupportsStreaming() bool {
	return true
}

func (s *PulsarSource) DefaultStreamingStrategy() config.IncrementalStrategy {
	return config.StrategyMerge
}

func (s *PulsarSource) GetTable(_ context.Context, req source.TableRequest) (source.SourceTable, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("pulsar source requires a topic as source-table")
	}
	strategy := req.Strategy
	if strategy == "" {
		strategy = config.StrategyReplace
	}
	if req.Streaming {
		streamStrategy := req.Strategy
		if streamStrategy == "" {
			streamStrategy = s.DefaultStreamingStrategy()
		}
		primaryKeys := req.PrimaryKeys
		if len(primaryKeys) == 0 {
			primaryKeys = []string{"msg_id"}
		}
		return &source.DynamicSourceTable{
			TableName:           tableName(req.Name),
			TablePrimaryKeys:    primaryKeys,
			TableIncrementalKey: streamOrderColumn,
			TableStrategy:       streamStrategy,
			KnownSchema:         true,
			SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
				return streamingEnvelopeSchema(tableName(req.Name)), nil
			},
			ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
				return s.read(ctx, req.Name, opts)
			},
		}, nil
	}

	return &source.DynamicSourceTable{
		TableName:           tableName(req.Name),
		TablePrimaryKeys:    []string{"pulsar_msg_id"},
		TableIncrementalKey: req.IncrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("pulsar source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, req.Name, opts)
		},
	}, nil
}

func streamingEnvelopeSchema(name string) *schema.TableSchema {
	return &schema.TableSchema{
		Name: name,
		Columns: []schema.Column{
			{Name: "msg_id", DataType: schema.TypeString, Nullable: false, IsPrimaryKey: true},
			{Name: "data", DataType: schema.TypeJSON, Nullable: true},
			{Name: streamOrderColumn, DataType: schema.TypeInt64, Nullable: false},
		},
		PrimaryKeys:    []string{"msg_id"},
		IncrementalKey: streamOrderColumn,
	}
}

func parsePulsarURI(raw string) (pulsarConfig, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return pulsarConfig{}, fmt.Errorf("invalid Pulsar URI: %w", err)
	}
	if u.Scheme != "pulsar" && u.Scheme != "pulsar+ssl" {
		return pulsarConfig{}, fmt.Errorf("invalid Pulsar URI: must start with pulsar:// or pulsar+ssl://")
	}
	q := u.Query()
	cfg := pulsarConfig{
		Subscription:      firstQuery(q, "subscription", "subscription_name"),
		Token:             firstQuery(q, "token"),
		BatchSize:         defaultBatchSize,
		BatchTimeout:      defaultBatchTimeout,
		OperationTimeout:  defaultClientTimeout,
		ConnectionTimeout: defaultClientTimeout,
		TLSAllowInsecure:  parseBool(firstQuery(q, "tls_allow_insecure", "tls_allow_insecure_connection")),
		TLSValidateHost:   u.Scheme == "pulsar+ssl",
	}
	if cfg.Subscription == "" {
		cfg.Subscription = "ingestr"
	}
	if _, ok := q["tls_validate_hostname"]; ok {
		cfg.TLSValidateHost = parseBool(firstQuery(q, "tls_validate_hostname"))
	}
	if v := q.Get("batch_size"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed <= 0 {
			return pulsarConfig{}, fmt.Errorf("pulsar URI: batch_size must be positive")
		}
		cfg.BatchSize = parsed
	}
	if v := q.Get("batch_timeout"); v != "" {
		seconds, err := strconv.ParseFloat(v, 64)
		if err != nil || seconds <= 0 {
			return pulsarConfig{}, fmt.Errorf("pulsar URI: batch_timeout must be positive seconds")
		}
		cfg.BatchTimeout = time.Duration(seconds * float64(time.Second))
	}
	if v := q.Get("operation_timeout"); v != "" {
		parsed, err := parsePositiveSeconds(v, "operation_timeout")
		if err != nil {
			return pulsarConfig{}, err
		}
		cfg.OperationTimeout = parsed
	}
	if v := q.Get("connection_timeout"); v != "" {
		parsed, err := parsePositiveSeconds(v, "connection_timeout")
		if err != nil {
			return pulsarConfig{}, err
		}
		cfg.ConnectionTimeout = parsed
	}
	for _, key := range []string{"subscription", "subscription_name", "token", "batch_size", "batch_timeout", "operation_timeout", "connection_timeout", "tls_allow_insecure", "tls_allow_insecure_connection", "tls_validate_hostname"} {
		q.Del(key)
	}
	u.RawQuery = q.Encode()
	cfg.ServiceURL = u.String()
	if cfg.ServiceURL == "" || u.Host == "" {
		return pulsarConfig{}, fmt.Errorf("pulsar URI: host is required")
	}
	return cfg, nil
}

func parsePositiveSeconds(value, name string) (time.Duration, error) {
	seconds, err := strconv.ParseFloat(value, 64)
	if err != nil || seconds <= 0 {
		return 0, fmt.Errorf("pulsar URI: %s must be positive seconds", name)
	}
	return time.Duration(seconds * float64(time.Second)), nil
}

func firstQuery(values url.Values, keys ...string) string {
	for _, key := range keys {
		if v := values.Get(key); v != "" {
			return v
		}
	}
	return ""
}

func parseBool(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	return value == "1" || value == "true" || value == "yes"
}

func (s *PulsarSource) read(ctx context.Context, topic string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	if s.client == nil {
		return nil, fmt.Errorf("pulsar source is not connected")
	}
	if opts.Streaming {
		return s.readStreaming(ctx, topic, opts)
	}
	return s.readBatch(ctx, topic, opts)
}

func (s *PulsarSource) readBatch(ctx context.Context, topic string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	partitions, err := s.client.TopicPartitions(topic)
	if err != nil {
		return nil, fmt.Errorf("failed to get Pulsar topic partitions for %s: %w", topic, err)
	}
	readers := make([]pulsarBatchReader, 0, len(partitions))
	for _, partition := range partitions {
		reader, err := s.client.CreateReader(pulsargo.ReaderOptions{
			Topic:                   partition,
			StartMessageID:          pulsargo.EarliestMessageID(),
			StartMessageIDInclusive: true,
			ReceiverQueueSize:       s.batchSize(opts),
		})
		if err != nil {
			closePulsarBatchReaders(readers)
			return nil, fmt.Errorf("failed to create Pulsar reader for %s: %w", partition, err)
		}
		target, err := reader.GetLastMessageID()
		if err != nil {
			reader.Close()
			closePulsarBatchReaders(readers)
			return nil, fmt.Errorf("failed to get Pulsar last message id for %s: %w", partition, err)
		}
		if compareMessageID(target, pulsargo.EarliestMessageID()) <= 0 {
			reader.Close()
			continue
		}
		readers = append(readers, pulsarBatchReader{topic: partition, reader: reader, target: target})
		config.Debug("[PULSAR] Batch mode cutoff for %s: %s", partition, target.String())
	}
	if len(readers) == 0 {
		return closedResults(), nil
	}

	results := make(chan source.RecordBatchResult, 8)
	go func() {
		defer close(results)
		defer closePulsarBatchReaders(readers)
		batchSize := s.batchSize(opts)
		batch := make([]map[string]any, 0, batchSize)
		flush := func() bool {
			if len(batch) == 0 {
				return true
			}
			record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, nil, opts.ExcludeColumns)
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to convert Pulsar messages to Arrow: %w", err)}
				return false
			}
			results <- source.RecordBatchResult{Batch: record}
			batch = batch[:0]
			return true
		}
		for _, entry := range readers {
			for {
				if ctx.Err() != nil {
					flush()
					return
				}
				nextCtx, cancel := context.WithTimeout(ctx, s.cfg.BatchTimeout)
				msg, err := entry.reader.Next(nextCtx)
				timedOut := errors.Is(nextCtx.Err(), context.DeadlineExceeded)
				cancel()
				if err != nil {
					if ctx.Err() != nil {
						flush()
						return
					}
					flush()
					if timedOut {
						results <- source.RecordBatchResult{Err: fmt.Errorf("timed out reading Pulsar topic %s before cutoff %s", entry.topic, entry.target.String())}
					} else {
						results <- source.RecordBatchResult{Err: fmt.Errorf("failed to read Pulsar topic %s: %w", entry.topic, err)}
					}
					return
				}
				if compareMessageID(msg.ID(), entry.target) > 0 {
					break
				}
				batch = append(batch, messageToItem(msg))
				if len(batch) >= batchSize {
					if !flush() {
						return
					}
				}
				if compareMessageID(msg.ID(), entry.target) >= 0 {
					break
				}
			}
		}
		flush()
	}()
	return results, nil
}

func (s *PulsarSource) readStreaming(ctx context.Context, topic string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	consumer, err := s.client.Subscribe(pulsargo.ConsumerOptions{
		Topic:                       topic,
		SubscriptionName:            s.cfg.Subscription,
		Type:                        pulsargo.Shared,
		SubscriptionInitialPosition: pulsargo.SubscriptionPositionEarliest,
		ReceiverQueueSize:           s.batchSize(opts),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to Pulsar topic %s: %w", topic, err)
	}
	s.mu.Lock()
	s.streamConsumer = consumer
	s.nextSeq = 0
	s.pending = nil
	s.mu.Unlock()

	results := make(chan source.RecordBatchResult, 8)
	go func() {
		defer close(results)
		batchSize := s.batchSize(opts)
		batch := make([]map[string]any, 0, batchSize)
		var maxSeq int64
		flush := func() bool {
			if len(batch) == 0 {
				return true
			}
			record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, streamingEnvelopeSchema(tableName(topic)).Columns, opts.ExcludeColumns)
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to convert Pulsar messages to Arrow: %w", err)}
				return false
			}
			results <- source.RecordBatchResult{Batch: record, CommitToken: pulsarCommitToken{MaxSeq: maxSeq}}
			batch = batch[:0]
			return true
		}
		for {
			if ctx.Err() != nil {
				flush()
				return
			}
			receiveCtx, cancel := context.WithTimeout(ctx, s.cfg.BatchTimeout)
			msg, err := consumer.Receive(receiveCtx)
			timedOut := errors.Is(receiveCtx.Err(), context.DeadlineExceeded)
			cancel()
			if err != nil {
				if ctx.Err() != nil {
					flush()
					return
				}
				if !timedOut {
					flush()
					results <- source.RecordBatchResult{Err: fmt.Errorf("failed to receive Pulsar message from %s: %w", topic, err)}
					return
				}
				if !flush() {
					return
				}
				continue
			}
			seq := s.trackPending(msg.ID())
			maxSeq = seq
			batch = append(batch, messageToEnvelope(msg, seq))
			if len(batch) >= batchSize {
				if !flush() {
					return
				}
			}
		}
	}()
	return results, nil
}

type pulsarBatchReader struct {
	topic  string
	reader pulsargo.Reader
	target pulsargo.MessageID
}

func closePulsarBatchReaders(readers []pulsarBatchReader) {
	for _, entry := range readers {
		entry.reader.Close()
	}
}

func (s *PulsarSource) batchSize(opts source.ReadOptions) int {
	if opts.PageSize > 0 {
		return opts.PageSize
	}
	if s.cfg.BatchSize > 0 {
		return s.cfg.BatchSize
	}
	return defaultBatchSize
}

func (s *PulsarSource) trackPending(id pulsargo.MessageID) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextSeq++
	seq := s.nextSeq
	s.pending = append(s.pending, trackedMessageID{seq: seq, id: id})
	return seq
}

func (s *PulsarSource) CommitStream(_ context.Context, token any) error {
	tok, ok := token.(pulsarCommitToken)
	if !ok {
		return fmt.Errorf("pulsar: unexpected commit token type %T", token)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.streamConsumer == nil {
		config.Debug("[PULSAR] CommitStream: no active consumer, skipping ack")
		return nil
	}
	remaining := s.pending[:0]
	for i, pending := range s.pending {
		if pending.seq <= tok.MaxSeq {
			if err := s.streamConsumer.AckID(pending.id); err != nil {
				remaining = append(remaining, pending)
				remaining = append(remaining, s.pending[i+1:]...)
				s.pending = remaining
				return fmt.Errorf("failed to ack Pulsar message: %w", err)
			}
		} else {
			remaining = append(remaining, pending)
		}
	}
	s.pending = remaining
	return nil
}

func closedResults() <-chan source.RecordBatchResult {
	ch := make(chan source.RecordBatchResult)
	close(ch)
	return ch
}

func messageToItem(msg pulsargo.Message) map[string]any {
	msgID := digest128(msg.Topic() + ":" + msg.ID().String())
	item := map[string]any{
		"pulsar_msg_id": msgID,
		"data":          decodeMaybeJSON(msg.Payload()),
		"pulsar": map[string]any{
			"topic":          msg.Topic(),
			"message_id":     msg.ID().String(),
			"key":            msg.Key(),
			"ordering_key":   msg.OrderingKey(),
			"producer":       msg.ProducerName(),
			"publish_time":   msg.PublishTime().UTC().Format(time.RFC3339Nano),
			"event_time":     formatOptionalTime(msg.EventTime()),
			"properties":     msg.Properties(),
			"redelivery_cnt": msg.RedeliveryCount(),
		},
	}
	return item
}

func messageToEnvelope(msg pulsargo.Message, seq int64) map[string]any {
	item := messageToItem(msg)
	msgID, _ := item["pulsar_msg_id"].(string)
	delete(item, "pulsar_msg_id")
	encoded, err := json.Marshal(item)
	if err != nil {
		encoded = []byte("null")
	}
	return map[string]any{
		"msg_id":          msgID,
		"data":            string(encoded),
		streamOrderColumn: seq,
	}
}

func decodeMaybeJSON(data []byte) any {
	var decoded any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&decoded); err == nil {
		return decoded
	}
	return string(data)
}

func formatOptionalTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func compareMessageID(a, b pulsargo.MessageID) int {
	if a.PartitionIdx() >= 0 && b.PartitionIdx() >= 0 && a.PartitionIdx() < b.PartitionIdx() {
		return -1
	}
	if a.PartitionIdx() >= 0 && b.PartitionIdx() >= 0 && a.PartitionIdx() > b.PartitionIdx() {
		return 1
	}
	if a.LedgerID() < b.LedgerID() {
		return -1
	}
	if a.LedgerID() > b.LedgerID() {
		return 1
	}
	if a.EntryID() < b.EntryID() {
		return -1
	}
	if a.EntryID() > b.EntryID() {
		return 1
	}
	if a.BatchIdx() < 0 && b.BatchIdx() < 0 {
		return 0
	}
	if a.BatchIdx() >= 0 && b.BatchIdx() < 0 {
		return -1
	}
	if a.BatchIdx() < 0 && b.BatchIdx() >= 0 {
		return 1
	}
	if a.BatchIdx() < b.BatchIdx() {
		return -1
	}
	if a.BatchIdx() > b.BatchIdx() {
		return 1
	}
	return 0
}

func tableName(topic string) string {
	parts := strings.Split(strings.Trim(topic, "/"), "/")
	if len(parts) == 0 {
		return topic
	}
	name := parts[len(parts)-1]
	if name == "" {
		return topic
	}
	return name
}

func digest128(value string) string {
	sum := sha256.Sum256([]byte(value))
	return strings.TrimRight(base64.StdEncoding.EncodeToString(sum[:15]), "=")
}

var (
	_ source.Source          = (*PulsarSource)(nil)
	_ source.StreamingSource = (*PulsarSource)(nil)
	_ source.StreamCommitter = (*PulsarSource)(nil)
)
