package redis

import (
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

func TestParseRedisURI(t *testing.T) {
	cfg, clientURL, err := parseRedisURI("redis://localhost:6379/0?batch_size=100&batch_timeout=1.5&claim_min_idle=12&group=analytics&consumer=worker-1")
	if err != nil {
		t.Fatalf("parseRedisURI returned error: %v", err)
	}
	if cfg.Group != "analytics" {
		t.Errorf("Group = %q, want analytics", cfg.Group)
	}
	if cfg.Consumer != "worker-1" {
		t.Errorf("Consumer = %q, want worker-1", cfg.Consumer)
	}
	if cfg.BatchSize != 100 {
		t.Errorf("BatchSize = %d, want 100", cfg.BatchSize)
	}
	if cfg.BatchTimeout.String() != "1.5s" {
		t.Errorf("BatchTimeout = %s, want 1.5s", cfg.BatchTimeout)
	}
	if cfg.ClaimMinIdle != 12*time.Second {
		t.Errorf("ClaimMinIdle = %s, want 12s", cfg.ClaimMinIdle)
	}
	if clientURL != "redis://localhost:6379/0" {
		t.Errorf("clientURL = %q", clientURL)
	}
}

func TestRedisClaimMinIdle(t *testing.T) {
	if got := redisClaimMinIdle(0, time.Minute, time.Second); got != 0 {
		t.Errorf("explicit zero ClaimMinIdle = %v, want 0", got)
	}
	if got := redisClaimMinIdle(-1, time.Second, time.Second); got != defaultFlushInterval {
		t.Errorf("minimum ClaimMinIdle = %v, want %v", got, defaultFlushInterval)
	}
	if got := redisClaimMinIdle(-1, time.Minute, time.Second); got != 181*time.Second {
		t.Errorf("computed ClaimMinIdle = %v, want 181s", got)
	}
}

func TestCompareRedisIDs(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1-0", "1-0", 0},
		{"1-0", "1-1", -1},
		{"2-0", "1-99", 1},
		{"10-0", "2-0", 1},
		{"18446744073709551615-0", "9223372036854775807-999999999999", 1},
	}
	for _, tt := range tests {
		if got := compareRedisIDs(tt.a, tt.b); got != tt.want {
			t.Errorf("compareRedisIDs(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestMessageToEnvelope(t *testing.T) {
	msg := goredis.XMessage{
		ID: "1700000000000-2",
		Values: map[string]interface{}{
			"payload": `{"id":1}`,
		},
	}
	env := messageToEnvelope("events", msg, 42)
	if env["msg_id"] == "" {
		t.Fatal("msg_id is empty")
	}
	if env[streamOrderColumn] != int64(1700000000000000002) {
		t.Errorf("order = %v", env[streamOrderColumn])
	}
	if env["data"] == "" {
		t.Fatal("data is empty")
	}
}

func TestRedisOrderClampsOverflow(t *testing.T) {
	if got := redisOrder("9223372036854775808-1", 42); got != int64(1<<63-1) {
		t.Errorf("redisOrder overflow = %d, want MaxInt64", got)
	}
	if got := redisOrder("not-an-id", 42); got != int64(42) {
		t.Errorf("redisOrder fallback = %d, want 42", got)
	}
}
