package kafka

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/source"
	kafkago "github.com/segmentio/kafka-go"
	"golang.org/x/crypto/sha3"
)

func TestParseKafkaURI(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		wantErr bool
		check   func(t *testing.T, cfg kafkaConfig)
	}{
		{
			name: "valid full URI",
			uri:  "kafka://?bootstrap_servers=localhost:9092&group_id=my-group&security_protocol=SASL_SSL&sasl_mechanisms=PLAIN&sasl_username=user&sasl_password=pass",
			check: func(t *testing.T, cfg kafkaConfig) {
				if cfg.BootstrapServers != "localhost:9092" {
					t.Errorf("BootstrapServers = %q, want %q", cfg.BootstrapServers, "localhost:9092")
				}
				if cfg.GroupID != "my-group" {
					t.Errorf("GroupID = %q, want %q", cfg.GroupID, "my-group")
				}
				if cfg.SecurityProtocol != "SASL_SSL" {
					t.Errorf("SecurityProtocol = %q, want %q", cfg.SecurityProtocol, "SASL_SSL")
				}
				if cfg.SASLMechanism != "PLAIN" {
					t.Errorf("SASLMechanism = %q, want %q", cfg.SASLMechanism, "PLAIN")
				}
				if cfg.SASLUsername != "user" {
					t.Errorf("SASLUsername = %q, want %q", cfg.SASLUsername, "user")
				}
				if cfg.SASLPassword != "pass" {
					t.Errorf("SASLPassword = %q, want %q", cfg.SASLPassword, "pass")
				}
				if cfg.BatchSize != defaultBatchSize {
					t.Errorf("BatchSize = %d, want %d", cfg.BatchSize, defaultBatchSize)
				}
				if cfg.BatchTimeout != defaultBatchTimeout {
					t.Errorf("BatchTimeout = %v, want %v", cfg.BatchTimeout, defaultBatchTimeout)
				}
			},
		},
		{
			name: "minimal valid URI",
			uri:  "kafka://?bootstrap_servers=broker1:9092,broker2:9092&group_id=g1",
			check: func(t *testing.T, cfg kafkaConfig) {
				if cfg.BootstrapServers != "broker1:9092,broker2:9092" {
					t.Errorf("BootstrapServers = %q, want %q", cfg.BootstrapServers, "broker1:9092,broker2:9092")
				}
				if cfg.GroupID != "g1" {
					t.Errorf("GroupID = %q, want %q", cfg.GroupID, "g1")
				}
			},
		},
		{
			name: "custom batch_size and batch_timeout",
			uri:  "kafka://?bootstrap_servers=localhost:9092&group_id=g1&batch_size=500&batch_timeout=10",
			check: func(t *testing.T, cfg kafkaConfig) {
				if cfg.BatchSize != 500 {
					t.Errorf("BatchSize = %d, want 500", cfg.BatchSize)
				}
				if cfg.BatchTimeout != 10*time.Second {
					t.Errorf("BatchTimeout = %v, want %v", cfg.BatchTimeout, 10*time.Second)
				}
			},
		},
		{
			name: "OAUTHBEARER MSK IAM URI",
			uri:  "kafka://?bootstrap_servers=b1:9098&group_id=g1&sasl_mechanisms=OAUTHBEARER&aws_region=us-east-1&aws_role_arn=arn:aws:iam::123456789012:role/msk&aws_role_session_name=ingestr&aws_profile=default&aws_access_key_id=AKID&aws_secret_access_key=SECRET&aws_session_token=TOKEN",
			check: func(t *testing.T, cfg kafkaConfig) {
				if cfg.SASLMechanism != "OAUTHBEARER" {
					t.Errorf("SASLMechanism = %q, want %q", cfg.SASLMechanism, "OAUTHBEARER")
				}
				if cfg.AWSRegion != "us-east-1" {
					t.Errorf("AWSRegion = %q, want %q", cfg.AWSRegion, "us-east-1")
				}
				if cfg.AWSRoleArn != "arn:aws:iam::123456789012:role/msk" {
					t.Errorf("AWSRoleArn = %q, want %q", cfg.AWSRoleArn, "arn:aws:iam::123456789012:role/msk")
				}
				if cfg.AWSRoleSessionName != "ingestr" {
					t.Errorf("AWSRoleSessionName = %q, want %q", cfg.AWSRoleSessionName, "ingestr")
				}
				if cfg.AWSProfile != "default" {
					t.Errorf("AWSProfile = %q, want %q", cfg.AWSProfile, "default")
				}
				if cfg.AWSAccessKeyID != "AKID" {
					t.Errorf("AWSAccessKeyID = %q, want %q", cfg.AWSAccessKeyID, "AKID")
				}
				if cfg.AWSSecretAccessKey != "SECRET" {
					t.Errorf("AWSSecretAccessKey = %q, want %q", cfg.AWSSecretAccessKey, "SECRET")
				}
				if cfg.AWSSessionToken != "TOKEN" {
					t.Errorf("AWSSessionToken = %q, want %q", cfg.AWSSessionToken, "TOKEN")
				}
			},
		},
		{
			name:    "wrong scheme",
			uri:     "postgres://localhost:9092",
			wantErr: true,
		},
		{
			name:    "empty URI",
			uri:     "kafka://",
			wantErr: true,
		},
		{
			name:    "missing bootstrap_servers",
			uri:     "kafka://?group_id=g1",
			wantErr: true,
		},
		{
			name:    "missing group_id",
			uri:     "kafka://?bootstrap_servers=localhost:9092",
			wantErr: true,
		},
		{
			name:    "invalid batch_size",
			uri:     "kafka://?bootstrap_servers=localhost:9092&group_id=g1&batch_size=abc",
			wantErr: true,
		},
		{
			name:    "zero batch_size",
			uri:     "kafka://?bootstrap_servers=localhost:9092&group_id=g1&batch_size=0",
			wantErr: true,
		},
		{
			name:    "negative batch_size",
			uri:     "kafka://?bootstrap_servers=localhost:9092&group_id=g1&batch_size=-1",
			wantErr: true,
		},
		{
			name:    "invalid batch_timeout",
			uri:     "kafka://?bootstrap_servers=localhost:9092&group_id=g1&batch_timeout=abc",
			wantErr: true,
		},
		{
			name:    "zero batch_timeout",
			uri:     "kafka://?bootstrap_servers=localhost:9092&group_id=g1&batch_timeout=0",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := parseKafkaURI(tt.uri)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}

func shake128Base64(input string) string {
	h := sha3.NewShake128()
	h.Write([]byte(input))
	digest := make([]byte, 15)
	_, _ = h.Read(digest)
	return strings.TrimRight(base64.StdEncoding.EncodeToString(digest), "=")
}

func TestPhysicalMsgID(t *testing.T) {
	id1 := physicalMsgID("my-topic", 0, 100)
	id2 := physicalMsgID("my-topic", 0, 100)
	if id1 != id2 {
		t.Errorf("physicalMsgID not deterministic: %q != %q", id1, id2)
	}

	// SHAKE-128 → 15 bytes → base64 without padding = 20 chars
	if len(id1) != 20 {
		t.Errorf("physicalMsgID length = %d, want 20", len(id1))
	}

	// Verify it matches SHAKE-128 base64 of the injective "topic:partition:offset"
	// coordinate — the message's unique location within the topic.
	expected := shake128Base64("my-topic:0:100")
	if id1 != expected {
		t.Errorf("physicalMsgID = %q, want %q", id1, expected)
	}

	// The regression this fix targets: ids differing only by offset must be
	// distinct (a null-key stream on one partition previously collapsed to one id).
	idOff := physicalMsgID("my-topic", 0, 101)
	if id1 == idOff {
		t.Error("physicalMsgID collision for different offsets")
	}

	// The input encoding must be injective across the (partition, offset) split:
	// partition 1/offset 10 and partition 11/offset 0 are different messages and
	// must not produce the same id (a separator-less encoding would collide here).
	if a, b := physicalMsgID("t", 1, 10), physicalMsgID("t", 11, 0); a == b {
		t.Errorf("physicalMsgID collision across partition/offset boundary: %q", a)
	}

	if id3 := physicalMsgID("my-topic", 1, 100); id1 == id3 {
		t.Error("physicalMsgID collision for different partitions")
	}
	if id4 := physicalMsgID("other-topic", 0, 100); id1 == id4 {
		t.Error("physicalMsgID collision for different topics")
	}
}

func TestKeyedMsgID(t *testing.T) {
	id1 := keyedMsgID("my-topic", 0, "key1")
	id2 := keyedMsgID("my-topic", 0, "key1")
	if id1 != id2 {
		t.Errorf("keyedMsgID not deterministic: %q != %q", id1, id2)
	}

	if len(id1) != 20 {
		t.Errorf("keyedMsgID length = %d, want 20", len(id1))
	}

	// Stable per key (topic + partition + key), offset excluded — this is the
	// changelog/compaction identity used by the streaming envelope's keyed branch.
	expected := shake128Base64(fmt.Sprintf("%s%d%s", "my-topic", 0, "key1"))
	if id1 != expected {
		t.Errorf("keyedMsgID = %q, want %q", id1, expected)
	}

	if id3 := keyedMsgID("my-topic", 1, "key1"); id1 == id3 {
		t.Error("keyedMsgID collision for different partitions")
	}
	if id4 := keyedMsgID("other-topic", 0, "key1"); id1 == id4 {
		t.Error("keyedMsgID collision for different topics")
	}
	if id5 := keyedMsgID("my-topic", 0, "key2"); id1 == id5 {
		t.Error("keyedMsgID collision for different keys")
	}
}

func TestStreamMsgID(t *testing.T) {
	// Keyless messages must get distinct ids per offset (otherwise the merge
	// envelope collapses every keyless message on a partition into one row).
	a := streamMsgID(kafkago.Message{Topic: "t", Partition: 0, Offset: 1})
	b := streamMsgID(kafkago.Message{Topic: "t", Partition: 0, Offset: 2})
	if a == b {
		t.Errorf("keyless messages at different offsets share msg_id: %q", a)
	}
	// Same offset (e.g. a redelivery) is stable so at-least-once dedup works.
	if a2 := streamMsgID(kafkago.Message{Topic: "t", Partition: 0, Offset: 1}); a != a2 {
		t.Errorf("keyless msg_id not stable for the same offset: %q != %q", a, a2)
	}

	// Keyed messages dedup by key regardless of offset (last-value-per-key).
	k1 := streamMsgID(kafkago.Message{Topic: "t", Partition: 0, Offset: 10, Key: []byte("k")})
	k2 := streamMsgID(kafkago.Message{Topic: "t", Partition: 0, Offset: 11, Key: []byte("k")})
	if k1 != k2 {
		t.Errorf("keyed messages with the same key should share msg_id: %q != %q", k1, k2)
	}
	if kOther := streamMsgID(kafkago.Message{Topic: "t", Partition: 0, Offset: 12, Key: []byte("k2")}); k1 == kOther {
		t.Error("different keys should produce different msg_ids")
	}
}

func TestMessageToItem(t *testing.T) {
	ts := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	msg := kafkago.Message{
		Topic:     "test-topic",
		Partition: 2,
		Offset:    42,
		Key:       []byte("msg-key"),
		Value:     []byte(`{"user": "alice"}`),
		Time:      ts,
	}

	item := messageToItem(msg)

	msgID, ok := item["_kafka_msg_id"].(string)
	if !ok || len(msgID) != 20 {
		t.Errorf("_kafka_msg_id = %v (len=%d), want 20-char string", item["_kafka_msg_id"], len(msgID))
	}

	meta, ok := item["_kafka"].(map[string]interface{})
	if !ok {
		t.Fatal("_kafka metadata is not a map")
	}
	if meta["partition"] != 2 {
		t.Errorf("partition = %v, want 2", meta["partition"])
	}
	if meta["topic"] != "test-topic" {
		t.Errorf("topic = %v, want test-topic", meta["topic"])
	}
	if meta["key"] != "msg-key" {
		t.Errorf("key = %v, want msg-key", meta["key"])
	}
	if meta["offset"] != int64(42) {
		t.Errorf("offset = %v, want 42", meta["offset"])
	}
	if meta["data"] != `{"user": "alice"}` {
		t.Errorf("data = %v, want {\"user\": \"alice\"}", meta["data"])
	}

	tsMeta, ok := meta["ts"].(map[string]interface{})
	if !ok {
		t.Fatal("ts metadata is not a map")
	}
	if tsMeta["type"] != 1 {
		t.Errorf("ts.type = %v, want 1 (TIMESTAMP_CREATE_TIME)", tsMeta["type"])
	}
	tsStr, ok := tsMeta["value"].(string)
	if !ok {
		t.Fatalf("ts.value type = %T, want string", tsMeta["value"])
	}
	if tsStr != ts.Format(time.RFC3339Nano) {
		t.Errorf("ts.value = %q, want %q", tsStr, ts.Format(time.RFC3339Nano))
	}
}

func TestMessageToItem_NilKey(t *testing.T) {
	msg := kafkago.Message{
		Topic:     "test-topic",
		Partition: 0,
		Offset:    0,
		Key:       nil,
		Value:     []byte("data"),
	}

	item := messageToItem(msg)
	meta := item["_kafka"].(map[string]interface{})

	if meta["key"] != nil {
		t.Errorf("key = %v, want nil", meta["key"])
	}

	tsMeta := meta["ts"].(map[string]interface{})
	if tsMeta["type"] != 0 {
		t.Errorf("ts.type = %v, want 0 for zero time", tsMeta["type"])
	}
}

// Keyless messages on one partition differing only by offset must produce
// distinct _kafka_msg_id values; previously they all collapsed to one id.
func TestMessageToItem_KeylessDistinctPerOffset(t *testing.T) {
	base := kafkago.Message{Topic: "test-topic", Partition: 0, Key: nil, Value: []byte("data")}

	seen := make(map[string]int64)
	for off := int64(0); off < 500; off++ {
		msg := base
		msg.Offset = off
		id, ok := messageToItem(msg)["_kafka_msg_id"].(string)
		if !ok {
			t.Fatalf("offset %d: _kafka_msg_id is not a string", off)
		}
		if prev, dup := seen[id]; dup {
			t.Fatalf("offset %d and %d collide on _kafka_msg_id %q", prev, off, id)
		}
		seen[id] = off
	}
}

func TestBuildDialer_NoAuth(t *testing.T) {
	s := &KafkaSource{
		cfg: kafkaConfig{
			BootstrapServers: "localhost:9092",
			GroupID:          "test",
		},
	}

	dialer, err := s.buildDialer()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dialer.TLS != nil {
		t.Error("TLS should be nil without security protocol")
	}
	if dialer.SASLMechanism != nil {
		t.Error("SASL mechanism should be nil without SASL config")
	}
}

func TestBuildDialer_SSL(t *testing.T) {
	s := &KafkaSource{
		cfg: kafkaConfig{
			BootstrapServers: "localhost:9092",
			GroupID:          "test",
			SecurityProtocol: "SSL",
		},
	}

	dialer, err := s.buildDialer()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dialer.TLS == nil {
		t.Error("TLS should be set for SSL protocol")
	}
}

func TestBuildDialer_SASL_SSL(t *testing.T) {
	s := &KafkaSource{
		cfg: kafkaConfig{
			BootstrapServers: "localhost:9092",
			GroupID:          "test",
			SecurityProtocol: "SASL_SSL",
			SASLMechanism:    "PLAIN",
			SASLUsername:     "user",
			SASLPassword:     "pass",
		},
	}

	dialer, err := s.buildDialer()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dialer.TLS == nil {
		t.Error("TLS should be set for SASL_SSL protocol")
	}
	if dialer.SASLMechanism == nil {
		t.Error("SASL mechanism should be set")
	}
}

func TestBuildDialer_OAuthBearer(t *testing.T) {
	s := &KafkaSource{
		cfg: kafkaConfig{
			BootstrapServers: "b1:9098",
			GroupID:          "test",
			SASLMechanism:    "OAUTHBEARER",
			AWSRegion:        "us-east-1",
		},
	}

	dialer, err := s.buildDialer()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dialer.SASLMechanism == nil {
		t.Fatal("SASL mechanism should be set for OAUTHBEARER")
	}
	if dialer.SASLMechanism.Name() != "OAUTHBEARER" {
		t.Errorf("mechanism name = %q, want OAUTHBEARER", dialer.SASLMechanism.Name())
	}
	if dialer.TLS == nil {
		t.Error("TLS should be auto-enabled for OAUTHBEARER")
	}
}

func TestBuildDialer_OAuthBearer_MissingRegion(t *testing.T) {
	s := &KafkaSource{
		cfg: kafkaConfig{
			BootstrapServers: "b1:9098",
			GroupID:          "test",
			SASLMechanism:    "OAUTHBEARER",
		},
	}

	oldDebugMode := config.DebugMode
	config.DebugMode = true
	t.Cleanup(func() {
		config.DebugMode = oldDebugMode
	})

	output := captureStdout(t, func() {
		if _, err := s.buildDialer(); err == nil {
			t.Fatal("expected error for OAUTHBEARER without aws_region")
		}
	})
	if strings.Contains(output, "TLS auto-enabled") {
		t.Fatalf("debug output = %q, should not log TLS auto-enable for invalid OAUTHBEARER config", output)
	}
}

func TestBuildDialer_OAuthBearer_PlaintextEscapeHatch(t *testing.T) {
	s := &KafkaSource{
		cfg: kafkaConfig{
			BootstrapServers: "b1:9092",
			GroupID:          "test",
			SASLMechanism:    "OAUTHBEARER",
			SecurityProtocol: "SASL_PLAINTEXT",
			AWSRegion:        "us-east-1",
		},
	}

	dialer, err := s.buildDialer()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dialer.SASLMechanism == nil {
		t.Fatal("SASL mechanism should be set for OAUTHBEARER")
	}
	if dialer.TLS != nil {
		t.Error("TLS should not be auto-enabled when SASL_PLAINTEXT is explicitly set")
	}
}

func TestBuildSASLMechanism(t *testing.T) {
	tests := []struct {
		name    string
		cfg     kafkaConfig
		wantErr bool
	}{
		{name: "PLAIN", cfg: kafkaConfig{SASLMechanism: "PLAIN", SASLUsername: "user", SASLPassword: "pass"}},
		{name: "SCRAM-SHA-256", cfg: kafkaConfig{SASLMechanism: "SCRAM-SHA-256", SASLUsername: "user", SASLPassword: "pass"}},
		{name: "SCRAM-SHA-512", cfg: kafkaConfig{SASLMechanism: "SCRAM-SHA-512", SASLUsername: "user", SASLPassword: "pass"}},
		{name: "lowercase plain", cfg: kafkaConfig{SASLMechanism: "plain", SASLUsername: "user", SASLPassword: "pass"}},
		{name: "PLAIN missing username", cfg: kafkaConfig{SASLMechanism: "PLAIN", SASLPassword: "pass"}, wantErr: true},
		{name: "PLAIN missing password", cfg: kafkaConfig{SASLMechanism: "PLAIN", SASLUsername: "user"}, wantErr: true},
		{name: "SCRAM-SHA-256 missing username", cfg: kafkaConfig{SASLMechanism: "SCRAM-SHA-256", SASLPassword: "pass"}, wantErr: true},
		{name: "SCRAM-SHA-256 missing password", cfg: kafkaConfig{SASLMechanism: "SCRAM-SHA-256", SASLUsername: "user"}, wantErr: true},
		{name: "SCRAM-SHA-512 missing username", cfg: kafkaConfig{SASLMechanism: "SCRAM-SHA-512", SASLPassword: "pass"}, wantErr: true},
		{name: "SCRAM-SHA-512 missing password", cfg: kafkaConfig{SASLMechanism: "SCRAM-SHA-512", SASLUsername: "user"}, wantErr: true},
		{name: "OAUTHBEARER", cfg: kafkaConfig{SASLMechanism: "OAUTHBEARER", AWSRegion: "us-east-1"}},
		{name: "OAUTHBEARER missing region", cfg: kafkaConfig{SASLMechanism: "OAUTHBEARER"}, wantErr: true},
		{name: "unsupported", cfg: kafkaConfig{SASLMechanism: "GSSAPI"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := buildSASLMechanism(tt.cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if m == nil {
				t.Fatal("mechanism is nil")
			}
		})
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = old
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close stdout pipe: %v", err)
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read stdout pipe: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}
	return buf.String()
}

func TestGetTable(t *testing.T) {
	s := NewKafkaSource()

	_, err := s.GetTable(context.Background(), source.TableRequest{Name: ""})
	if err == nil {
		t.Fatal("expected error for empty topic name")
	}

	table, err := s.GetTable(context.Background(), source.TableRequest{
		Name:        "my-topic",
		PrimaryKeys: []string{"_kafka_msg_id"},
		Strategy:    config.StrategyReplace,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if table.Name() != "my-topic" {
		t.Errorf("Name = %q, want %q", table.Name(), "my-topic")
	}
	if pks := table.PrimaryKeys(); len(pks) != 1 || pks[0] != "_kafka_msg_id" {
		t.Errorf("PrimaryKeys = %v, want [_kafka_msg_id]", pks)
	}
	if table.Strategy() != config.StrategyReplace {
		t.Errorf("Strategy = %v, want %v", table.Strategy(), config.StrategyReplace)
	}
	if table.HasKnownSchema() {
		t.Error("HasKnownSchema should be false")
	}
}

func TestConnect(t *testing.T) {
	s := NewKafkaSource()

	err := s.Connect(context.Background(), "kafka://?bootstrap_servers=localhost:9092&group_id=test-group")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.cfg.BootstrapServers != "localhost:9092" {
		t.Errorf("BootstrapServers = %q, want %q", s.cfg.BootstrapServers, "localhost:9092")
	}
	if s.cfg.GroupID != "test-group" {
		t.Errorf("GroupID = %q, want %q", s.cfg.GroupID, "test-group")
	}

	err = s.Connect(context.Background(), "invalid://uri")
	if err == nil {
		t.Fatal("expected error for invalid URI")
	}
}

func TestSchemes(t *testing.T) {
	s := NewKafkaSource()
	schemes := s.Schemes()
	if len(schemes) != 1 || schemes[0] != "kafka" {
		t.Errorf("Schemes = %v, want [kafka]", schemes)
	}
}

func TestHandlesIncrementality(t *testing.T) {
	s := NewKafkaSource()
	if s.HandlesIncrementality() {
		t.Error("HandlesIncrementality should be false")
	}
}

func TestIsRetriable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "retriable - LeaderNotAvailable", err: kafkago.LeaderNotAvailable, want: true},
		{name: "retriable - RequestTimedOut", err: kafkago.RequestTimedOut, want: true},
		{name: "retriable - NotEnoughReplicas", err: kafkago.NotEnoughReplicas, want: true},
		{name: "retriable - NetworkException", err: kafkago.NetworkException, want: true},
		{name: "non-retriable - TopicAuthorizationFailed", err: kafkago.TopicAuthorizationFailed, want: false},
		{name: "non-retriable - InvalidMessageSize", err: kafkago.InvalidMessageSize, want: false},
		{name: "non-retriable - SASLAuthenticationFailed", err: kafkago.SASLAuthenticationFailed, want: false},
		{name: "transient - ECONNREFUSED", err: syscall.ECONNREFUSED, want: true},
		{name: "transient - ECONNRESET", err: syscall.ECONNRESET, want: true},
		{name: "transient - EPIPE", err: syscall.EPIPE, want: true},
		{name: "transient - ErrUnexpectedEOF", err: io.ErrUnexpectedEOF, want: true},
		{name: "wrapped retriable", err: fmt.Errorf("wrapped: %w", kafkago.LeaderNotAvailable), want: true},
		{name: "wrapped transient", err: fmt.Errorf("conn: %w", syscall.ECONNRESET), want: true},
		{name: "generic error", err: errors.New("something broke"), want: false},
		{name: "EOF", err: io.EOF, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRetriable(tt.err)
			if got != tt.want {
				t.Errorf("isRetriable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
