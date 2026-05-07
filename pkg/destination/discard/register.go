package discard

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"discard"},
		func() interface{} { return NewDiscardDestination() },
	)
}
