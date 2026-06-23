package nats

import (
	"testing"
	"time"
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
