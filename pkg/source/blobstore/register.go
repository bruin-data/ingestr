package blobstore

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"s3", "gs", "gcs", "az", "azure", "sftp"},
		func() interface{} { return NewBlobstoreSource() },
	)
}
