package csv

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"csv"},
		func() interface{} { return NewCSVSource() },
	)
}
