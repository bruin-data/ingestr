package rabbitmq

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"amqp", "amqps"},
		func() interface{} { return NewRabbitMQSource() },
	)
}
