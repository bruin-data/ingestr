package cursor

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"cursor"},
		func() interface{} { return NewCursorSource() },
	)
}
