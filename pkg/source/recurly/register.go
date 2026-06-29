package recurly

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"recurly"},
		func() interface{} { return NewRecurlySource() },
	)
}
