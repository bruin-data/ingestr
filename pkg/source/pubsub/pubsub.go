package pubsub

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	pubsubv1 "cloud.google.com/go/pubsub/apiv1"
	"cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

const (
	defaultBatchSize          = 3000
	defaultBatchTimeout       = 5 * time.Second
	defaultPullTimeout        = 2 * time.Second
	defaultAckDeadlineSeconds = int32(300)
	maxPullMessages           = 1000
	maxAckIDs                 = 1000
	streamOrderColumn         = "_ingestr_order"
)

type pubSubConfig struct {
	ProjectID          string
	Endpoint           string
	CredentialsFile    string
	CredentialsJSON    string
	PullTimeout        time.Duration
	AckDeadlineSeconds int32
}

type PubSubSource struct {
	cfg        pubSubConfig
	subscriber *pubsubv1.SubscriberClient

	mu                 sync.Mutex
	streamSubscription string
	nextSeq            int64
	pending            map[int64]string
}

type pubSubCommitToken struct {
	MaxSeq int64
}

type pubSubAck struct {
	seq   int64
	ackID string
}

func NewPubSubSource() *PubSubSource {
	return &PubSubSource{
		pending: make(map[int64]string),
	}
}

func (s *PubSubSource) Schemes() []string {
	return []string{"pubsub"}
}

func (s *PubSubSource) HandlesIncrementality() bool {
	return false
}

func (s *PubSubSource) SupportsStreaming() bool {
	return true
}

func (s *PubSubSource) DefaultStreamingStrategy() config.IncrementalStrategy {
	return config.StrategyMerge
}

func (s *PubSubSource) Connect(ctx context.Context, uri string) error {
	cfg, err := parsePubSubURI(uri)
	if err != nil {
		return err
	}
	s.cfg = cfg

	opts := make([]option.ClientOption, 0, 4)
	if cfg.Endpoint != "" {
		opts = append(opts, option.WithEndpoint(cfg.Endpoint))
		if cfg.CredentialsFile == "" && cfg.CredentialsJSON == "" {
			opts = append(
				opts,
				option.WithoutAuthentication(),
				option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
			)
		}
	}
	if cfg.CredentialsFile != "" {
		opts = append(opts, option.WithAuthCredentialsFile(option.ServiceAccount, cfg.CredentialsFile))
	} else if cfg.CredentialsJSON != "" {
		opts = append(opts, option.WithAuthCredentialsJSON(option.ServiceAccount, []byte(cfg.CredentialsJSON)))
	}

	subscriber, err := pubsubv1.NewSubscriberClient(ctx, opts...)
	if err != nil {
		return fmt.Errorf("failed to create Pub/Sub subscriber client: %w", err)
	}
	s.subscriber = subscriber

	config.Debug("[PUBSUB] Connected to project %s", cfg.ProjectID)
	return nil
}

func (s *PubSubSource) Close(_ context.Context) error {
	if s.subscriber != nil {
		return s.subscriber.Close()
	}
	return nil
}

func (s *PubSubSource) GetTable(_ context.Context, req source.TableRequest) (source.SourceTable, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("pubsub source requires a subscription name as source-table")
	}

	subscription := subscriptionName(s.cfg.ProjectID, req.Name)
	tableName := pubSubTableName(req.Name)
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
			TableName:           tableName,
			TablePrimaryKeys:    primaryKeys,
			TableIncrementalKey: streamOrderColumn,
			TableStrategy:       streamStrategy,
			KnownSchema:         true,
			SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
				return streamingEnvelopeSchema(tableName), nil
			},
			ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
				return s.read(ctx, tableName, subscription, opts)
			},
		}, nil
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    req.PrimaryKeys,
		TableIncrementalKey: req.IncrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("Pub/Sub source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, subscription, opts)
		},
	}, nil
}

func streamingEnvelopeSchema(subscription string) *schema.TableSchema {
	return &schema.TableSchema{
		Name: subscription,
		Columns: []schema.Column{
			{Name: "msg_id", DataType: schema.TypeString, Nullable: false, IsPrimaryKey: true},
			{Name: "data", DataType: schema.TypeJSON, Nullable: true},
			{Name: streamOrderColumn, DataType: schema.TypeInt64, Nullable: false},
		},
		PrimaryKeys:    []string{"msg_id"},
		IncrementalKey: streamOrderColumn,
	}
}

func parsePubSubURI(raw string) (pubSubConfig, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return pubSubConfig{}, fmt.Errorf("invalid pubsub URI: %w", err)
	}
	if u.Scheme != "pubsub" {
		return pubSubConfig{}, fmt.Errorf("invalid pubsub URI: must start with pubsub://")
	}

	projectID := strings.Trim(strings.TrimPrefix(u.Host+u.Path, "/"), "/")
	if projectID == "" {
		return pubSubConfig{}, fmt.Errorf("pubsub URI: project id is required")
	}

	q := u.Query()
	cfg := pubSubConfig{
		ProjectID:          projectID,
		Endpoint:           normalizeEndpoint(firstQuery(q, "endpoint", "endpoint_url", "emulator_host", "pubsub_emulator_host")),
		CredentialsFile:    firstQuery(q, "credentials_file", "credentials_path", "credentials"),
		PullTimeout:        defaultPullTimeout,
		AckDeadlineSeconds: defaultAckDeadlineSeconds,
	}
	if v := firstQuery(q, "credentials_base64"); v != "" {
		decoded, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			return pubSubConfig{}, fmt.Errorf("pubsub URI: failed to decode credentials_base64: %w", err)
		}
		cfg.CredentialsJSON = string(decoded)
	}
	if v := firstQuery(q, "pull_timeout_seconds", "pull_timeout"); v != "" {
		seconds, err := strconv.ParseFloat(v, 64)
		if err != nil || seconds <= 0 {
			return pubSubConfig{}, fmt.Errorf("pubsub URI: pull_timeout_seconds must be positive")
		}
		cfg.PullTimeout = time.Duration(seconds * float64(time.Second))
	}
	if v := firstQuery(q, "ack_deadline_seconds", "visibility_timeout"); v != "" {
		seconds, err := strconv.ParseInt(v, 10, 32)
		if err != nil || seconds <= 0 {
			return pubSubConfig{}, fmt.Errorf("pubsub URI: ack_deadline_seconds must be positive")
		}
		cfg.AckDeadlineSeconds = int32(seconds)
	}
	return cfg, nil
}

func firstQuery(values url.Values, keys ...string) string {
	for _, key := range keys {
		if v := values.Get(key); v != "" {
			return v
		}
	}
	return ""
}

func normalizeEndpoint(endpoint string) string {
	if endpoint == "" {
		return ""
	}
	if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
		return u.Host
	}
	return endpoint
}

func subscriptionName(projectID, name string) string {
	if strings.HasPrefix(name, "projects/") {
		return name
	}
	return fmt.Sprintf("projects/%s/subscriptions/%s", projectID, name)
}

func pubSubTableName(name string) string {
	parts := strings.Split(strings.Trim(name, "/"), "/")
	return parts[len(parts)-1]
}

func (s *PubSubSource) read(ctx context.Context, tableName, subscription string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}

	streaming := opts.Streaming
	if streaming {
		s.mu.Lock()
		s.streamSubscription = subscription
		s.nextSeq = 0
		s.pending = make(map[int64]string)
		s.mu.Unlock()
	}

	results := make(chan source.RecordBatchResult, 8)
	go func() {
		defer close(results)

		var ackDeadlineErrs <-chan error
		var stopAckDeadline context.CancelFunc
		if streaming {
			ackDeadlineCtx, cancel := context.WithCancel(ctx)
			stopAckDeadline = cancel
			errs := make(chan error, 1)
			ackDeadlineErrs = errs
			go s.extendAckDeadlineLoop(ackDeadlineCtx, subscription, errs)
		}
		defer func() {
			if stopAckDeadline != nil {
				stopAckDeadline()
			}
		}()

		var envelopeCols []schema.Column
		if streaming {
			envelopeCols = streamingEnvelopeSchema(tableName).Columns
		}

		batch := make([]map[string]any, 0, batchSize)
		batchAcks := make([]pubSubAck, 0, batchSize)
		totalRead := int64(0)
		limit := int64(opts.Limit)
		lastFlush := time.Now()

		flush := func() bool {
			if len(batch) == 0 {
				return true
			}
			record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, envelopeCols, opts.ExcludeColumns)
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to convert Pub/Sub messages to Arrow: %w", err)}
				return false
			}
			res := source.RecordBatchResult{Batch: record}
			if streaming {
				res.CommitToken = pubSubCommitToken{MaxSeq: batchAcks[len(batchAcks)-1].seq}
			}
			results <- res

			if !streaming {
				if _, err := s.ackMessages(ctx, subscription, batchAcks); err != nil {
					results <- source.RecordBatchResult{Err: err}
					return false
				}
			}

			batch = batch[:0]
			batchAcks = batchAcks[:0]
			lastFlush = time.Now()
			return true
		}

		for {
			if ctx.Err() != nil {
				flush()
				return
			}
			select {
			case err := <-ackDeadlineErrs:
				results <- source.RecordBatchResult{Err: err}
				return
			default:
			}

			maxMessages := maxPullMessages
			if remaining := batchSize - len(batch); remaining > 0 && remaining < maxMessages {
				maxMessages = remaining
			}

			pullCtx, cancel := context.WithTimeout(ctx, s.cfg.PullTimeout)
			resp, err := s.subscriber.Pull(pullCtx, &pubsubpb.PullRequest{
				Subscription: subscription,
				MaxMessages:  int32(maxMessages),
			})
			cancel()
			if err != nil {
				if ctx.Err() != nil {
					flush()
					return
				}
				if status.Code(err) == codes.DeadlineExceeded {
					if !flush() {
						return
					}
					if !streaming {
						return
					}
					continue
				}
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to pull Pub/Sub messages: %w", err)}
				return
			}

			if len(resp.ReceivedMessages) == 0 {
				if !flush() {
					return
				}
				if !streaming {
					return
				}
				continue
			}

			if streaming {
				ackIDs := make([]string, 0, len(resp.ReceivedMessages))
				for _, msg := range resp.ReceivedMessages {
					ackIDs = append(ackIDs, msg.GetAckId())
				}
				if err := s.extendAckDeadline(ctx, subscription, ackIDs); err != nil {
					results <- source.RecordBatchResult{Err: err}
					return
				}
			}

			for _, msg := range resp.ReceivedMessages {
				ackID := msg.GetAckId()
				var seq int64
				if streaming {
					seq = s.trackAckID(ackID)
					batch = append(batch, messageToEnvelope(msg, subscription, seq))
				} else {
					batch = append(batch, messageToItem(msg, subscription))
				}
				batchAcks = append(batchAcks, pubSubAck{seq: seq, ackID: ackID})
				totalRead++

				if limit > 0 && totalRead >= limit {
					flush()
					return
				}
				if len(batch) >= batchSize {
					if !flush() {
						return
					}
				}
			}
			if streaming && len(batch) > 0 && time.Since(lastFlush) >= defaultBatchTimeout {
				if !flush() {
					return
				}
			}
		}
	}()

	return results, nil
}

func (s *PubSubSource) extendAckDeadline(ctx context.Context, subscription string, ackIDs []string) error {
	if len(ackIDs) == 0 {
		return nil
	}
	deadlineSeconds := s.ackDeadlineSeconds()
	for start := 0; start < len(ackIDs); start += maxAckIDs {
		end := start + maxAckIDs
		if end > len(ackIDs) {
			end = len(ackIDs)
		}
		if err := s.subscriber.ModifyAckDeadline(ctx, &pubsubpb.ModifyAckDeadlineRequest{
			Subscription:       subscription,
			AckIds:             ackIDs[start:end],
			AckDeadlineSeconds: deadlineSeconds,
		}); err != nil {
			return fmt.Errorf("failed to extend Pub/Sub ack deadline: %w", err)
		}
	}
	return nil
}

func (s *PubSubSource) extendAckDeadlineLoop(ctx context.Context, subscription string, errs chan<- error) {
	ticker := time.NewTicker(s.ackDeadlineExtensionInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			acks, activeSubscription := s.pendingSnapshot()
			if len(acks) == 0 {
				continue
			}
			if activeSubscription != "" {
				subscription = activeSubscription
			}
			if subscription == "" {
				continue
			}
			if err := s.extendAckDeadline(ctx, subscription, ackIDs(acks)); err != nil {
				if ctx.Err() != nil {
					return
				}
				select {
				case errs <- err:
				default:
				}
				return
			}
		}
	}
}

func (s *PubSubSource) ackDeadlineSeconds() int32 {
	if s.cfg.AckDeadlineSeconds > 0 {
		return s.cfg.AckDeadlineSeconds
	}
	return defaultAckDeadlineSeconds
}

func (s *PubSubSource) ackDeadlineExtensionInterval() time.Duration {
	interval := time.Duration(s.ackDeadlineSeconds()) * time.Second / 2
	if interval < time.Second {
		return time.Second
	}
	return interval
}

func (s *PubSubSource) trackAckID(ackID string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextSeq++
	s.pending[s.nextSeq] = ackID
	return s.nextSeq
}

func (s *PubSubSource) CommitStream(ctx context.Context, token any) error {
	tok, ok := token.(pubSubCommitToken)
	if !ok {
		return fmt.Errorf("pubsub: unexpected commit token type %T", token)
	}

	acks, subscription := s.pendingThrough(tok.MaxSeq)
	if len(acks) == 0 {
		return nil
	}
	if subscription == "" {
		config.Debug("[PUBSUB] CommitStream: no active subscription, skipping ack")
		return nil
	}

	committed, err := s.ackMessages(ctx, subscription, acks)
	if len(committed) > 0 {
		s.removePending(committed)
	}
	return err
}

func (s *PubSubSource) pendingThrough(maxSeq int64) ([]pubSubAck, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	acks := make([]pubSubAck, 0)
	for seq, ackID := range s.pending {
		if seq <= maxSeq {
			acks = append(acks, pubSubAck{seq: seq, ackID: ackID})
		}
	}
	return acks, s.streamSubscription
}

func (s *PubSubSource) pendingSnapshot() ([]pubSubAck, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	acks := make([]pubSubAck, 0, len(s.pending))
	for seq, ackID := range s.pending {
		acks = append(acks, pubSubAck{seq: seq, ackID: ackID})
	}
	return acks, s.streamSubscription
}

func (s *PubSubSource) removePending(acks []pubSubAck) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ack := range acks {
		delete(s.pending, ack.seq)
	}
}

func (s *PubSubSource) ackMessages(ctx context.Context, subscription string, acks []pubSubAck) ([]pubSubAck, error) {
	committed := make([]pubSubAck, 0, len(acks))
	for start := 0; start < len(acks); start += maxAckIDs {
		end := start + maxAckIDs
		if end > len(acks) {
			end = len(acks)
		}
		chunk := acks[start:end]
		ackIDs := make([]string, 0, len(chunk))
		for _, ack := range chunk {
			ackIDs = append(ackIDs, ack.ackID)
		}
		if err := s.subscriber.Acknowledge(ctx, &pubsubpb.AcknowledgeRequest{
			Subscription: subscription,
			AckIds:       ackIDs,
		}); err != nil {
			return committed, fmt.Errorf("failed to acknowledge Pub/Sub messages: %w", err)
		}
		committed = append(committed, chunk...)
	}
	return committed, nil
}

func ackIDs(acks []pubSubAck) []string {
	ids := make([]string, 0, len(acks))
	for _, ack := range acks {
		ids = append(ids, ack.ackID)
	}
	return ids
}

func messageToItem(msg *pubsubpb.ReceivedMessage, subscription string) map[string]any {
	pubsubMsg := msg.GetMessage()
	dataBytes := pubsubMsg.GetData()

	var data any
	decoder := json.NewDecoder(bytes.NewReader(dataBytes))
	decoder.UseNumber()
	if err := decoder.Decode(&data); err != nil {
		data = string(dataBytes)
	}

	msgID := pubsubMsg.GetMessageId()
	if msgID == "" {
		msgID = digest128(subscription + string(dataBytes))
	}

	var publishTime string
	if pubsubMsg.GetPublishTime() != nil {
		publishTime = pubsubMsg.GetPublishTime().AsTime().UTC().Format(time.RFC3339Nano)
	}

	return map[string]any{
		"data": data,
		"metadata": map[string]any{
			"subscription": subscription,
			"message_id":   msgID,
			"publish_time": publishTime,
			"ordering_key": pubsubMsg.GetOrderingKey(),
			"attributes":   pubsubMsg.GetAttributes(),
		},
		"msg_id": msgID,
	}
}

func messageToEnvelope(msg *pubsubpb.ReceivedMessage, subscription string, seq int64) map[string]any {
	item := messageToItem(msg, subscription)
	msgID, _ := item["msg_id"].(string)
	delete(item, "msg_id")
	encoded, err := json.Marshal(item)
	if err != nil {
		encoded = jsonStringFallback(string(msg.GetMessage().GetData()))
	}
	return map[string]any{
		"msg_id":          msgID,
		"data":            string(encoded),
		streamOrderColumn: seq,
	}
}

func jsonStringFallback(value string) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		return []byte("null")
	}
	return encoded
}

func digest128(v string) string {
	sum := sha256.Sum256([]byte(v))
	return strings.TrimRight(base64.StdEncoding.EncodeToString(sum[:15]), "=")
}

var (
	_ source.Source          = (*PubSubSource)(nil)
	_ source.StreamingSource = (*PubSubSource)(nil)
	_ source.StreamCommitter = (*PubSubSource)(nil)
)
