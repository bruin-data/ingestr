package customerio

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"customerio"},
		func() interface{} { return NewCustomerIOSource() },
	)
}
