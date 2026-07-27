package nats

import (
	"encoding/json"
	"net/url"
	"testing"
	"time"

	natsgo "github.com/nats-io/nats.go"
)

func TestParseNATSURI(t *testing.T) {
	cfg, err := parseNATSURI("nats://localhost:4222?stream=EVENTS&subject=events.*&durable=analytics&batch_size=100&batch_timeout=1.5&token=secret")
	if err != nil {
		t.Fatalf("parseNATSURI returned error: %v", err)
	}
	if cfg.URL != "nats://localhost:4222" {
		t.Errorf("URL = %q", cfg.URL)
	}
	if cfg.Stream != "EVENTS" {
		t.Errorf("Stream = %q", cfg.Stream)
	}
	if cfg.Subject != "events.*" {
		t.Errorf("Subject = %q", cfg.Subject)
	}
	if cfg.Durable != "analytics" {
		t.Errorf("Durable = %q", cfg.Durable)
	}
	if cfg.BindConsumer {
		t.Fatal("BindConsumer = true, want false for durable")
	}
	if cfg.BatchSize != 100 {
		t.Errorf("BatchSize = %d", cfg.BatchSize)
	}
	if cfg.BatchTimeout != 1500*time.Millisecond {
		t.Errorf("BatchTimeout = %v", cfg.BatchTimeout)
	}
	if cfg.Token != "secret" {
		t.Errorf("Token = %q", cfg.Token)
	}
}

func TestNATSSchemes(t *testing.T) {
	schemes := NewNATSSource().Schemes()
	if len(schemes) != 1 || schemes[0] != "nats" {
		t.Fatalf("Schemes = %v, want [nats]", schemes)
	}
	if _, err := parseNATSURI("tls://localhost:4222"); err == nil {
		t.Fatal("parseNATSURI accepted transport scheme as source scheme")
	}
}

func TestParseNATSURIConsumerBindsExistingConsumer(t *testing.T) {
	cfg, err := parseNATSURI("nats://localhost:4222?stream=EVENTS&consumer=existing")
	if err != nil {
		t.Fatalf("parseNATSURI returned error: %v", err)
	}
	if cfg.Durable != "existing" {
		t.Errorf("Durable = %q, want existing", cfg.Durable)
	}
	if !cfg.BindConsumer {
		t.Fatal("BindConsumer = false, want true for consumer")
	}
}

func TestParseNATSURIDefaultSubject(t *testing.T) {
	cfg, err := parseNATSURI("nats://localhost:4222")
	if err != nil {
		t.Fatalf("parseNATSURI returned error: %v", err)
	}
	if cfg.Subject != ">" {
		t.Errorf("Subject = %q, want >", cfg.Subject)
	}
}

func TestParseNATSURIValidation(t *testing.T) {
	tests := []struct {
		name string
		uri  string
	}{
		{name: "missing host", uri: "nats://?stream=EVENTS"},
		{name: "invalid bind", uri: "nats://localhost:4222?bind=sometimes"},
		{name: "invalid batch size", uri: "nats://localhost:4222?batch_size=0"},
		{name: "invalid batch timeout", uri: "nats://localhost:4222?batch_timeout=soon"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseNATSURI(tt.uri); err == nil {
				t.Fatalf("parseNATSURI(%q) returned no error", tt.uri)
			}
		})
	}
}

func TestNATSAckWaitUsesFlushInterval(t *testing.T) {
	if got := natsAckWait(5*time.Second, 30*time.Second); got < 90*time.Second {
		t.Errorf("natsAckWait = %v, want at least 90s", got)
	}
	if got := natsAckWait(time.Second, time.Second); got != 30*time.Second {
		t.Errorf("natsAckWait minimum = %v, want 30s", got)
	}
}

func TestSafeName(t *testing.T) {
	if got := safeName("ORDERS.events.>"); got != "ORDERS_events__" {
		t.Errorf("safeName returned %q", got)
	}
}

func TestTrackPendingUsesDeliveryOrder(t *testing.T) {
	src := NewNATSSource()
	first := &natsgo.Msg{Data: []byte("first")}
	second := &natsgo.Msg{Data: []byte("second")}

	if seq := src.trackPending(first); seq != 1 {
		t.Fatalf("first pending sequence = %d, want 1", seq)
	}
	if seq := src.trackPending(second); seq != 2 {
		t.Fatalf("second pending sequence = %d, want 2", seq)
	}
	if src.pending[1] != first || src.pending[2] != second {
		t.Fatal("pending messages were not tracked in delivery order")
	}
}

func TestDecodeMaybeJSONPreservesLargeNumbers(t *testing.T) {
	decoded, ok := decodeMaybeJSON([]byte(`{"id":9007199254740993}`)).(map[string]any)
	if !ok {
		t.Fatalf("decodeMaybeJSON returned %T, want map[string]any", decoded)
	}
	if got, ok := decoded["id"].(json.Number); !ok || got.String() != "9007199254740993" {
		t.Fatalf("decoded id = %#v, want json.Number with exact value", decoded["id"])
	}
}

func TestSanitizeNATSURL(t *testing.T) {
	sanitized := sanitizeNATSURL("nats://user:password@localhost:4222")
	u, err := url.Parse(sanitized)
	if err != nil {
		t.Fatalf("failed to parse sanitized URL: %v", err)
	}
	if got := u.User.Username(); got != "***" {
		t.Fatalf("sanitized username = %q, want ***", got)
	}
	if _, ok := u.User.Password(); ok {
		t.Fatal("sanitized URL still contains a password")
	}
}
