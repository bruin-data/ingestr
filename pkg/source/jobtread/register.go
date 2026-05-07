package jobtread

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"jobtread"},
		func() interface{} { return NewJobTreadSource() },
	)
}
