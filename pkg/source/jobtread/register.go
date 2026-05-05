package jobtread

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"jobtread"},
		func() interface{} { return NewJobTreadSource() },
	)
}
