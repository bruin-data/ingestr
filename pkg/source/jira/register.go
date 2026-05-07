package jira

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"jira"},
		func() interface{} { return NewJiraSource() },
	)
}
