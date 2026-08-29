package abra

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"abra", "flexibee"},
		func() interface{} { return NewAbraSource() },
	)
}
