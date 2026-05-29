package blobstore

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"s3", "gs", "gcs", "az", "azure", "adls", "adlsgen2", "azdatalake", "abfs", "abfss", "sftp"},
		func() interface{} { return NewBlobstoreSource() },
	)
}
