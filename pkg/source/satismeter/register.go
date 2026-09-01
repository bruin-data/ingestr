package satismeter

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"satismeter"},
		func() interface{} { return NewSatisMeterSource() },
	)
}
