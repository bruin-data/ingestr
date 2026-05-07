package pinterest

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"pinterest"},
		func() interface{} { return NewPinterestSource() },
	)
}
