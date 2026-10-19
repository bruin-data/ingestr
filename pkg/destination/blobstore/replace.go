package blobstore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"cloud.google.com/go/storage"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"google.golang.org/api/iterator"
)

// s3DeleteBatchSize is the most keys a single S3 DeleteObjects request accepts.
const s3DeleteBatchSize = 1000

var errListUnsupported = errors.New("listing files is not supported for this provider")

// listPreviousBlobs returns the files under the table path before a replace writes
// anything. Only these files are removed afterwards, so this load never deletes files
// written after it started. Concurrent replaces on one path still need external coordination.
func (d *BlobstoreDestination) listPreviousBlobs(ctx context.Context) ([]string, error) {
	// Never clear a whole bucket: without a path, the table has no files of its own.
	if strings.Trim(d.basePath, "/") == "" {
		output.Warnf("[BLOBSTORE] replace keeps existing files for %s: the table has no path inside bucket %s, and the bucket root is never cleared", d.tableName, d.bucketName)
		return nil, nil
	}

	// The trailing slash in the prefix keeps "reports/t" from matching "reports/t2".
	keys, err := d.listBlobs(ctx, d.bucketName, d.tablePrefix())
	if errors.Is(err, errListUnsupported) {
		output.Warnf("[BLOBSTORE] replace keeps existing files for %s: removing old files is not supported for %s yet", d.tableName, d.provider)
		return nil, nil
	}
	return keys, err
}

// removeStaleBlobs completes a replace by deleting the previous files that this load did
// not rewrite. It runs only after every new file is written.
func (d *BlobstoreDestination) removeStaleBlobs(ctx context.Context) error {
	d.mu.Lock()
	bucket := d.bucketName
	var stale []string
	for _, key := range d.previousKeys {
		if _, ok := d.writtenKeys[key]; !ok {
			stale = append(stale, key)
		}
	}
	d.mu.Unlock()

	if len(stale) == 0 {
		return nil
	}

	config.Debug("[BLOBSTORE] Removing %d files from the previous load under %s/%s", len(stale), bucket, d.tablePrefix())
	return d.deleteBlobs(ctx, bucket, stale)
}

func (d *BlobstoreDestination) listBlobs(ctx context.Context, bucket, prefix string) ([]string, error) {
	var keys []string

	switch d.provider {
	case ProviderS3:
		pager := s3.NewListObjectsV2Paginator(d.s3Client, &s3.ListObjectsV2Input{
			Bucket: aws.String(bucket),
			Prefix: aws.String(prefix),
		})
		for pager.HasMorePages() {
			page, err := pager.NextPage(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to list s3://%s/%s: %w", bucket, prefix, err)
			}
			for _, obj := range page.Contents {
				keys = append(keys, aws.ToString(obj.Key))
			}
		}
		return keys, nil

	case ProviderGCS:
		it := d.gcsClient.Bucket(bucket).Objects(ctx, &storage.Query{Prefix: prefix})
		for {
			attrs, err := it.Next()
			if errors.Is(err, iterator.Done) {
				return keys, nil
			}
			if err != nil {
				return nil, fmt.Errorf("failed to list gs://%s/%s: %w", bucket, prefix, err)
			}
			keys = append(keys, attrs.Name)
		}
	}

	return nil, errListUnsupported
}

func (d *BlobstoreDestination) deleteBlobs(ctx context.Context, bucket string, keys []string) error {
	switch d.provider {
	case ProviderS3:
		for start := 0; start < len(keys); start += s3DeleteBatchSize {
			batch := keys[start:min(start+s3DeleteBatchSize, len(keys))]
			objects := make([]s3types.ObjectIdentifier, len(batch))
			for i, key := range batch {
				objects[i] = s3types.ObjectIdentifier{Key: aws.String(key)}
			}
			resp, err := d.s3Client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
				Bucket: aws.String(bucket),
				Delete: &s3types.Delete{Objects: objects, Quiet: aws.Bool(true)},
			})
			if err != nil {
				return fmt.Errorf("failed to delete files in s3://%s: %w", bucket, err)
			}
			if len(resp.Errors) > 0 {
				first := resp.Errors[0]
				return fmt.Errorf("failed to delete %d files in s3://%s, first: %s: %s",
					len(resp.Errors), bucket, aws.ToString(first.Key), aws.ToString(first.Message))
			}
		}
		return nil

	case ProviderGCS:
		for _, key := range keys {
			err := d.gcsClient.Bucket(bucket).Object(key).Delete(ctx)
			if err != nil && !errors.Is(err, storage.ErrObjectNotExist) {
				return fmt.Errorf("failed to delete gs://%s/%s: %w", bucket, key, err)
			}
		}
		return nil
	}

	return errListUnsupported
}
