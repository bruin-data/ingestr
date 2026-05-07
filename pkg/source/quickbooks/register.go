package quickbooks

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"quickbooks"},
		func() any { return NewQuickBooksSource() },
	)
}
