package square

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"square"},
		func() interface{} { return NewSquareSource() },
	)
}
