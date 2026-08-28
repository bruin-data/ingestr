package sklik

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"sklik"},
		func() interface{} { return NewSklikSource() },
	)
}
