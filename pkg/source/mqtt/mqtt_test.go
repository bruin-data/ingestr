package mqtt

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	sourcepkg "github.com/bruin-data/ingestr/pkg/source"
)

func TestParseMQTTURI(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		wantErr bool
		check   func(t *testing.T, cfg mqttConfig)
	}{
		{
			name: "full mqtt uri",
			uri:  "mqtt://user:pass@localhost:1883?client_id=ingestr-test&qos=2&clean_session=false&batch_size=25&batch_timeout=1500ms&connect_timeout=2&keep_alive=10s&auto_reconnect=true&connect_retry=true",
			check: func(t *testing.T, cfg mqttConfig) {
				if cfg.BrokerURL != "tcp://localhost:1883" {
					t.Fatalf("BrokerURL = %q", cfg.BrokerURL)
				}
				if cfg.ClientID != "ingestr-test" {
					t.Fatalf("ClientID = %q", cfg.ClientID)
				}
				if cfg.Username != "user" || cfg.Password != "pass" {
					t.Fatalf("credentials = %q/%q", cfg.Username, cfg.Password)
				}
				if cfg.QOS != 2 {
					t.Fatalf("QOS = %d", cfg.QOS)
				}
				if cfg.CleanSession {
					t.Fatal("CleanSession should be false")
				}
				if cfg.BatchSize != 25 {
					t.Fatalf("BatchSize = %d", cfg.BatchSize)
				}
				if cfg.BatchTimeout != 1500*time.Millisecond {
					t.Fatalf("BatchTimeout = %s", cfg.BatchTimeout)
				}
				if cfg.ConnectTimeout != 2*time.Second {
					t.Fatalf("ConnectTimeout = %s", cfg.ConnectTimeout)
				}
				if cfg.KeepAlive != 10*time.Second {
					t.Fatalf("KeepAlive = %s", cfg.KeepAlive)
				}
				if !cfg.AutoReconnect || !cfg.ConnectRetry {
					t.Fatal("reconnect flags should be true")
				}
			},
		},
		{
			name: "mqtts defaults",
			uri:  "mqtts://example.com?client_id=secure&insecure_skip_verify=true",
			check: func(t *testing.T, cfg mqttConfig) {
				if cfg.BrokerURL != "ssl://example.com:8883" {
					t.Fatalf("BrokerURL = %q", cfg.BrokerURL)
				}
				if !cfg.InsecureSkipVerify {
					t.Fatal("InsecureSkipVerify should be true")
				}
			},
		},
		{name: "invalid scheme", uri: "http://localhost:1883", wantErr: true},
		{name: "missing host", uri: "mqtt://?client_id=x", wantErr: true},
		{name: "invalid qos", uri: "mqtt://localhost:1883?client_id=x&qos=3", wantErr: true},
		{name: "persistent session requires client id", uri: "mqtt://localhost:1883?clean_session=false", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := parseMQTTURI(tt.uri)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
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

func TestStreamingTableDefaultsToMerge(t *testing.T) {
	src := NewMQTTSource()
	table, err := src.GetTable(context.Background(), sourcepkg.TableRequest{
		Name:      "events/#",
		Streaming: true,
	})
	if err != nil {
		t.Fatalf("GetTable returned error: %v", err)
	}
	if got := table.Strategy(); got != config.StrategyMerge {
		t.Fatalf("Strategy = %q, want %q", got, config.StrategyMerge)
	}
	pks := table.PrimaryKeys()
	if len(pks) != 1 || pks[0] != "msg_id" {
		t.Fatalf("PrimaryKeys = %v, want [msg_id]", pks)
	}
}

func TestMessageToItemPreservesMessageID(t *testing.T) {
	msg := fakeMessage{
		topic:     "sensors/temp",
		qos:       1,
		messageID: 42,
		payload:   []byte(`{"device":"a","value":21.5}`),
	}

	item := messageToItem(msg, 7)
	if item["msg_id"] == "" {
		t.Fatalf("msg_id = %v", item["msg_id"])
	}
	if item["message_id"] != int64(42) {
		t.Fatalf("message_id = %v", item["message_id"])
	}
	if item[streamOrderColumn] != int64(7) {
		t.Fatalf("%s = %v", streamOrderColumn, item[streamOrderColumn])
	}
	data, ok := item["data"].(map[string]any)
	if !ok {
		t.Fatalf("data type = %T", item["data"])
	}
	if data["device"] != "a" {
		t.Fatalf("device = %v", data["device"])
	}

	envelope := messageToEnvelope(msg, 7)
	if envelope["message_id"] != int64(42) {
		t.Fatalf("envelope message_id = %v", envelope["message_id"])
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(envelope["data"].(string)), &decoded); err != nil {
		t.Fatalf("invalid envelope data JSON: %v", err)
	}
	metadata := decoded["metadata"].(map[string]any)
	if metadata["message_id"] != float64(42) {
		t.Fatalf("metadata message_id = %v", metadata["message_id"])
	}
}

func TestMQTTMessageIDUsesFingerprintAndSequenceWhenPacketIDExists(t *testing.T) {
	msg := fakeMessage{
		topic:     "sensors/temp",
		qos:       1,
		messageID: 42,
		payload:   []byte(`{"device":"a","value":21.5}`),
	}
	redelivery := msg
	redelivery.duplicate = true
	differentPayload := msg
	differentPayload.payload = []byte(`{"device":"a","value":22.0}`)

	msgID, numericID := mqttMsgID(msg, 7)
	redeliveryID, _ := mqttMsgID(redelivery, 7)
	otherID, _ := mqttMsgID(differentPayload, 8)
	reusedPacketID, _ := mqttMsgID(msg, 8)

	if numericID != int64(42) {
		t.Fatalf("numericID = %v, want 42", numericID)
	}
	if msgID != redeliveryID {
		t.Fatalf("redelivery msg_id changed: %q != %q", msgID, redeliveryID)
	}
	if msgID == otherID {
		t.Fatalf("different payload with reused packet ID produced same msg_id: %q", msgID)
	}
	if msgID == reusedPacketID {
		t.Fatalf("reused packet ID after ack should produce a new msg_id: %q", msgID)
	}
}

func TestMessageToItemGeneratesStableIDWhenPacketIDMissing(t *testing.T) {
	msg := fakeMessage{
		topic:    "sensors/temp",
		qos:      0,
		retained: true,
		payload:  []byte("plain"),
	}

	item := messageToItem(msg, 1)
	if item["message_id"] != nil {
		t.Fatalf("message_id = %v, want nil", item["message_id"])
	}
	if item["msg_id"] == "" {
		t.Fatal("msg_id should be generated")
	}
	if item["data"] != "plain" {
		t.Fatalf("data = %v", item["data"])
	}

	again := messageToItem(msg, 1)
	if item["msg_id"] != again["msg_id"] {
		t.Fatalf("generated msg_id should be deterministic for same seq: %v != %v", item["msg_id"], again["msg_id"])
	}
	otherSeq := messageToItem(msg, 2)
	if item["msg_id"] != otherSeq["msg_id"] {
		t.Fatalf("generated msg_id should be stable across reads: %v != %v", item["msg_id"], otherSeq["msg_id"])
	}
	otherPayload := msg
	otherPayload.payload = []byte("other")
	if item["msg_id"] == messageToItem(otherPayload, 2)["msg_id"] {
		t.Fatal("generated msg_id should include payload fingerprint")
	}

	live := msg
	live.retained = false
	if messageToItem(live, 1)["msg_id"] == messageToItem(live, 2)["msg_id"] {
		t.Fatal("live zero-packet-ID messages should include sequence")
	}
}

func TestFinalizeBatchAcksPendingMessages(t *testing.T) {
	src := NewMQTTSource()
	msg1 := &ackMessage{fakeMessage: fakeMessage{topic: "sensors/temp", messageID: 1}}
	msg2 := &ackMessage{fakeMessage: fakeMessage{topic: "sensors/temp", messageID: 2}}

	src.trackMessage(msg1)
	src.trackMessage(msg2)
	if msg1.ackCount != 0 || msg2.ackCount != 0 {
		t.Fatalf("messages were acked before finalize: %d, %d", msg1.ackCount, msg2.ackCount)
	}

	if err := src.FinalizeBatch(context.Background()); err != nil {
		t.Fatalf("FinalizeBatch returned error: %v", err)
	}
	if msg1.ackCount != 1 || msg2.ackCount != 1 {
		t.Fatalf("ack counts = %d, %d; want 1, 1", msg1.ackCount, msg2.ackCount)
	}

	if err := src.FinalizeBatch(context.Background()); err != nil {
		t.Fatalf("second FinalizeBatch returned error: %v", err)
	}
	if msg1.ackCount != 1 || msg2.ackCount != 1 {
		t.Fatalf("second finalize ack counts = %d, %d; want 1, 1", msg1.ackCount, msg2.ackCount)
	}
}

func TestTrackMessageReusesPendingSequenceForRedelivery(t *testing.T) {
	src := NewMQTTSource()
	msg := &ackMessage{fakeMessage: fakeMessage{topic: "sensors/temp", qos: 1, messageID: 1, payload: []byte("a")}}
	redelivery := &ackMessage{fakeMessage: msg.fakeMessage}
	redelivery.duplicate = true

	seq := src.trackMessage(msg)
	redeliverySeq := src.trackMessage(redelivery)
	if seq != redeliverySeq {
		t.Fatalf("redelivery sequence = %d, want %d", redeliverySeq, seq)
	}

	if err := src.CommitStream(context.Background(), mqttCommitToken{MaxSeq: seq}); err != nil {
		t.Fatalf("CommitStream returned error: %v", err)
	}
	if msg.ackCount != 0 || redelivery.ackCount != 1 {
		t.Fatalf("ack counts = %d, %d; want latest redelivery only", msg.ackCount, redelivery.ackCount)
	}

	reusedSeq := src.trackMessage(msg)
	if reusedSeq == seq {
		t.Fatalf("reused packet ID after ack kept old sequence %d", seq)
	}
}

func TestTrackMessageDistinguishesPacketIDsForPendingRedelivery(t *testing.T) {
	src := NewMQTTSource()
	msg1 := &ackMessage{fakeMessage: fakeMessage{topic: "sensors/temp", qos: 1, messageID: 1, payload: []byte("a")}}
	msg2 := &ackMessage{fakeMessage: fakeMessage{topic: "sensors/temp", qos: 1, messageID: 2, payload: []byte("a")}}

	seq1 := src.trackMessage(msg1)
	seq2 := src.trackMessage(msg2)
	if seq1 == seq2 {
		t.Fatalf("different packet IDs reused pending sequence %d", seq1)
	}

	redelivery := &ackMessage{fakeMessage: msg1.fakeMessage}
	redelivery.duplicate = true
	redeliverySeq := src.trackMessage(redelivery)
	if redeliverySeq != seq1 {
		t.Fatalf("redelivery sequence = %d, want %d", redeliverySeq, seq1)
	}
}

func TestCommitStreamAcksThroughToken(t *testing.T) {
	src := NewMQTTSource()
	msg1 := &ackMessage{fakeMessage: fakeMessage{topic: "sensors/temp", messageID: 1}}
	msg2 := &ackMessage{fakeMessage: fakeMessage{topic: "sensors/temp", messageID: 2}}
	msg3 := &ackMessage{fakeMessage: fakeMessage{topic: "sensors/temp", messageID: 3}}

	src.trackMessage(msg1)
	src.trackMessage(msg2)
	src.trackMessage(msg3)

	if err := src.CommitStream(context.Background(), mqttCommitToken{MaxSeq: 2}); err != nil {
		t.Fatalf("CommitStream returned error: %v", err)
	}
	if msg1.ackCount != 1 || msg2.ackCount != 1 || msg3.ackCount != 0 {
		t.Fatalf("ack counts = %d, %d, %d; want 1, 1, 0", msg1.ackCount, msg2.ackCount, msg3.ackCount)
	}

	if err := src.CommitStream(context.Background(), mqttCommitToken{MaxSeq: 3}); err != nil {
		t.Fatalf("second CommitStream returned error: %v", err)
	}
	if msg3.ackCount != 1 {
		t.Fatalf("msg3 ack count = %d, want 1", msg3.ackCount)
	}
}

type fakeMessage struct {
	topic     string
	qos       byte
	retained  bool
	duplicate bool
	messageID uint16
	payload   []byte
}

func (m fakeMessage) Duplicate() bool {
	return m.duplicate
}

func (m fakeMessage) Qos() byte {
	return m.qos
}

func (m fakeMessage) Retained() bool {
	return m.retained
}

func (m fakeMessage) Topic() string {
	return m.topic
}

func (m fakeMessage) MessageID() uint16 {
	return m.messageID
}

func (m fakeMessage) Payload() []byte {
	return m.payload
}

func (m fakeMessage) Ack() {}

type ackMessage struct {
	fakeMessage
	ackCount int
}

func (m *ackMessage) Ack() {
	m.ackCount++
}
