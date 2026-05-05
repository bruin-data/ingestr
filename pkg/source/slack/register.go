package slack

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"slack"},
		func() interface{} { return NewSlackSource() },
	)
}
