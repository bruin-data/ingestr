package sumble

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"sumble"},
		func() interface{} { return NewSumbleSource() },
	)
}
