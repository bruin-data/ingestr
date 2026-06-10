package paddle

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"paddle"},
		func() interface{} { return NewPaddleSource() },
	)
}
