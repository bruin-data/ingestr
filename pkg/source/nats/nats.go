package nats

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

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	natsgo "github.com/nats-io/nats.go"
)

const (
	defaultBatchSize    = 3000
	defaultBatchTimeout = 5 * time.Second
	streamOrderColumn   = "_ingestr_order"
)

type natsConfig struct {
	URL          string
	Stream       string
	Subject      string
	SubjectSet   bool
	Durable      string
	BindConsumer bool
	Token        string
	Credentials  string
	BatchSize    int
	BatchTimeout time.Duration
}

type natsCommitToken struct {
	MaxPendingSeq uint64
}

type NATSSource struct {
	cfg natsConfig
	nc  *natsgo.Conn
	js  natsgo.JetStreamContext

	mu             sync.Mutex
	nextPendingSeq uint64
	pending        map[uint64]*natsgo.Msg
}

func NewNATSSource() *NATSSource {
	return &NATSSource{pending: make(map[uint64]*natsgo.Msg)}
}

func (s *NATSSource) Schemes() []string {
	return []string{"nats"}
}

func (s *NATSSource) Connect(_ context.Context, raw string) error {
	cfg, err := parseNATSURI(raw)
	if err != nil {
		return err
	}
	opts := []natsgo.Option{natsgo.Name("ingestr")}
	if cfg.Token != "" {
		opts = append(opts, natsgo.Token(cfg.Token))
	}
	if cfg.Credentials != "" {
		opts = append(opts, natsgo.UserCredentials(cfg.Credentials))
	}
	nc, err := natsgo.Connect(cfg.URL, opts...)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}
	js, err := nc.JetStream(natsgo.MaxWait(cfg.BatchTimeout))
	if err != nil {
		nc.Close()
		return fmt.Errorf("failed to initialize JetStream: %w", err)
	}
	s.cfg = cfg
	s.nc = nc
	s.js = js
	config.Debug("[NATS] Connected to %s", sanitizeNATSURL(cfg.URL))
	return nil
}

func (s *NATSSource) Close(_ context.Context) error {
	if s.nc != nil {
		s.nc.Close()
	}
	return nil
}

func (s *NATSSource) HandlesIncrementality() bool {
	return false
}

func (s *NATSSource) SupportsStreaming() bool {
	return true
}

func (s *NATSSource) DefaultStreamingStrategy() config.IncrementalStrategy {
	return config.StrategyMerge
}

func (s *NATSSource) GetTable(_ context.Context, req source.TableRequest) (source.SourceTable, error) {
	stream := req.Name
	if stream == "" {
		stream = s.cfg.Stream
	}
	if stream == "" {
		return nil, fmt.Errorf("nats source requires a stream name as source-table or stream query parameter")
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
			TableName:           stream,
			TablePrimaryKeys:    primaryKeys,
			TableIncrementalKey: streamOrderColumn,
			TableStrategy:       streamStrategy,
			KnownSchema:         true,
			SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
				return streamingEnvelopeSchema(stream), nil
			},
			ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
				return s.read(ctx, stream, opts)
			},
		}, nil
	}

	return &source.DynamicSourceTable{
		TableName:           stream,
		TablePrimaryKeys:    []string{"nats_msg_id"},
		TableIncrementalKey: req.IncrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("nats source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, stream, opts)
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

func parseNATSURI(raw string) (natsConfig, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return natsConfig{}, fmt.Errorf("invalid NATS URI: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "nats":
	default:
		return natsConfig{}, fmt.Errorf("invalid NATS URI: unsupported scheme %q", u.Scheme)
	}
	if u.Hostname() == "" {
		return natsConfig{}, fmt.Errorf("nats URI: host is required")
	}
	q := u.Query()
	durable := q.Get("durable")
	bindConsumer, err := parseNATSBool(firstQuery(q, "bind", "bind_consumer"))
	if err != nil {
		return natsConfig{}, err
	}
	if durable == "" {
		if consumer := firstQuery(q, "consumer", "consumer_name"); consumer != "" {
			durable = consumer
			bindConsumer = true
		}
	}
	cfg := natsConfig{
		Stream:       firstQuery(q, "stream"),
		Subject:      firstQuery(q, "subject"),
		SubjectSet:   q.Get("subject") != "",
		Durable:      durable,
		BindConsumer: bindConsumer,
		Token:        firstQuery(q, "token"),
		Credentials:  firstQuery(q, "credentials", "creds", "credentials_file"),
		BatchSize:    defaultBatchSize,
		BatchTimeout: defaultBatchTimeout,
	}
	if cfg.Subject == "" && !cfg.BindConsumer {
		cfg.Subject = ">"
	}
	if v := q.Get("batch_size"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed <= 0 {
			return natsConfig{}, fmt.Errorf("nats URI: batch_size must be positive")
		}
		cfg.BatchSize = parsed
	}
	if v := q.Get("batch_timeout"); v != "" {
		seconds, err := strconv.ParseFloat(v, 64)
		if err != nil || seconds <= 0 {
			return natsConfig{}, fmt.Errorf("nats URI: batch_timeout must be positive seconds")
		}
		cfg.BatchTimeout = time.Duration(seconds * float64(time.Second))
	}
	for _, key := range []string{"stream", "subject", "durable", "consumer", "consumer_name", "bind", "bind_consumer", "token", "credentials", "creds", "credentials_file", "batch_size", "batch_timeout"} {
		q.Del(key)
	}
	u.RawQuery = q.Encode()
	cfg.URL = u.String()
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

func parseNATSBool(value string) (bool, error) {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", "0", "false", "no":
		return false, nil
	case "1", "true", "yes":
		return true, nil
	default:
		return false, fmt.Errorf("nats URI: bind must be a boolean")
	}
}

func (s *NATSSource) read(ctx context.Context, stream string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	if s.js == nil {
		return nil, fmt.Errorf("nats source is not connected")
	}
	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = s.cfg.BatchSize
	}
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}

	var cutoff uint64
	if !opts.Streaming {
		info, err := s.js.StreamInfo(stream)
		if err != nil {
			return nil, fmt.Errorf("failed to read NATS stream info for %s: %w", stream, err)
		}
		cutoff = info.State.LastSeq
		if cutoff == 0 {
			return closedResults(), nil
		}
		config.Debug("[NATS] Batch mode cutoff for %s: %d", stream, cutoff)
	}

	durable := ""
	subOpts := []natsgo.SubOpt{
		natsgo.BindStream(stream),
		natsgo.AckExplicit(),
		natsgo.PullMaxWaiting(1),
	}
	if opts.Streaming {
		durable = s.cfg.Durable
		if s.cfg.BindConsumer && durable == "" {
			return nil, fmt.Errorf("nats source: bind requires consumer, consumer_name, or durable query parameter")
		}
		if durable == "" {
			durable = "ingestr_" + safeName(stream)
		}
		if s.cfg.BindConsumer {
			subOpts = []natsgo.SubOpt{natsgo.Bind(stream, durable)}
		} else {
			subOpts = append(subOpts, natsgo.AckWait(natsAckWait(s.cfg.BatchTimeout, opts.FlushInterval)))
		}
	} else {
		subOpts = append(subOpts, natsgo.DeliverAll())
	}
	subject := s.cfg.Subject
	if opts.Streaming && s.cfg.BindConsumer {
		var err error
		subject, err = s.resolveBoundConsumerSubject(stream, durable, opts)
		if err != nil {
			return nil, err
		}
	}
	sub, err := s.js.PullSubscribe(subject, durable, subOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to NATS stream %s subject %s: %w", stream, subject, err)
	}

	if opts.Streaming {
		s.mu.Lock()
		s.nextPendingSeq = 0
		s.pending = make(map[uint64]*natsgo.Msg)
		s.mu.Unlock()
	}

	results := make(chan source.RecordBatchResult, 8)
	go func() {
		defer close(results)
		defer func() {
			_ = sub.Unsubscribe()
		}()

		for {
			if ctx.Err() != nil {
				return
			}
			fetchCtx, cancel := context.WithTimeout(ctx, s.cfg.BatchTimeout)
			msgs, err := sub.Fetch(batchSize, natsgo.Context(fetchCtx))
			timedOut := errors.Is(fetchCtx.Err(), context.DeadlineExceeded)
			cancel()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				if timedOut || errors.Is(err, natsgo.ErrTimeout) || errors.Is(err, context.DeadlineExceeded) {
					if !opts.Streaming {
						return
					}
					continue
				}
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to fetch NATS messages: %w", err)}
				return
			}
			if len(msgs) == 0 {
				if !opts.Streaming {
					return
				}
				continue
			}

			items := make([]map[string]any, 0, len(msgs))
			var maxSeq uint64
			var maxPendingSeq uint64
			var ackNow []*natsgo.Msg
			reachedCutoff := false
			for _, msg := range msgs {
				meta, err := msg.Metadata()
				if err != nil {
					results <- source.RecordBatchResult{Err: fmt.Errorf("failed to read NATS message metadata: %w", err)}
					return
				}
				seq := meta.Sequence.Stream
				if cutoff > 0 && seq > cutoff {
					reachedCutoff = true
					break
				}
				if opts.Streaming {
					items = append(items, messageToEnvelope(msg, meta))
					maxPendingSeq = s.trackPending(msg)
				} else {
					items = append(items, messageToItem(msg, meta))
					ackNow = append(ackNow, msg)
				}
				if seq > maxSeq {
					maxSeq = seq
				}
			}
			if len(items) == 0 {
				if !opts.Streaming {
					return
				}
				continue
			}
			var cols []schema.Column
			if opts.Streaming {
				cols = streamingEnvelopeSchema(stream).Columns
			}
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, cols, opts.ExcludeColumns)
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to convert NATS messages to Arrow: %w", err)}
				return
			}
			res := source.RecordBatchResult{Batch: record}
			if opts.Streaming {
				res.CommitToken = natsCommitToken{MaxPendingSeq: maxPendingSeq}
			}
			results <- res
			for _, msg := range ackNow {
				if err := msg.Ack(); err != nil {
					config.Debug("[NATS] Failed to ack message: %v", err)
				}
			}
			if reachedCutoff || cutoff > 0 && maxSeq >= cutoff {
				return
			}
		}
	}()
	return results, nil
}

func (s *NATSSource) resolveBoundConsumerSubject(stream, durable string, opts source.ReadOptions) (string, error) {
	info, err := s.js.ConsumerInfo(stream, durable)
	if err != nil {
		return "", fmt.Errorf("failed to inspect NATS consumer %s on stream %s: %w", durable, stream, err)
	}
	if info.Config.AckPolicy != natsgo.AckExplicitPolicy {
		return "", fmt.Errorf("nats consumer %s must use explicit ack policy for streaming ingestion; got %s", durable, info.Config.AckPolicy.String())
	}
	requiredAckWait := natsAckWait(s.cfg.BatchTimeout, opts.FlushInterval)
	if info.Config.AckWait > 0 && info.Config.AckWait < requiredAckWait {
		return "", fmt.Errorf("nats consumer %s ack_wait %s is shorter than required %s for flush interval %s", durable, info.Config.AckWait, requiredAckWait, opts.FlushInterval)
	}
	if s.cfg.SubjectSet {
		return s.cfg.Subject, nil
	}
	if info.Config.FilterSubject != "" {
		return info.Config.FilterSubject, nil
	}
	switch len(info.Config.FilterSubjects) {
	case 0:
		return ">", nil
	case 1:
		return info.Config.FilterSubjects[0], nil
	default:
		return "", fmt.Errorf("nats consumer %s has multiple filter subjects; set subject= in the URI", durable)
	}
}

func (s *NATSSource) trackPending(msg *natsgo.Msg) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextPendingSeq++
	seq := s.nextPendingSeq
	s.pending[seq] = msg
	return seq
}

func (s *NATSSource) CommitStream(_ context.Context, token any) error {
	tok, ok := token.(natsCommitToken)
	if !ok {
		return fmt.Errorf("nats: unexpected commit token type %T", token)
	}
	s.mu.Lock()
	msgs := make([]*natsgo.Msg, 0)
	for seq, msg := range s.pending {
		if seq <= tok.MaxPendingSeq {
			msgs = append(msgs, msg)
			delete(s.pending, seq)
		}
	}
	s.mu.Unlock()
	for _, msg := range msgs {
		if err := msg.Ack(); err != nil {
			return fmt.Errorf("failed to ack NATS message: %w", err)
		}
	}
	return nil
}

func natsAckWait(batchTimeout, flushInterval time.Duration) time.Duration {
	if flushInterval <= 0 {
		flushInterval = 30 * time.Second
	}
	wait := flushInterval*3 + batchTimeout
	if wait < 30*time.Second {
		return 30 * time.Second
	}
	return wait
}

func closedResults() <-chan source.RecordBatchResult {
	ch := make(chan source.RecordBatchResult)
	close(ch)
	return ch
}

func messageToItem(msg *natsgo.Msg, meta *natsgo.MsgMetadata) map[string]any {
	data := decodeMaybeJSON(msg.Data)
	headers := map[string]any{}
	for key, values := range msg.Header {
		copied := make([]string, len(values))
		copy(copied, values)
		headers[key] = copied
	}
	msgID := fmt.Sprintf("%s:%d", meta.Stream, meta.Sequence.Stream)
	return map[string]any{
		"nats_msg_id": digest128(msgID),
		"data":        data,
		"nats": map[string]any{
			"stream":            meta.Stream,
			"consumer":          meta.Consumer,
			"subject":           msg.Subject,
			"stream_sequence":   meta.Sequence.Stream,
			"consumer_sequence": meta.Sequence.Consumer,
			"timestamp":         meta.Timestamp.UTC().Format(time.RFC3339Nano),
			"num_delivered":     meta.NumDelivered,
			"headers":           headers,
		},
	}
}

func messageToEnvelope(msg *natsgo.Msg, meta *natsgo.MsgMetadata) map[string]any {
	item := messageToItem(msg, meta)
	msgID, _ := item["nats_msg_id"].(string)
	delete(item, "nats_msg_id")
	encoded, err := json.Marshal(item)
	if err != nil {
		encoded = []byte("null")
	}
	return map[string]any{
		"msg_id":          msgID,
		"data":            string(encoded),
		streamOrderColumn: int64(meta.Sequence.Stream),
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

func digest128(value string) string {
	sum := sha256.Sum256([]byte(value))
	return strings.TrimRight(base64.StdEncoding.EncodeToString(sum[:15]), "=")
}

func safeName(value string) string {
	replacer := strings.NewReplacer(".", "_", "-", "_", "/", "_", ">", "_", "*", "_")
	return replacer.Replace(value)
}

func sanitizeNATSURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	u.User = url.User("***")
	return u.String()
}

var (
	_ source.Source          = (*NATSSource)(nil)
	_ source.StreamingSource = (*NATSSource)(nil)
	_ source.StreamCommitter = (*NATSSource)(nil)
)
