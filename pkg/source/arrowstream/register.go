package arrowstream

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"arrow-stream", "arrowstream"},
		func() interface{} { return NewArrowStreamSource() },
	)
}
