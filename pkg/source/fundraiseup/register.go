package fundraiseup

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"fundraiseup"},
		func() interface{} { return NewFundraiseUpSource() },
	)
}
