package redis

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

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	goredis "github.com/redis/go-redis/v9"
)

const (
	defaultBatchSize     = 3000
	defaultBatchTimeout  = 5 * time.Second
	defaultFlushInterval = 30 * time.Second
	defaultGroup         = "ingestr"
	defaultConsumer      = "ingestr"
	streamOrderColumn    = "_ingestr_order"
)

type redisConfig struct {
	RawURL       string
	Group        string
	Consumer     string
	BatchSize    int
	BatchTimeout time.Duration
	ClaimMinIdle time.Duration
}

type redisCommitToken struct {
	Stream string
	MaxID  string
}

type RedisSource struct {
	cfg    redisConfig
	client *goredis.Client

	mu      sync.Mutex
	pending map[string]struct{}
}

func NewRedisSource() *RedisSource {
	return &RedisSource{pending: make(map[string]struct{})}
}

func (s *RedisSource) Schemes() []string {
	return []string{"redis", "rediss"}
}

func (s *RedisSource) Connect(ctx context.Context, raw string) error {
	cfg, redisURL, err := parseRedisURI(raw)
	if err != nil {
		return err
	}
	opts, err := goredis.ParseURL(redisURL)
	if err != nil {
		return fmt.Errorf("invalid Redis URI: %w", err)
	}
	client := goredis.NewClient(opts)
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}
	s.cfg = cfg
	s.client = client
	config.Debug("[REDIS] Connected to %s", opts.Addr)
	return nil
}

func (s *RedisSource) Close(_ context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *RedisSource) HandlesIncrementality() bool {
	return false
}

func (s *RedisSource) SupportsStreaming() bool {
	return true
}

func (s *RedisSource) DefaultStreamingStrategy() config.IncrementalStrategy {
	return config.StrategyMerge
}

func (s *RedisSource) GetTable(_ context.Context, req source.TableRequest) (source.SourceTable, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("redis source requires a stream key as source-table")
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
			TableName:           req.Name,
			TablePrimaryKeys:    primaryKeys,
			TableIncrementalKey: streamOrderColumn,
			TableStrategy:       streamStrategy,
			KnownSchema:         true,
			SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
				return streamingEnvelopeSchema(req.Name), nil
			},
			ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
				return s.read(ctx, req.Name, opts)
			},
		}, nil
	}

	return &source.DynamicSourceTable{
		TableName:           req.Name,
		TablePrimaryKeys:    []string{"redis_msg_id"},
		TableIncrementalKey: req.IncrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("redis source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, req.Name, opts)
		},
	}, nil
}

func streamingEnvelopeSchema(stream string) *schema.TableSchema {
	return &schema.TableSchema{
		Name: stream,
		Columns: []schema.Column{
			{Name: "msg_id", DataType: schema.TypeString, Nullable: false, IsPrimaryKey: true},
			{Name: "data", DataType: schema.TypeJSON, Nullable: true},
			{Name: streamOrderColumn, DataType: schema.TypeInt64, Nullable: false},
		},
		PrimaryKeys:    []string{"msg_id"},
		IncrementalKey: streamOrderColumn,
	}
}

func parseRedisURI(raw string) (redisConfig, string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return redisConfig{}, "", fmt.Errorf("invalid Redis URI: %w", err)
	}
	if u.Scheme != "redis" && u.Scheme != "rediss" {
		return redisConfig{}, "", fmt.Errorf("invalid Redis URI: must start with redis:// or rediss://")
	}
	q := u.Query()
	cfg := redisConfig{
		RawURL:       raw,
		Group:        firstQuery(q, "group", "group_id", "consumer_group"),
		Consumer:     firstQuery(q, "consumer", "consumer_name"),
		BatchSize:    defaultBatchSize,
		BatchTimeout: defaultBatchTimeout,
		ClaimMinIdle: -1,
	}
	if cfg.Group == "" {
		cfg.Group = defaultGroup
	}
	if cfg.Consumer == "" {
		cfg.Consumer = defaultConsumer
	}
	if v := q.Get("batch_size"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed <= 0 {
			return redisConfig{}, "", fmt.Errorf("redis URI: batch_size must be positive")
		}
		cfg.BatchSize = parsed
		q.Del("batch_size")
	}
	if v := q.Get("batch_timeout"); v != "" {
		seconds, err := strconv.ParseFloat(v, 64)
		if err != nil || seconds <= 0 {
			return redisConfig{}, "", fmt.Errorf("redis URI: batch_timeout must be positive seconds")
		}
		cfg.BatchTimeout = time.Duration(seconds * float64(time.Second))
		q.Del("batch_timeout")
	}
	if v := q.Get("claim_min_idle"); v != "" {
		seconds, err := strconv.ParseFloat(v, 64)
		if err != nil || seconds < 0 {
			return redisConfig{}, "", fmt.Errorf("redis URI: claim_min_idle must be non-negative seconds")
		}
		cfg.ClaimMinIdle = time.Duration(seconds * float64(time.Second))
		q.Del("claim_min_idle")
	}
	for _, key := range []string{"group", "group_id", "consumer_group", "consumer", "consumer_name"} {
		q.Del(key)
	}
	u.RawQuery = q.Encode()
	return cfg, u.String(), nil
}

func firstQuery(values url.Values, keys ...string) string {
	for _, key := range keys {
		if v := values.Get(key); v != "" {
			return v
		}
	}
	return ""
}

func (s *RedisSource) read(ctx context.Context, stream string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	if s.client == nil {
		return nil, fmt.Errorf("redis source is not connected")
	}
	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = s.cfg.BatchSize
	}
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}

	var cutoff string
	if opts.Streaming {
		if err := s.ensureConsumerGroup(ctx, stream); err != nil {
			return nil, err
		}
		s.mu.Lock()
		s.pending = make(map[string]struct{})
		s.mu.Unlock()
	} else {
		info, err := s.client.XInfoStream(ctx, stream).Result()
		if err != nil {
			if err == goredis.Nil {
				return closedResults(), nil
			}
			return nil, fmt.Errorf("failed to read Redis stream info for %s: %w", stream, err)
		}
		if info.Length == 0 || info.LastEntry.ID == "" {
			return closedResults(), nil
		}
		cutoff = info.LastEntry.ID
		config.Debug("[REDIS] Batch mode cutoff for %s: %s", stream, cutoff)
	}

	results := make(chan source.RecordBatchResult, 8)
	go func() {
		defer close(results)
		lastID := "0-0"
		claimStart := "0-0"
		reclaimingPending := opts.Streaming
		var orderSeq int64
		batch := make([]map[string]any, 0, batchSize)
		var maxID string
		claimMinIdle := redisClaimMinIdle(s.cfg.ClaimMinIdle, opts.FlushInterval, s.cfg.BatchTimeout)
		flush := func() bool {
			if len(batch) == 0 {
				return true
			}
			var cols []schema.Column
			if opts.Streaming {
				cols = streamingEnvelopeSchema(stream).Columns
			}
			record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, cols, opts.ExcludeColumns)
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to convert Redis stream entries to Arrow: %w", err)}
				return false
			}
			res := source.RecordBatchResult{Batch: record}
			if opts.Streaming {
				res.CommitToken = redisCommitToken{Stream: stream, MaxID: maxID}
			}
			results <- res
			batch = batch[:0]
			return true
		}

		for {
			if ctx.Err() != nil {
				flush()
				return
			}
			if opts.Streaming && !reclaimingPending && claimStart != "0-0" {
				if s.hasTrackedPending() {
					select {
					case <-ctx.Done():
						flush()
						return
					case <-time.After(100 * time.Millisecond):
						continue
					}
				}
				reclaimingPending = true
			}
			var streams []goredis.XStream
			var err error
			claimedPage := false
			if opts.Streaming && reclaimingPending {
				var messages []goredis.XMessage
				messages, claimStart, err = s.client.XAutoClaim(ctx, &goredis.XAutoClaimArgs{
					Stream:   stream,
					Group:    s.cfg.Group,
					Consumer: s.cfg.Consumer,
					MinIdle:  claimMinIdle,
					Start:    claimStart,
					Count:    int64(batchSize - len(batch)),
				}).Result()
				if err == nil {
					streams = []goredis.XStream{{Stream: stream, Messages: messages}}
					claimedPage = len(messages) > 0
					if len(messages) == 0 && claimStart == "0-0" {
						reclaimingPending = false
						continue
					}
				}
			} else if opts.Streaming {
				streams, err = s.client.XReadGroup(ctx, &goredis.XReadGroupArgs{
					Group:    s.cfg.Group,
					Consumer: s.cfg.Consumer,
					Streams:  []string{stream, ">"},
					Count:    int64(batchSize - len(batch)),
					Block:    s.cfg.BatchTimeout,
				}).Result()
			} else {
				streams, err = s.client.XRead(ctx, &goredis.XReadArgs{
					Streams: []string{stream, lastID},
					Count:   int64(batchSize - len(batch)),
					Block:   -1,
				}).Result()
			}
			if err != nil {
				if ctx.Err() != nil {
					flush()
					return
				}
				if err == goredis.Nil {
					if !flush() {
						return
					}
					if !opts.Streaming {
						return
					}
					if !s.hasTrackedPending() {
						reclaimingPending = true
					}
					continue
				}
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to read Redis stream %s: %w", stream, err)}
				return
			}
			if len(streams) == 0 || len(streams[0].Messages) == 0 {
				if !flush() {
					return
				}
				if !opts.Streaming {
					return
				}
				continue
			}
			for _, msg := range streams[0].Messages {
				if cutoff != "" && compareRedisIDs(msg.ID, cutoff) > 0 {
					flush()
					return
				}
				lastID = msg.ID
				if opts.Streaming {
					orderSeq++
					batch = append(batch, messageToEnvelope(stream, msg, orderSeq))
					s.trackPending(msg.ID)
					maxID = msg.ID
				} else {
					batch = append(batch, messageToItem(stream, msg))
				}
				if len(batch) >= batchSize {
					if !flush() {
						return
					}
				}
				if cutoff != "" && compareRedisIDs(msg.ID, cutoff) >= 0 {
					flush()
					return
				}
			}
			if opts.Streaming && reclaimingPending {
				if claimedPage && !flush() {
					return
				}
				reclaimingPending = false
			}
		}
	}()
	return results, nil
}

func (s *RedisSource) ensureConsumerGroup(ctx context.Context, stream string) error {
	err := s.client.XGroupCreateMkStream(ctx, stream, s.cfg.Group, "0").Err()
	if err != nil && !strings.Contains(strings.ToUpper(err.Error()), "BUSYGROUP") {
		return fmt.Errorf("failed to create Redis consumer group %s for stream %s: %w", s.cfg.Group, stream, err)
	}
	if err := s.client.XGroupCreateConsumer(ctx, stream, s.cfg.Group, s.cfg.Consumer).Err(); err != nil && err != goredis.Nil {
		return fmt.Errorf("failed to create Redis consumer %s in group %s: %w", s.cfg.Consumer, s.cfg.Group, err)
	}
	return nil
}

func (s *RedisSource) trackPending(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending[id] = struct{}{}
}

func (s *RedisSource) hasTrackedPending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pending) > 0
}

func (s *RedisSource) CommitStream(ctx context.Context, token any) error {
	tok, ok := token.(redisCommitToken)
	if !ok {
		return fmt.Errorf("redis: unexpected commit token type %T", token)
	}
	if tok.MaxID == "" {
		return nil
	}
	s.mu.Lock()
	ids := make([]string, 0)
	for id := range s.pending {
		if compareRedisIDs(id, tok.MaxID) <= 0 {
			ids = append(ids, id)
		}
	}
	s.mu.Unlock()
	if len(ids) == 0 {
		return nil
	}
	if err := s.client.XAck(ctx, tok.Stream, s.cfg.Group, ids...).Err(); err != nil {
		return fmt.Errorf("failed to ack Redis stream entries: %w", err)
	}
	s.mu.Lock()
	for _, id := range ids {
		delete(s.pending, id)
	}
	s.mu.Unlock()
	return nil
}

func redisClaimMinIdle(configured, flushInterval, batchTimeout time.Duration) time.Duration {
	if configured >= 0 {
		return configured
	}
	if flushInterval <= 0 {
		flushInterval = defaultFlushInterval
	}
	minIdle := flushInterval*3 + batchTimeout
	if minIdle < defaultFlushInterval {
		return defaultFlushInterval
	}
	return minIdle
}

func closedResults() <-chan source.RecordBatchResult {
	ch := make(chan source.RecordBatchResult)
	close(ch)
	return ch
}

func messageToItem(stream string, msg goredis.XMessage) map[string]any {
	values := normalizeValues(msg.Values)
	return map[string]any{
		"redis_msg_id": digest128(stream + ":" + msg.ID),
		"redis": map[string]any{
			"stream": stream,
			"id":     msg.ID,
		},
		"data": values,
	}
}

func messageToEnvelope(stream string, msg goredis.XMessage, fallbackOrder int64) map[string]any {
	item := messageToItem(stream, msg)
	msgID, _ := item["redis_msg_id"].(string)
	delete(item, "redis_msg_id")
	encoded, err := json.Marshal(item)
	if err != nil {
		encoded = []byte("null")
	}
	return map[string]any{
		"msg_id":          msgID,
		"data":            string(encoded),
		streamOrderColumn: redisOrder(msg.ID, fallbackOrder),
	}
}

func normalizeValues(values map[string]any) map[string]any {
	out := make(map[string]any, len(values))
	for k, v := range values {
		switch typed := v.(type) {
		case string:
			out[k] = decodeMaybeJSON([]byte(typed))
		case []byte:
			out[k] = decodeMaybeJSON(typed)
		default:
			out[k] = typed
		}
	}
	return out
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

func compareRedisIDs(a, b string) int {
	ams, aseq, aok := parseRedisID(a)
	bms, bseq, bok := parseRedisID(b)
	if !aok || !bok {
		return strings.Compare(a, b)
	}
	switch {
	case ams < bms:
		return -1
	case ams > bms:
		return 1
	case aseq < bseq:
		return -1
	case aseq > bseq:
		return 1
	default:
		return 0
	}
}

func parseRedisID(id string) (uint64, uint64, bool) {
	left, right, ok := strings.Cut(id, "-")
	if !ok {
		ms, ok := parseUint64(left)
		return ms, 0, ok
	}
	ms, msOK := parseUint64(left)
	seq, seqOK := parseUint64(right)
	return ms, seq, msOK && seqOK
}

func parseUint64(v string) (uint64, bool) {
	n, err := strconv.ParseUint(v, 10, 64)
	return n, err == nil
}

func redisOrder(id string, fallback int64) int64 {
	ms, seq, ok := parseRedisID(id)
	if !ok {
		return fallback
	}
	const max = uint64(1<<63 - 1)
	if ms > max/1_000_000 {
		return int64(max)
	}
	order := ms * 1_000_000
	if seq > max-order {
		return int64(max)
	}
	return int64(order + seq)
}

func digest128(value string) string {
	sum := sha256.Sum256([]byte(value))
	return strings.TrimRight(base64.StdEncoding.EncodeToString(sum[:15]), "=")
}

var (
	_ source.Source          = (*RedisSource)(nil)
	_ source.StreamingSource = (*RedisSource)(nil)
	_ source.StreamCommitter = (*RedisSource)(nil)
)
