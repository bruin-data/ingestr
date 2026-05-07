package bruin

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"bruin"},
		func() interface{} { return NewBruinSource() },
	)
}
