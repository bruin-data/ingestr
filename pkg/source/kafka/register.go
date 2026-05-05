package kafka

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"kafka"},
		func() interface{} { return NewKafkaSource() },
	)
}
