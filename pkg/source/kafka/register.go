package kafka

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"kafka"},
		func() interface{} { return NewKafkaSource() },
	)
}
