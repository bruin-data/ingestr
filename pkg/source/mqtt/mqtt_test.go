package mqtt

import (
	"encoding/json"
	"testing"
	"time"
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

func TestMessageToItemPreservesMessageID(t *testing.T) {
	msg := fakeMessage{
		topic:     "sensors/temp",
		qos:       1,
		messageID: 42,
		payload:   []byte(`{"device":"a","value":21.5}`),
	}

	item := messageToItem(msg, 7)
	if item["msg_id"] != "sensors/temp:42" {
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

func TestMessageToItemGeneratesIDWhenPacketIDMissing(t *testing.T) {
	msg := fakeMessage{
		topic:   "sensors/temp",
		qos:     0,
		payload: []byte("plain"),
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
	if item["msg_id"] == otherSeq["msg_id"] {
		t.Fatal("generated msg_id should include sequence to avoid same-payload collisions")
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
