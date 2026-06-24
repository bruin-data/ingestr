package eventhubs

import (
	"net/url"
	"testing"
)

func TestParseEventHubsURIWithConnectionString(t *testing.T) {
	cfg, err := parseEventHubsURI("eventhubs://example.servicebus.windows.net?connection_string=Endpoint%3Dsb%3A%2F%2Fexample.servicebus.windows.net%2F%3BSharedAccessKeyName%3Dname%3BSharedAccessKey%3Dkey&consumer_group=analytics&batch_size=100")
	if err != nil {
		t.Fatalf("parseEventHubsURI returned error: %v", err)
	}
	if cfg.Namespace != "example.servicebus.windows.net" {
		t.Errorf("Namespace = %q", cfg.Namespace)
	}
	if cfg.ConsumerGroup != "analytics" {
		t.Errorf("ConsumerGroup = %q", cfg.ConsumerGroup)
	}
	if cfg.BatchSize != "100" {
		t.Errorf("BatchSize = %q", cfg.BatchSize)
	}

	kafkaURI := cfg.kafkaURI()
	parsed, err := url.Parse(kafkaURI)
	if err != nil {
		t.Fatalf("generated kafka URI is invalid: %v", err)
	}
	q := parsed.Query()
	if q.Get("bootstrap_servers") != "example.servicebus.windows.net:9093" {
		t.Errorf("bootstrap_servers = %q", q.Get("bootstrap_servers"))
	}
	if q.Get("group_id") != "analytics" {
		t.Errorf("group_id = %q", q.Get("group_id"))
	}
	if q.Get("sasl_username") != "$ConnectionString" {
		t.Errorf("sasl_username = %q", q.Get("sasl_username"))
	}
	if q.Get("security_protocol") != "SASL_SSL" {
		t.Errorf("security_protocol = %q", q.Get("security_protocol"))
	}
}

func TestParseEventHubsURIBuildsConnectionStringFromSAS(t *testing.T) {
	cfg, err := parseEventHubsURI("eventhubs://example?shared_access_key_name=RootManageSharedAccessKey&shared_access_key=secret")
	if err != nil {
		t.Fatalf("parseEventHubsURI returned error: %v", err)
	}
	if cfg.Namespace != "example.servicebus.windows.net" {
		t.Errorf("Namespace = %q", cfg.Namespace)
	}
	if cfg.ConsumerGroup != defaultConsumerGroup {
		t.Errorf("ConsumerGroup = %q", cfg.ConsumerGroup)
	}
	want := "Endpoint=sb://example.servicebus.windows.net/;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=secret"
	if cfg.ConnectionString != want {
		t.Errorf("ConnectionString = %q, want %q", cfg.ConnectionString, want)
	}
}

func TestParseEventHubsURIRequiresCredentials(t *testing.T) {
	if _, err := parseEventHubsURI("eventhubs://example.servicebus.windows.net"); err == nil {
		t.Fatal("expected missing credentials error")
	}
}
