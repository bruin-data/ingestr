package quickbooks

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"quickbooks"},
		func() any { return NewQuickBooksSource() },
	)
}
