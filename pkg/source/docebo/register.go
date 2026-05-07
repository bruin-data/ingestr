package docebo

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"docebo"},
		func() interface{} { return NewDoceboSource() },
	)
}
