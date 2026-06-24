package redis

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"redis", "rediss"},
		func() interface{} { return NewRedisSource() },
	)
}
