package blobstore

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"cloud.google.com/go/storage"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"golang.org/x/sync/errgroup"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/iterator"
)

const (
	s3DeleteBatchSize = 1000
	deleteParallelism = 16
)

var (
	errListUnsupported = errors.New("listing files is not supported for this provider")
	errAccessDenied    = errors.New("access denied")
)

// listPreviousBlobs returns the files a replace of t removes once the new files are
// written. Files written after the listing are never removed. Concurrent replaces on one
// path still need external coordination.
func (d *BlobstoreDestination) listPreviousBlobs(ctx context.Context, t *blobTable) ([]string, error) {
	if strings.Trim(t.basePath, "/") == "" {
		output.Warnf("[BLOBSTORE] replace keeps existing files for %s: the table has no path inside bucket %s, and the bucket root is never cleared\n", t.name, t.bucket)
		return nil, nil
	}

	// The trailing slash in the prefix keeps "reports/t" from matching "reports/t2".
	prefix := t.prefix()
	keys, err := d.listBlobs(ctx, t.bucket, prefix)
	switch {
	case errors.Is(err, errListUnsupported):
		output.Warnf("[BLOBSTORE] replace keeps existing files for %s: removing old files is not supported for %s yet\n", t.name, d.provider)
		return nil, nil
	case isAccessDenied(err):
		output.Warnf("[BLOBSTORE] replace keeps existing files for %s: listing %s/%s needs list permission on the bucket: %v\n", t.name, t.bucket, prefix, err)
		return nil, nil
	case err != nil:
		return nil, err
	}

	// Only files this layout produces directly under the table path are part of the table;
	// subfolders and other files under it are left alone.
	pattern := d.layoutPattern(t.name)
	var owned []string
	for _, key := range keys {
		if pattern.MatchString(strings.TrimPrefix(key, prefix)) {
			owned = append(owned, key)
		}
	}
	return owned, nil
}

// layoutPattern matches the keys, relative to the table path, that renderLayout produces
// for table.
func (d *BlobstoreDestination) layoutPattern(table string) *regexp.Regexp {
	pattern := strings.NewReplacer(
		regexp.QuoteMeta("{table_name}"), regexp.QuoteMeta(layoutTableName(table)),
		regexp.QuoteMeta("{load_id}"), "[0-9a-f]{8}",
		regexp.QuoteMeta("{file_id}"), "[0-9]+",
		regexp.QuoteMeta("{ext}"), "parquet",
	).Replace(regexp.QuoteMeta(d.layout))
	return regexp.MustCompile("^" + pattern + "$")
}

// removeStaleBlobs deletes the previous files of a replace that this load did not
// rewrite. It runs after the new files are written, once per replace.
func (d *BlobstoreDestination) removeStaleBlobs(ctx context.Context, t *blobTable) error {
	t.mu.Lock()
	var stale []string
	for _, key := range t.previousKeys {
		if _, ok := t.writtenKeys[key]; !ok {
			stale = append(stale, key)
		}
	}
	t.previousKeys = nil
	t.writtenKeys = nil
	t.mu.Unlock()

	if len(stale) == 0 {
		return nil
	}

	config.Debug("[BLOBSTORE] Removing %d files from the previous load under %s/%s", len(stale), t.bucket, t.prefix())
	err := d.deleteBlobs(ctx, t.bucket, stale)
	if isAccessDenied(err) {
		output.Warnf("[BLOBSTORE] replace kept %d files from the previous load of %s: deleting them needs delete permission on %s: %v\n", len(stale), t.name, t.bucket, err)
		return nil
	}
	return err
}

func isAccessDenied(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errAccessDenied) {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode() == "AccessDenied"
	}
	var gcsErr *googleapi.Error
	return errors.As(err, &gcsErr) && gcsErr.Code == http.StatusForbidden
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
		// Some S3-compatible stores reject batch deletes without a Content-MD5 header, which
		// current AWS SDKs no longer send.
		if d.customEndpoint {
			return deleteEach(ctx, keys, func(ctx context.Context, key string) error {
				_, err := d.s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
				if err != nil {
					return fmt.Errorf("failed to delete s3://%s/%s: %w", bucket, key, err)
				}
				return nil
			})
		}
		return d.deleteS3Batches(ctx, bucket, keys)

	case ProviderGCS:
		return deleteEach(ctx, keys, func(ctx context.Context, key string) error {
			err := d.gcsClient.Bucket(bucket).Object(key).Delete(ctx)
			if err != nil && !errors.Is(err, storage.ErrObjectNotExist) {
				return fmt.Errorf("failed to delete gs://%s/%s: %w", bucket, key, err)
			}
			return nil
		})
	}

	return errListUnsupported
}

func (d *BlobstoreDestination) deleteS3Batches(ctx context.Context, bucket string, keys []string) error {
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
			err := fmt.Errorf("failed to delete %d files in s3://%s, first: %s: %s",
				len(resp.Errors), bucket, aws.ToString(first.Key), aws.ToString(first.Message))
			if aws.ToString(first.Code) == "AccessDenied" {
				return fmt.Errorf("%w: %w", errAccessDenied, err)
			}
			return err
		}
	}
	return nil
}

func deleteEach(ctx context.Context, keys []string, del func(context.Context, string) error) error {
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(deleteParallelism)
	for _, key := range keys {
		g.Go(func() error { return del(ctx, key) })
	}
	return g.Wait()
}
