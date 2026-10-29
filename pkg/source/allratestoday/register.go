package allratestoday

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"allratestoday"},
		func() interface{} { return New() },
	)
}
