package adapty

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"adapty"},
		func() interface{} { return NewAdaptySource() },
	)
}
