package personio

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"personio"},
		func() interface{} { return NewPersonioSource() },
	)
}
