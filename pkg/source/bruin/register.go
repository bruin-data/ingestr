package bruin

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"bruin"},
		func() interface{} { return NewBruinSource() },
	)
}
