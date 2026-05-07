package smartsheet

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"smartsheet"},
		func() interface{} { return NewSmartsheetSource() },
	)
}
