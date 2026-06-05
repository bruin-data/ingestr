package maxcompute

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"maxcompute", "odps"},
		func() interface{} { return NewMaxComputeSource() },
	)
}
