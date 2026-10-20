package blobstore

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"cloud.google.com/go/storage"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"golang.org/x/sync/errgroup"
	"google.golang.org/api/iterator"
)

const (
	s3DeleteBatchSize = 1000
	deleteParallelism = 16
)

var errListUnsupported = errors.New("listing files is not supported for this provider")

const replacePermissions = "replace needs list and delete permission on the bucket (S3: s3:ListBucket, s3:DeleteObject; GCS: storage.objects.list, storage.objects.delete); use the append strategy to keep existing files"

// Stand-ins for the per-load layout values; they never occur in a real layout.
const (
	loadIDToken = "\x00load_id\x00"
	fileIDToken = "\x00file_id\x00"
)

// FinalizeReplace removes the files a replace of table found before writing, except the
// ones it rewrote. The replace strategy calls it only after every row was written, so a
// failed load keeps the previous files.
func (d *BlobstoreDestination) FinalizeReplace(ctx context.Context, table string) error {
	d.mu.Lock()
	t, ok := d.tables[table]
	d.mu.Unlock()
	if !ok {
		return nil
	}
	if err := d.removeStaleBlobs(ctx, t); err != nil {
		return fmt.Errorf("failed to remove the previous files of %s, so some may remain next to the new ones; %s: %w", t.name, replacePermissions, err)
	}
	return nil
}

// listPreviousBlobs returns the files of t that a replace removes once the new files are
// written: files directly in the folder the layout writes into whose names match the
// layout. Files written after the listing are never removed. Concurrent replaces on one
// path still need external coordination.
func (d *BlobstoreDestination) listPreviousBlobs(ctx context.Context, t *blobTable) ([]string, error) {
	// The trailing slash in the prefix keeps "reports/t" from matching "reports/t2".
	prefix := t.prefix()
	rendered := layoutReplacer(t.name, loadIDToken, fileIDToken).Replace(d.layout)
	listPrefix, delimited := prefix, true
	if dir := rendered[:strings.LastIndex(rendered, "/")+1]; strings.Contains(dir, "\x00") {
		delimited = false
	} else {
		listPrefix += dir
	}

	if strings.Trim(listPrefix, "/") == "" {
		output.Warnf("[BLOBSTORE] replace keeps existing files for %s: its files go directly into the root of bucket %s, which is never cleared\n", t.name, t.bucket)
		return nil, nil
	}

	keys, err := d.listBlobs(ctx, t.bucket, listPrefix, delimited)
	switch {
	case errors.Is(err, errListUnsupported):
		output.Warnf("[BLOBSTORE] replace keeps existing files for %s: removing old files is not supported for %s yet\n", t.name, d.provider)
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("failed to list the previous files of %s; %s: %w", t.name, replacePermissions, err)
	}

	pattern := layoutPattern(rendered)
	var owned []string
	for _, key := range keys {
		if pattern.MatchString(strings.TrimPrefix(key, prefix)) {
			owned = append(owned, key)
		}
	}
	return owned, nil
}

func layoutPattern(rendered string) *regexp.Regexp {
	pattern := strings.NewReplacer(
		loadIDToken, loadIDPattern,
		fileIDToken, "[0-9]+",
	).Replace(regexp.QuoteMeta(rendered))
	return regexp.MustCompile("^" + pattern + "$")
}

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
	return d.deleteBlobs(ctx, t.bucket, stale)
}

// listBlobs lists the keys under prefix; delimited leaves out keys in subfolders.
func (d *BlobstoreDestination) listBlobs(ctx context.Context, bucket, prefix string, delimited bool) ([]string, error) {
	var keys []string
	var delimiter string
	if delimited {
		delimiter = "/"
	}

	switch d.provider {
	case ProviderS3:
		input := &s3.ListObjectsV2Input{Bucket: aws.String(bucket), Prefix: aws.String(prefix)}
		if delimited {
			input.Delimiter = aws.String(delimiter)
		}
		pager := s3.NewListObjectsV2Paginator(d.s3Client, input)
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
		query := &storage.Query{Prefix: prefix, Delimiter: delimiter}
		if err := query.SetAttrSelection([]string{"Name"}); err != nil {
			return nil, err
		}
		it := d.gcsClient.Bucket(bucket).Objects(ctx, query)
		for {
			attrs, err := it.Next()
			if errors.Is(err, iterator.Done) {
				return keys, nil
			}
			if err != nil {
				return nil, fmt.Errorf("failed to list gs://%s/%s: %w", bucket, prefix, err)
			}
			if attrs.Name != "" {
				keys = append(keys, attrs.Name)
			}
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
			codes := map[string]bool{}
			for _, e := range resp.Errors {
				codes[aws.ToString(e.Code)] = true
			}
			first := resp.Errors[0]
			return fmt.Errorf("failed to delete %d files in s3://%s (codes: %s), first: %s: %s",
				len(resp.Errors), bucket, strings.Join(slices.Sorted(maps.Keys(codes)), ", "), aws.ToString(first.Key), aws.ToString(first.Message))
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
