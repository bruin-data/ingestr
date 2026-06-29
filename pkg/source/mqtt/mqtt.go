package mqtt

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	paho "github.com/eclipse/paho.mqtt.golang"
)

const (
	defaultBatchSize      = 3000
	defaultBatchTimeout   = 5 * time.Second
	defaultConnectTimeout = 30 * time.Second
	defaultKeepAlive      = 30 * time.Second
	defaultQOS            = byte(1)
	streamOrderColumn     = "_ingestr_order"
)

type mqttConfig struct {
	BrokerURL            string
	ClientID             string
	Username             string
	Password             string
	QOS                  byte
	CleanSession         bool
	BatchSize            int
	BatchTimeout         time.Duration
	ConnectTimeout       time.Duration
	KeepAlive            time.Duration
	InsecureSkipVerify   bool
	ConnectRetry         bool
	ConnectRetryInterval time.Duration
	AutoReconnect        bool
}

type MQTTSource struct {
	cfg    mqttConfig
	client paho.Client

	mu           sync.Mutex
	nextSeq      int64
	pending      map[int64]paho.Message
	pendingKeys  map[int64]string
	pendingByKey map[string]int64
	connLost     chan error
}

type mqttCommitToken struct {
	MaxSeq int64
}

func NewMQTTSource() *MQTTSource {
	return &MQTTSource{
		pending:      make(map[int64]paho.Message),
		pendingKeys:  make(map[int64]string),
		pendingByKey: make(map[string]int64),
	}
}

func (s *MQTTSource) Schemes() []string {
	return []string{"mqtt", "mqtts"}
}

func (s *MQTTSource) HandlesIncrementality() bool {
	return false
}

func (s *MQTTSource) SupportsStreaming() bool {
	return true
}

func (s *MQTTSource) DefaultStreamingStrategy() config.IncrementalStrategy {
	return config.StrategyMerge
}

func (s *MQTTSource) Connect(ctx context.Context, rawURI string) error {
	cfg, err := parseMQTTURI(rawURI)
	if err != nil {
		return err
	}

	s.cfg = cfg
	s.connLost = make(chan error, 1)
	opts := clientOptions(cfg)
	opts.SetConnectionLostHandler(func(_ paho.Client, err error) {
		if err == nil {
			return
		}
		select {
		case s.connLost <- fmt.Errorf("mqtt connection lost: %w", err):
		default:
		}
	})

	client := paho.NewClient(opts)
	if err := waitToken(ctx, client.Connect(), cfg.ConnectTimeout, "connect to MQTT broker"); err != nil {
		return err
	}

	s.client = client
	config.Debug("[MQTT] Connected to %s, client_id=%s, qos=%d", sanitizeURI(cfg.BrokerURL), cfg.ClientID, cfg.QOS)
	return nil
}

func (s *MQTTSource) Close(_ context.Context) error {
	if s.client != nil && s.client.IsConnected() {
		s.client.Disconnect(250)
	}
	return nil
}

func (s *MQTTSource) GetTable(_ context.Context, req source.TableRequest) (source.SourceTable, error) {
	topic := req.Name
	if topic == "" {
		return nil, fmt.Errorf("mqtt source requires a topic filter as source-table")
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
			TableName:           tableNameFromTopic(topic),
			TablePrimaryKeys:    primaryKeys,
			TableIncrementalKey: streamOrderColumn,
			TableStrategy:       streamStrategy,
			KnownSchema:         true,
			SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
				return streamingEnvelopeSchema(tableNameFromTopic(topic)), nil
			},
			ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
				return s.read(ctx, topic, opts)
			},
		}, nil
	}

	return &source.DynamicSourceTable{
		TableName:           tableNameFromTopic(topic),
		TablePrimaryKeys:    req.PrimaryKeys,
		TableIncrementalKey: req.IncrementalKey,
		TableStrategy:       strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("mqtt source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, topic, opts)
		},
	}, nil
}

func streamingEnvelopeSchema(table string) *schema.TableSchema {
	return &schema.TableSchema{
		Name: table,
		Columns: []schema.Column{
			{Name: "msg_id", DataType: schema.TypeString, Nullable: false, IsPrimaryKey: true},
			{Name: "message_id", DataType: schema.TypeInt64, Nullable: true},
			{Name: "data", DataType: schema.TypeJSON, Nullable: true},
			{Name: streamOrderColumn, DataType: schema.TypeInt64, Nullable: false},
		},
		PrimaryKeys:    []string{"msg_id"},
		IncrementalKey: streamOrderColumn,
	}
}

func (s *MQTTSource) read(ctx context.Context, topic string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	if s.client == nil || !s.client.IsConnected() {
		return nil, fmt.Errorf("mqtt client is not connected")
	}

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = s.cfg.BatchSize
	}
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}

	streaming := opts.Streaming
	s.mu.Lock()
	s.nextSeq = 0
	s.pending = make(map[int64]paho.Message)
	s.pendingKeys = make(map[int64]string)
	s.pendingByKey = make(map[string]int64)
	s.mu.Unlock()

	incoming := make(chan paho.Message, batchSize)
	done := make(chan struct{})
	handler := func(_ paho.Client, msg paho.Message) {
		select {
		case incoming <- msg:
		case <-ctx.Done():
		case <-done:
		}
	}

	if err := waitToken(ctx, s.client.Subscribe(topic, s.cfg.QOS, handler), s.cfg.ConnectTimeout, "subscribe to MQTT topic"); err != nil {
		close(done)
		return nil, fmt.Errorf("failed to subscribe to topic %s: %w", topic, err)
	}
	config.Debug("[MQTT] Subscribed to %s with qos=%d", topic, s.cfg.QOS)

	results := make(chan source.RecordBatchResult, 8)
	go func() {
		defer close(results)
		defer close(done)
		if !streaming {
			defer func() {
				if s.client != nil && s.client.IsConnected() {
					_ = waitToken(context.Background(), s.client.Unsubscribe(topic), s.cfg.ConnectTimeout, "unsubscribe from MQTT topic")
				}
			}()
		}

		var envelopeCols []schema.Column
		if streaming {
			envelopeCols = streamingEnvelopeSchema(tableNameFromTopic(topic)).Columns
		}

		batch := make([]map[string]any, 0, batchSize)
		var batchSeqs []int64
		if streaming {
			batchSeqs = make([]int64, 0, batchSize)
		}
		totalRead := int64(0)
		limit := int64(opts.Limit)
		receivedSinceLastTick := false

		timer := time.NewTimer(s.cfg.BatchTimeout)
		defer timer.Stop()

		resetTimer := func() {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(s.cfg.BatchTimeout)
		}

		flush := func() bool {
			if len(batch) == 0 {
				return true
			}
			record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, envelopeCols, opts.ExcludeColumns)
			if err != nil {
				results <- source.RecordBatchResult{Err: fmt.Errorf("failed to convert MQTT messages to Arrow: %w", err)}
				return false
			}

			res := source.RecordBatchResult{Batch: record}
			if streaming {
				res.CommitToken = mqttCommitToken{MaxSeq: batchSeqs[len(batchSeqs)-1]}
			}
			results <- res

			batch = batch[:0]
			if streaming {
				batchSeqs = batchSeqs[:0]
			}
			return true
		}

		for {
			select {
			case <-ctx.Done():
				flush()
				return

			case err := <-s.connLost:
				if ctx.Err() != nil {
					flush()
					return
				}
				results <- source.RecordBatchResult{Err: err}
				return

			case msg := <-incoming:
				seq := s.trackMessage(msg)
				if streaming {
					batch = append(batch, messageToEnvelope(msg, seq))
					batchSeqs = append(batchSeqs, seq)
				} else {
					totalRead++
					batch = append(batch, messageToItem(msg, seq))
				}
				if streaming {
					totalRead++
				}
				receivedSinceLastTick = true

				if limit > 0 && totalRead >= limit {
					flush()
					return
				}

				if len(batch) >= batchSize {
					if !flush() {
						return
					}
					resetTimer()
					receivedSinceLastTick = false
				}

			case <-timer.C:
				if len(batch) > 0 {
					if !flush() {
						return
					}
					timer.Reset(s.cfg.BatchTimeout)
					receivedSinceLastTick = false
					continue
				}
				if !streaming && !receivedSinceLastTick {
					config.Debug("[MQTT] No new messages received, finishing read (total: %d)", totalRead)
					return
				}
				receivedSinceLastTick = false
				timer.Reset(s.cfg.BatchTimeout)
			}
		}
	}()

	return results, nil
}

func (s *MQTTSource) trackMessage(msg paho.Message) int64 {
	key := mqttDeliveryKey(msg)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.pending == nil {
		s.pending = make(map[int64]paho.Message)
	}
	if s.pendingKeys == nil {
		s.pendingKeys = make(map[int64]string)
	}
	if s.pendingByKey == nil {
		s.pendingByKey = make(map[string]int64)
	}
	if seq, ok := s.pendingByKey[key]; ok {
		s.pending[seq] = msg
		return seq
	}

	s.nextSeq++
	s.pending[s.nextSeq] = msg
	s.pendingKeys[s.nextSeq] = key
	s.pendingByKey[key] = s.nextSeq
	return s.nextSeq
}

func (s *MQTTSource) CommitStream(_ context.Context, token any) error {
	tok, ok := token.(mqttCommitToken)
	if !ok {
		return fmt.Errorf("mqtt: unexpected commit token type %T", token)
	}

	msgs := s.pendingThrough(tok.MaxSeq)
	for _, msg := range msgs {
		msg.Ack()
	}
	return nil
}

func (s *MQTTSource) FinalizeBatch(_ context.Context) error {
	msgs := s.pendingThrough(s.currentSeq())
	for _, msg := range msgs {
		msg.Ack()
	}
	return nil
}

func (s *MQTTSource) pendingThrough(maxSeq int64) []paho.Message {
	s.mu.Lock()
	defer s.mu.Unlock()

	msgs := make([]paho.Message, 0)
	for seq, msg := range s.pending {
		if seq <= maxSeq {
			msgs = append(msgs, msg)
			delete(s.pending, seq)
			if key, ok := s.pendingKeys[seq]; ok {
				delete(s.pendingByKey, key)
				delete(s.pendingKeys, seq)
			}
		}
	}
	return msgs
}

func (s *MQTTSource) currentSeq() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nextSeq
}

func messageToItem(msg paho.Message, seq int64) map[string]any {
	id, numericID := mqttMsgID(msg, seq)
	return map[string]any{
		"msg_id":          id,
		"message_id":      numericID,
		"data":            decodePayload(msg.Payload()),
		"metadata":        messageMetadata(msg, numericID),
		streamOrderColumn: seq,
	}
}

func messageToEnvelope(msg paho.Message, seq int64) map[string]any {
	item := messageToItem(msg, seq)
	encoded, err := json.Marshal(map[string]any{
		"data":     item["data"],
		"metadata": item["metadata"],
	})
	if err != nil {
		encoded = fmt.Appendf(nil, "%q", string(msg.Payload()))
	}
	return map[string]any{
		"msg_id":          item["msg_id"],
		"message_id":      item["message_id"],
		"data":            string(encoded),
		streamOrderColumn: seq,
	}
}

func messageMetadata(msg paho.Message, numericID any) map[string]any {
	return map[string]any{
		"topic":      msg.Topic(),
		"qos":        int(msg.Qos()),
		"retained":   msg.Retained(),
		"duplicate":  msg.Duplicate(),
		"message_id": numericID,
	}
}

func mqttMsgID(msg paho.Message, seq int64) (string, any) {
	if id := msg.MessageID(); id != 0 {
		return fmt.Sprintf("%s:%d", mqttDeliveryKey(msg), seq), int64(id)
	}
	return mqttDeliveryKey(msg), nil
}

func mqttDeliveryKey(msg paho.Message) string {
	if id := msg.MessageID(); id != 0 {
		return fmt.Sprintf("%s:%d:%s", msg.Topic(), id, mqttMessageFingerprint(msg))
	}
	return fmt.Sprintf("%s:%s", msg.Topic(), mqttMessageFingerprint(msg))
}

func mqttMessageFingerprint(msg paho.Message) string {
	h := sha256.New()
	h.Write([]byte(msg.Topic()))
	h.Write([]byte{msg.Qos()})
	if msg.Retained() {
		h.Write([]byte{1})
	} else {
		h.Write([]byte{0})
	}
	h.Write(msg.Payload())
	sum := h.Sum(nil)
	return strings.TrimRight(base64.StdEncoding.EncodeToString(sum[:12]), "=")
}

func decodePayload(payload []byte) any {
	var decoded any
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.UseNumber()
	if err := dec.Decode(&decoded); err == nil {
		return decoded
	}
	return string(payload)
}

func clientOptions(cfg mqttConfig) *paho.ClientOptions {
	opts := paho.NewClientOptions().
		AddBroker(cfg.BrokerURL).
		SetClientID(cfg.ClientID).
		SetCleanSession(cfg.CleanSession).
		SetAutoAckDisabled(true).
		SetAutoReconnect(cfg.AutoReconnect).
		SetConnectRetry(cfg.ConnectRetry).
		SetConnectRetryInterval(cfg.ConnectRetryInterval).
		SetConnectTimeout(cfg.ConnectTimeout).
		SetKeepAlive(cfg.KeepAlive).
		SetWriteTimeout(cfg.ConnectTimeout).
		SetOrderMatters(false)

	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
	}
	if cfg.Password != "" {
		opts.SetPassword(cfg.Password)
	}
	if strings.HasPrefix(cfg.BrokerURL, "ssl://") {
		opts.SetTLSConfig(&tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: cfg.InsecureSkipVerify,
		})
	}
	return opts
}

func parseMQTTURI(raw string) (mqttConfig, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return mqttConfig{}, fmt.Errorf("invalid mqtt URI: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "mqtt" && scheme != "mqtts" {
		return mqttConfig{}, fmt.Errorf("invalid mqtt URI: must start with mqtt:// or mqtts://")
	}
	if u.Hostname() == "" {
		return mqttConfig{}, fmt.Errorf("mqtt URI: host is required")
	}

	q := u.Query()
	cleanSession := true
	if rawClean := firstQuery(q, "clean_session", "clean"); rawClean != "" {
		parsed, err := strconv.ParseBool(rawClean)
		if err != nil {
			return mqttConfig{}, fmt.Errorf("mqtt URI: clean_session must be a boolean")
		}
		cleanSession = parsed
	}

	clientID := firstQuery(q, "client_id", "clientid")
	if clientID == "" && !cleanSession {
		return mqttConfig{}, fmt.Errorf("mqtt URI: client_id is required when clean_session=false")
	}
	if clientID == "" {
		clientID = fmt.Sprintf("ingestr-%d-%d", os.Getpid(), time.Now().UnixNano())
	}

	username := firstQuery(q, "username", "user")
	password := firstQuery(q, "password", "pass")
	if u.User != nil {
		username = u.User.Username()
		if pwd, ok := u.User.Password(); ok {
			password = pwd
		}
	}

	brokerScheme := "tcp"
	defaultPort := "1883"
	if scheme == "mqtts" {
		brokerScheme = "ssl"
		defaultPort = "8883"
	}
	host := u.Host
	if u.Port() == "" {
		host = u.Hostname() + ":" + defaultPort
	}

	cfg := mqttConfig{
		BrokerURL:            brokerScheme + "://" + host,
		ClientID:             clientID,
		Username:             username,
		Password:             password,
		QOS:                  defaultQOS,
		CleanSession:         cleanSession,
		BatchSize:            defaultBatchSize,
		BatchTimeout:         defaultBatchTimeout,
		ConnectTimeout:       defaultConnectTimeout,
		KeepAlive:            defaultKeepAlive,
		ConnectRetryInterval: time.Second,
	}

	if v := firstQuery(q, "qos"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed < 0 || parsed > 2 {
			return mqttConfig{}, fmt.Errorf("mqtt URI: qos must be 0, 1, or 2")
		}
		cfg.QOS = byte(parsed)
	}
	if v := firstQuery(q, "batch_size"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed <= 0 {
			return mqttConfig{}, fmt.Errorf("mqtt URI: batch_size must be positive")
		}
		cfg.BatchSize = parsed
	}
	if v := firstQuery(q, "batch_timeout", "batch_timeout_seconds"); v != "" {
		parsed, err := parseDuration(v)
		if err != nil || parsed <= 0 {
			return mqttConfig{}, fmt.Errorf("mqtt URI: batch_timeout must be a positive duration")
		}
		cfg.BatchTimeout = parsed
	}
	if v := firstQuery(q, "connect_timeout", "connect_timeout_seconds"); v != "" {
		parsed, err := parseDuration(v)
		if err != nil || parsed <= 0 {
			return mqttConfig{}, fmt.Errorf("mqtt URI: connect_timeout must be a positive duration")
		}
		cfg.ConnectTimeout = parsed
	}
	if v := firstQuery(q, "keep_alive", "keepalive"); v != "" {
		parsed, err := parseDuration(v)
		if err != nil || parsed <= 0 {
			return mqttConfig{}, fmt.Errorf("mqtt URI: keep_alive must be a positive duration")
		}
		cfg.KeepAlive = parsed
	}
	if v := firstQuery(q, "insecure_skip_verify", "tls_insecure_skip_verify"); v != "" {
		parsed, err := strconv.ParseBool(v)
		if err != nil {
			return mqttConfig{}, fmt.Errorf("mqtt URI: insecure_skip_verify must be a boolean")
		}
		cfg.InsecureSkipVerify = parsed
	}
	if v := firstQuery(q, "connect_retry"); v != "" {
		parsed, err := strconv.ParseBool(v)
		if err != nil {
			return mqttConfig{}, fmt.Errorf("mqtt URI: connect_retry must be a boolean")
		}
		cfg.ConnectRetry = parsed
	}
	if v := firstQuery(q, "auto_reconnect"); v != "" {
		parsed, err := strconv.ParseBool(v)
		if err != nil {
			return mqttConfig{}, fmt.Errorf("mqtt URI: auto_reconnect must be a boolean")
		}
		cfg.AutoReconnect = parsed
	}
	if v := firstQuery(q, "connect_retry_interval"); v != "" {
		parsed, err := parseDuration(v)
		if err != nil || parsed <= 0 {
			return mqttConfig{}, fmt.Errorf("mqtt URI: connect_retry_interval must be a positive duration")
		}
		cfg.ConnectRetryInterval = parsed
	}

	return cfg, nil
}

func parseDuration(raw string) (time.Duration, error) {
	if seconds, err := strconv.Atoi(raw); err == nil {
		return time.Duration(seconds) * time.Second, nil
	}
	return time.ParseDuration(raw)
}

func firstQuery(values url.Values, keys ...string) string {
	for _, key := range keys {
		if v := values.Get(key); v != "" {
			return v
		}
	}
	return ""
}

func waitToken(ctx context.Context, token paho.Token, timeout time.Duration, action string) error {
	done := make(chan error, 1)
	go func() {
		token.Wait()
		done <- token.Error()
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		if err != nil {
			return fmt.Errorf("%s: %w", action, err)
		}
		return nil
	case <-timer.C:
		return fmt.Errorf("%s timed out after %s", action, timeout)
	}
}

func tableNameFromTopic(topic string) string {
	name := strings.Trim(topic, "/")
	if name == "" {
		return "mqtt_messages"
	}
	replacer := strings.NewReplacer("/", "_", "+", "plus", "#", "all")
	return replacer.Replace(name)
}

func sanitizeURI(raw string) string {
	if _, after, found := strings.Cut(raw, "@"); found {
		scheme := "mqtt"
		if before, _, hasScheme := strings.Cut(raw, "://"); hasScheme {
			scheme = before
		}
		return scheme + "://***@" + after
	}
	return raw
}

var (
	_ source.Source            = (*MQTTSource)(nil)
	_ source.StreamingSource   = (*MQTTSource)(nil)
	_ source.StreamCommitter   = (*MQTTSource)(nil)
	_ source.CDCBatchFinalizer = (*MQTTSource)(nil)
)
