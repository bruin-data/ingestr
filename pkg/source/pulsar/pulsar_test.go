package pulsar

import (
	"testing"
	"time"

	pulsargo "github.com/apache/pulsar-client-go/pulsar"
)

func TestParsePulsarURI(t *testing.T) {
	cfg, err := parsePulsarURI("pulsar://localhost:6650?subscription=analytics&token=secret&batch_size=100&batch_timeout=1.5")
	if err != nil {
		t.Fatalf("parsePulsarURI returned error: %v", err)
	}
	if cfg.ServiceURL != "pulsar://localhost:6650" {
		t.Errorf("ServiceURL = %q", cfg.ServiceURL)
	}
	if cfg.Subscription != "analytics" {
		t.Errorf("Subscription = %q", cfg.Subscription)
	}
	if cfg.Token != "secret" {
		t.Errorf("Token = %q", cfg.Token)
	}
	if cfg.BatchSize != 100 {
		t.Errorf("BatchSize = %d", cfg.BatchSize)
	}
	if cfg.BatchTimeout != 1500*time.Millisecond {
		t.Errorf("BatchTimeout = %v", cfg.BatchTimeout)
	}
	if cfg.OperationTimeout != 30*time.Second {
		t.Errorf("OperationTimeout = %v, want 30s default", cfg.OperationTimeout)
	}
}

func TestParsePulsarURIDefaultSubscription(t *testing.T) {
	cfg, err := parsePulsarURI("pulsar://localhost:6650")
	if err != nil {
		t.Fatalf("parsePulsarURI returned error: %v", err)
	}
	if cfg.Subscription != "ingestr" {
		t.Errorf("Subscription = %q", cfg.Subscription)
	}
}

func TestParsePulsarURISSLValidatesHostnameByDefault(t *testing.T) {
	cfg, err := parsePulsarURI("pulsar+ssl://localhost:6651")
	if err != nil {
		t.Fatalf("parsePulsarURI returned error: %v", err)
	}
	if !cfg.TLSValidateHost {
		t.Fatal("TLSValidateHost = false, want true for pulsar+ssl")
	}

	cfg, err = parsePulsarURI("pulsar+ssl://localhost:6651?tls_validate_hostname=false&operation_timeout=12&connection_timeout=8")
	if err != nil {
		t.Fatalf("parsePulsarURI returned error: %v", err)
	}
	if cfg.TLSValidateHost {
		t.Fatal("TLSValidateHost = true, want explicit false")
	}
	if cfg.OperationTimeout != 12*time.Second {
		t.Errorf("OperationTimeout = %v, want 12s", cfg.OperationTimeout)
	}
	if cfg.ConnectionTimeout != 8*time.Second {
		t.Errorf("ConnectionTimeout = %v, want 8s", cfg.ConnectionTimeout)
	}
}

func TestCompareMessageID(t *testing.T) {
	a := pulsargo.NewMessageID(1, 2, -1, -1)
	b := pulsargo.NewMessageID(1, 3, -1, -1)
	c := pulsargo.NewMessageID(1, 3, 1, -1)
	d := pulsargo.NewMessageID(1, 1, -1, 2)
	e := pulsargo.NewMessageID(9, 9, -1, 1)
	emptyPartition := pulsargo.NewMessageID(-1, -1, -1, 0)
	if compareMessageID(a, b) >= 0 {
		t.Fatal("a should sort before b")
	}
	if compareMessageID(b, a) <= 0 {
		t.Fatal("b should sort after a")
	}
	if compareMessageID(c, b) >= 0 {
		t.Fatal("batch-indexed id should sort before non-batch id in this source comparator")
	}
	if compareMessageID(d, e) <= 0 {
		t.Fatal("higher partition should sort after lower partition")
	}
	if compareMessageID(emptyPartition, pulsargo.EarliestMessageID()) != 0 {
		t.Fatal("empty partition sentinel should compare equal to earliest")
	}
}

func TestTableName(t *testing.T) {
	if got := tableName("persistent://tenant/ns/orders"); got != "orders" {
		t.Errorf("tableName returned %q", got)
	}
}
