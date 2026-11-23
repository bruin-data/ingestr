package odoo

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"odoo"},
		func() interface{} { return NewOdooSource() },
	)
}
