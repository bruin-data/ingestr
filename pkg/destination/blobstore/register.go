package blobstore

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterDestination(
		[]string{"s3", "gs", "gcs", "az", "azure", "adls", "adlsgen2", "azdatalake", "abfs", "abfss"},
		func() interface{} { return NewBlobstoreDestination() },
	)
}
