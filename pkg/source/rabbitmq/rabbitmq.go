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
}

func NewRabbitMQSource() *RabbitMQSource {
	return &RabbitMQSource{}
}

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

	if err := ch.Qos(batchSize, 0, false); err != nil {
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

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)
		defer func() { _ = ch.Close() }()

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

		flush := func() bool {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, nil, opts.ExcludeColumns)
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to convert RabbitMQ messages to Arrow: %w", err)}
				return false
			}

			results <- source.RecordBatchResult{Batch: record}

			if err := ch.Ack(lastDeliveryTag, true); err != nil {
				config.Debug("[RABBITMQ] Failed to ack messages up to tag %d: %v", lastDeliveryTag, err)
			}

			batchNum++
			config.Debug("[RABBITMQ] Batch %d: %d messages (total: %d)", batchNum, len(batch), totalRead)
			return true
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

				batch = append(batch, deliveryToItem(msg))
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
				if !receivedSinceLastTick {
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

var _ source.Source = (*RabbitMQSource)(nil)
