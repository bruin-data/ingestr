package monday

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"monday"},
		func() interface{} { return NewMondaySource() },
	)
}
