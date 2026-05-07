package clickup

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"clickup"},
		func() interface{} { return NewClickUpSource() },
	)
}
