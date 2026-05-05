package jira

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"jira"},
		func() interface{} { return NewJiraSource() },
	)
}
