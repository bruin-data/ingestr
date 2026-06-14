package rabbitmq

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	defaultBatchSize    = 3000
	defaultBatchTimeout = 5 * time.Second
)

type RabbitMQSource struct {
	conn *amqp.Connection

	// streamCh holds the live consumer channel in streaming mode so CommitStream
	// can acknowledge deliveries after the pipeline has flushed them.
	mu       sync.Mutex
	streamCh *amqp.Channel
}

func NewRabbitMQSource() *RabbitMQSource {
	return &RabbitMQSource{}
}

// SupportsStreaming reports that RabbitMQ can be consumed continuously.
func (s *RabbitMQSource) SupportsStreaming() bool {
	return true
}

// DefaultStreamingStrategy returns merge keyed on the envelope msg_id. Brokers
// deliver at-least-once, so the same message (same msg_id) can be redelivered
// after a restart; merge makes that idempotent (effectively-once) instead of
// piling up duplicate rows or violating the msg_id primary key.
func (s *RabbitMQSource) DefaultStreamingStrategy() config.IncrementalStrategy {
	return config.StrategyMerge
}

// CommitStream acknowledges all deliveries up to and including the given tag.
// The pipeline calls this only after the batch carrying the tag has been
// durably written, giving at-least-once delivery.
func (s *RabbitMQSource) CommitStream(_ context.Context, token any) error {
	tag, ok := token.(uint64)
	if !ok {
		return fmt.Errorf("rabbitmq: unexpected commit token type %T", token)
	}
	// Hold the lock across Ack so it cannot race with the consumer goroutine
	// clearing and closing the channel on shutdown (both guarded by s.mu).
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.streamCh == nil {
		return fmt.Errorf("rabbitmq: no active streaming consumer to acknowledge")
	}
	if err := s.streamCh.Ack(tag, true); err != nil {
		return fmt.Errorf("rabbitmq: failed to ack up to delivery tag %d: %w", tag, err)
	}
	return nil
}

// streamingEnvelopeSchema is the fixed schema used for schema-less broker
// sources in streaming mode: a primary-key message id plus a JSON column
// holding the decoded body and metadata.
func streamingEnvelopeSchema(queue string) *schema.TableSchema {
	return &schema.TableSchema{
		Name: queue,
		Columns: []schema.Column{
			{Name: "msg_id", DataType: schema.TypeString, Nullable: false, IsPrimaryKey: true},
			{Name: "data", DataType: schema.TypeJSON, Nullable: true},
			// Monotonic per-consumer source position (AMQP delivery tag). Used as
			// the merge incremental key so the latest record per msg_id wins
			// within a flush cycle.
			{Name: streamOrderColumn, DataType: schema.TypeInt64, Nullable: false},
		},
		PrimaryKeys:    []string{"msg_id"},
		IncrementalKey: streamOrderColumn,
	}
}

const streamOrderColumn = "_ingestr_order"

func (s *RabbitMQSource) Schemes() []string {
	return []string{"amqp", "amqps"}
}

func (s *RabbitMQSource) Connect(ctx context.Context, uri string) error {
	conn, err := amqp.Dial(uri)
	if err != nil {
		return fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	s.conn = conn
	config.Debug("[RABBITMQ] Connected to %s", sanitizeURI(uri))
	return nil
}

func (s *RabbitMQSource) Close(_ context.Context) error {
	if s.conn != nil {
		err := s.conn.Close()
		if err != nil {
			var amqpErr *amqp.Error
			if errors.As(err, &amqpErr) && amqpErr.Code == amqp.ChannelError {
				return nil
			}
			return err
		}
	}
	return nil
}

func (s *RabbitMQSource) HandlesIncrementality() bool {
	return false
}

func (s *RabbitMQSource) GetTable(_ context.Context, req source.TableRequest) (source.SourceTable, error) {
	queueName := req.Name
	if queueName == "" {
		return nil, fmt.Errorf("rabbitmq source requires a queue name as source-table")
	}

	strategy := req.Strategy
	if strategy == "" {
		strategy = config.StrategyReplace
	}

	// In streaming mode the queue has no inferable schema (the stream never
	// ends), so we project every message into a fixed msg_id + data envelope.
	if req.Streaming {
		primaryKeys := req.PrimaryKeys
		if len(primaryKeys) == 0 {
			primaryKeys = []string{"msg_id"}
		}
		return &source.DynamicSourceTable{
			TableName:           queueName,
			TablePrimaryKeys:    primaryKeys,
			TableIncrementalKey: req.IncrementalKey,
			TableStrategy:       strategy,
			KnownSchema:         true,
			SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
				return streamingEnvelopeSchema(queueName), nil
			},
			ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
				return s.read(ctx, queueName, opts)
			},
		}, nil
	}

	return &source.DynamicSourceTable{
		TableName:           queueName,
		TablePrimaryKeys:    req.PrimaryKeys,
		TableIncrementalKey: req.IncrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("rabbitmq source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, queueName, opts)
		},
	}, nil
}

func (s *RabbitMQSource) read(ctx context.Context, queue string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	ch, err := s.conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("failed to open channel: %w", err)
	}

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}

	streaming := opts.Streaming

	// In streaming mode the pipeline acks (via CommitStream) only after a flush,
	// so the un-acked window can exceed any prefetch limit. Unlimited prefetch
	// (0) avoids stalling the consumer; backpressure comes from the bounded
	// results channel. Non-streaming acks per batch, so batchSize prefetch is fine.
	prefetch := batchSize
	if streaming {
		prefetch = 0
	}
	if err := ch.Qos(prefetch, 0, false); err != nil {
		_ = ch.Close()
		return nil, fmt.Errorf("failed to set QoS: %w", err)
	}

	msgs, err := ch.Consume(
		queue,
		"",    // consumer tag (auto-generated)
		false, // auto-ack (manual so we can ack after processing)
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,   // args
	)
	if err != nil {
		_ = ch.Close()
		return nil, fmt.Errorf("failed to start consuming from queue %s: %w", queue, err)
	}

	if streaming {
		s.mu.Lock()
		s.streamCh = ch
		s.mu.Unlock()
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)
		if streaming {
			// Clear and close the channel under the lock so a concurrent
			// CommitStream (which acks under the same lock) never touches a
			// closing channel.
			defer func() {
				s.mu.Lock()
				s.streamCh = nil
				_ = ch.Close()
				s.mu.Unlock()
			}()
		} else {
			defer func() { _ = ch.Close() }()
		}

		var envelopeCols []schema.Column
		if streaming {
			envelopeCols = streamingEnvelopeSchema(queue).Columns
		}

		batch := make([]map[string]any, 0, batchSize)
		var lastDeliveryTag uint64
		totalRead := int64(0)
		batchNum := 0
		limit := int64(opts.Limit)
		receivedSinceLastTick := false

		timer := time.NewTimer(defaultBatchTimeout)
		defer timer.Stop()

		resetTimer := func() {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(defaultBatchTimeout)
		}

		// flush emits the accumulated batch. In streaming mode it attaches the
		// highest delivery tag as a cumulative CommitToken and does NOT ack;
		// the pipeline acks via CommitStream after the data is durable. In
		// non-streaming mode it acks immediately after emitting.
		flush := func() bool {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, envelopeCols, opts.ExcludeColumns)
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to convert RabbitMQ messages to Arrow: %w", err)}
				return false
			}

			res := source.RecordBatchResult{Batch: record}
			if streaming {
				res.CommitToken = lastDeliveryTag
			}
			results <- res

			if !streaming {
				if err := ch.Ack(lastDeliveryTag, true); err != nil {
					config.Debug("[RABBITMQ] Failed to ack messages up to tag %d: %v", lastDeliveryTag, err)
				}
			}

			batchNum++
			config.Debug("[RABBITMQ] Batch %d: %d messages (total: %d)", batchNum, len(batch), totalRead)
			return true
		}

		appendMsg := deliveryToItem
		if streaming {
			appendMsg = deliveryToEnvelope
		}

		for {
			select {
			case <-ctx.Done():
				if len(batch) > 0 {
					flush()
				}
				return

			case msg, ok := <-msgs:
				if !ok {
					if len(batch) > 0 {
						flush()
					}
					return
				}

				batch = append(batch, appendMsg(msg))
				lastDeliveryTag = msg.DeliveryTag
				totalRead++
				receivedSinceLastTick = true

				if limit > 0 && totalRead >= limit {
					flush()
					return
				}

				if len(batch) >= batchSize {
					if !flush() {
						return
					}
					batch = batch[:0]
					resetTimer()
					receivedSinceLastTick = false
				}

			case <-timer.C:
				if len(batch) > 0 {
					if !flush() {
						return
					}
					batch = batch[:0]
					timer.Reset(defaultBatchTimeout)
					receivedSinceLastTick = false
					continue
				}
				// In streaming mode never exit on idle; keep consuming.
				if !streaming && !receivedSinceLastTick {
					config.Debug("[RABBITMQ] No new messages received, finishing read (total: %d)", totalRead)
					return
				}
				receivedSinceLastTick = false
				timer.Reset(defaultBatchTimeout)
			}
		}
	}()

	return results, nil
}

func deliveryToItem(msg amqp.Delivery) map[string]any {
	var data any
	decoder := json.NewDecoder(bytes.NewReader(msg.Body))
	decoder.UseNumber()
	if err := decoder.Decode(&data); err != nil {
		data = string(msg.Body)
	}

	var headers map[string]any
	if len(msg.Headers) > 0 {
		headers = make(map[string]any, len(msg.Headers))
		for k, v := range msg.Headers {
			headers[k] = fmt.Sprintf("%v", v)
		}
	}

	ts := msg.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}

	item := map[string]any{
		"data": data,
		"metadata": map[string]any{
			"exchange":     msg.Exchange,
			"routing_key":  msg.RoutingKey,
			"content_type": msg.ContentType,
			"delivery_tag": msg.DeliveryTag,
			"message_id":   msg.MessageId,
			"timestamp":    ts.Format(time.RFC3339Nano),
			"headers":      headers,
		},
		"msg_id": generateMsgID(msg),
	}

	return item
}

// deliveryToEnvelope projects a message into the streaming envelope schema:
// a primary-key msg_id and a JSON data column holding the decoded body and
// metadata (everything deliveryToItem would have produced, minus msg_id).
func deliveryToEnvelope(msg amqp.Delivery) map[string]any {
	item := deliveryToItem(msg)
	msgID, _ := item["msg_id"].(string)
	delete(item, "msg_id")
	encoded, err := json.Marshal(item)
	if err != nil {
		encoded = fmt.Appendf(nil, "%q", string(msg.Body))
	}
	return map[string]any{
		"msg_id":          msgID,
		"data":            string(encoded),
		streamOrderColumn: int64(msg.DeliveryTag),
	}
}

func generateMsgID(msg amqp.Delivery) string {
	if msg.MessageId != "" {
		return msg.MessageId
	}
	h := sha256.New()
	h.Write([]byte(msg.Exchange))
	h.Write([]byte(msg.RoutingKey))
	h.Write(msg.Body)
	_, _ = fmt.Fprintf(h, "%d", msg.DeliveryTag)
	sum := h.Sum(nil)
	return strings.TrimRight(base64.StdEncoding.EncodeToString(sum[:15]), "=")
}

func sanitizeURI(uri string) string {
	if _, after, found := strings.Cut(uri, "@"); found {
		scheme := "amqp"
		if before, _, hasScheme := strings.Cut(uri, "://"); hasScheme {
			scheme = before
		}
		return scheme + "://***@" + after
	}
	return uri
}

var (
	_ source.Source          = (*RabbitMQSource)(nil)
	_ source.StreamingSource = (*RabbitMQSource)(nil)
	_ source.StreamCommitter = (*RabbitMQSource)(nil)
)
