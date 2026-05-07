package zoom

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"zoom"},
		func() interface{} { return NewZoomSource() },
	)
}
