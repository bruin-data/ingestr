package eventhubs

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/source/kafka"
)

const (
	defaultKafkaPort     = "9093"
	defaultConsumerGroup = "$Default"
)

type eventHubsConfig struct {
	Namespace           string
	BootstrapServers    string
	ConsumerGroup       string
	ConnectionString    string
	SharedAccessKeyName string
	SharedAccessKey     string
	BatchSize           string
	BatchTimeout        string
}

type EventHubsSource struct {
	delegate *kafka.KafkaSource
}

func NewEventHubsSource() *EventHubsSource {
	return &EventHubsSource{delegate: kafka.NewKafkaSource()}
}

func (s *EventHubsSource) Schemes() []string {
	return []string{"eventhubs", "eventhub", "azure-event-hubs", "azureeventhubs"}
}

func (s *EventHubsSource) Connect(ctx context.Context, raw string) error {
	cfg, err := parseEventHubsURI(raw)
	if err != nil {
		return err
	}
	if s.delegate == nil {
		s.delegate = kafka.NewKafkaSource()
	}
	return s.delegate.Connect(ctx, cfg.kafkaURI())
}

func (s *EventHubsSource) Close(ctx context.Context) error {
	if s.delegate == nil {
		return nil
	}
	return s.delegate.Close(ctx)
}

func (s *EventHubsSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if s.delegate == nil {
		return nil, fmt.Errorf("eventhubs source is not connected")
	}
	return s.delegate.GetTable(ctx, req)
}

func (s *EventHubsSource) HandlesIncrementality() bool {
	if s.delegate == nil {
		return false
	}
	return s.delegate.HandlesIncrementality()
}

func (s *EventHubsSource) SupportsStreaming() bool {
	return true
}

func (s *EventHubsSource) DefaultStreamingStrategy() config.IncrementalStrategy {
	if s.delegate == nil {
		return config.StrategyMerge
	}
	return s.delegate.DefaultStreamingStrategy()
}

func (s *EventHubsSource) CommitStream(ctx context.Context, token any) error {
	if s.delegate == nil {
		return fmt.Errorf("eventhubs source is not connected")
	}
	return s.delegate.CommitStream(ctx, token)
}

func parseEventHubsURI(raw string) (eventHubsConfig, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return eventHubsConfig{}, fmt.Errorf("invalid Event Hubs URI: %w", err)
	}
	switch u.Scheme {
	case "eventhubs", "eventhub", "azure-event-hubs", "azureeventhubs":
	default:
		return eventHubsConfig{}, fmt.Errorf("invalid Event Hubs URI: unsupported scheme %q", u.Scheme)
	}

	q := u.Query()
	cfg := eventHubsConfig{
		Namespace:           u.Hostname(),
		ConsumerGroup:       firstQuery(q, "consumer_group", "group_id"),
		ConnectionString:    firstQuery(q, "connection_string"),
		SharedAccessKeyName: firstQuery(q, "shared_access_key_name", "sas_key_name"),
		SharedAccessKey:     firstQuery(q, "shared_access_key", "sas_key"),
		BatchSize:           firstQuery(q, "batch_size"),
		BatchTimeout:        firstQuery(q, "batch_timeout"),
	}
	if cfg.ConsumerGroup == "" {
		cfg.ConsumerGroup = defaultConsumerGroup
	}
	if cfg.Namespace == "" {
		return eventHubsConfig{}, fmt.Errorf("event hubs URI: namespace host is required")
	}
	if !strings.Contains(cfg.Namespace, ".") {
		cfg.Namespace += ".servicebus.windows.net"
	}
	if cfg.ConnectionString == "" {
		if cfg.SharedAccessKeyName == "" || cfg.SharedAccessKey == "" {
			return eventHubsConfig{}, fmt.Errorf("event hubs URI: connection_string or shared_access_key_name/shared_access_key is required")
		}
		cfg.ConnectionString = fmt.Sprintf(
			"Endpoint=sb://%s/;SharedAccessKeyName=%s;SharedAccessKey=%s",
			cfg.Namespace,
			cfg.SharedAccessKeyName,
			cfg.SharedAccessKey,
		)
	}
	cfg.BootstrapServers = net.JoinHostPort(cfg.Namespace, defaultKafkaPort)
	return cfg, nil
}

func (c eventHubsConfig) kafkaURI() string {
	values := url.Values{}
	values.Set("bootstrap_servers", c.BootstrapServers)
	values.Set("group_id", c.ConsumerGroup)
	values.Set("security_protocol", "SASL_SSL")
	values.Set("sasl_mechanisms", "PLAIN")
	values.Set("sasl_username", "$ConnectionString")
	values.Set("sasl_password", c.ConnectionString)
	if c.BatchSize != "" {
		values.Set("batch_size", c.BatchSize)
	}
	if c.BatchTimeout != "" {
		values.Set("batch_timeout", c.BatchTimeout)
	}
	return "kafka://?" + values.Encode()
}

func firstQuery(values url.Values, keys ...string) string {
	for _, key := range keys {
		if v := values.Get(key); v != "" {
			return v
		}
	}
	return ""
}

var (
	_ source.Source          = (*EventHubsSource)(nil)
	_ source.StreamingSource = (*EventHubsSource)(nil)
	_ source.StreamCommitter = (*EventHubsSource)(nil)
)
