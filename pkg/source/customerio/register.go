package customerio

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"customerio"},
		func() interface{} { return NewCustomerIOSource() },
	)
}
