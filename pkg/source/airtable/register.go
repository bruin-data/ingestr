package airtable

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"airtable"},
		func() any { return NewAirtableSource() },
	)
}
