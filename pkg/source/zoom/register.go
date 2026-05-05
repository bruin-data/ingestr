package zoom

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"zoom"},
		func() interface{} { return NewZoomSource() },
	)
}
