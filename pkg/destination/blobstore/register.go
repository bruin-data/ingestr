package blobstore

import "github.com/bruin-data/gong/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"s3", "gs", "gcs", "az", "azure"},
		func() interface{} { return NewBlobstoreDestination() },
	)
}
