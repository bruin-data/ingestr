package vertica

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"vertica"},
		func() interface{} { return NewVerticaSource() },
	)
}
