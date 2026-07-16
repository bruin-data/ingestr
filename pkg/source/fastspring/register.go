package fastspring

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"fastspring"},
		func() interface{} { return NewFastspringSource() },
	)
}
