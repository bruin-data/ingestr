package slack

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"slack"},
		func() interface{} { return NewSlackSource() },
	)
}
