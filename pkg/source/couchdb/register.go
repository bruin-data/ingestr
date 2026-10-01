package couchdb

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource([]string{"couchdb", "couchdb+https"}, func() interface{} { return NewCouchDBSource() })
}
