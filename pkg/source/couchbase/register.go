package couchbase

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"couchbase"},
		func() interface{} { return NewCouchbaseSource() },
	)
}
