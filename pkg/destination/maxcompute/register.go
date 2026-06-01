package maxcompute

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"maxcompute", "odps"},
		func() interface{} { return NewMaxComputeDestination() },
	)
}
