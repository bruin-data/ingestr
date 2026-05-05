package chess

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"chess"},
		func() interface{} { return NewChessSource() },
	)
}
