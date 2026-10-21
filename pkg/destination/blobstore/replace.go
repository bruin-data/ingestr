package blobstore

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azdatalake/datalakeerror"
	datalakefilesystem "github.com/Azure/azure-sdk-for-go/sdk/storage/azdatalake/filesystem"
	"github.com/bruin-data/ingestr/internal/adlsutil"
	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"golang.org/x/sync/errgroup"
)

const (
	deleteParallelism = 16
	loadIDPattern     = "[0-9a-f]{8}"
	loadIDToken       = "\x00load_id\x00"
	fileIDToken       = "\x00file_id\x00"
)

const replacePermissions = "replace needs list and delete permission on the container (e.g. Storage Blob Data Contributor); use the append strategy to keep existing files"

type replaceState struct {
	bucket   string
	previous []string
	written  map[string]struct{}
}

// prepareReplace lists the files a replace of table removes once the new files are
// written: files directly in the folder the layout writes into whose names match it.
func (d *BlobstoreDestination) prepareReplace(ctx context.Context, table string) error {
	if d.provider != ProviderAzure && d.provider != ProviderAzureDatalake {
		return nil
	}

	bucket, basePath := parseBucketAndPath(table)
	listPrefix, recursive, pattern := replaceScope(d.renderLayout(table, loadIDToken, fileIDToken), basePath)
	if strings.Trim(listPrefix, "/") == "" {
		output.Warnf("[BLOBSTORE] replace keeps existing files for %s: its files go directly into the root of container %s, which is never cleared\n", table, bucket)
		return nil
	}

	keys, err := d.listBlobs(ctx, bucket, listPrefix, recursive)
	if err != nil {
		return fmt.Errorf("failed to list the previous files of %s; %s: %w", table, replacePermissions, err)
	}

	prefix := blobKey(basePath, "")
	var previous []string
	for _, key := range keys {
		if pattern.MatchString(strings.TrimPrefix(key, prefix)) {
			previous = append(previous, key)
		}
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.replaces == nil {
		d.replaces = make(map[string]*replaceState)
	}
	d.replaces[table] = &replaceState{bucket: bucket, previous: previous, written: make(map[string]struct{})}
	return nil
}

// replaceScope returns where to list a table's files and the pattern their path under
// basePath must match. Layouts with a per-load folder are listed recursively.
func replaceScope(rendered, basePath string) (listPrefix string, recursive bool, pattern *regexp.Regexp) {
	listPrefix = blobKey(basePath, "")
	if dir := rendered[:strings.LastIndex(rendered, "/")+1]; strings.Contains(dir, "\x00") {
		recursive = true
	} else {
		listPrefix += dir
	}

	quoted := strings.NewReplacer(loadIDToken, loadIDPattern, fileIDToken, "[0-9]+").Replace(regexp.QuoteMeta(rendered))
	return listPrefix, recursive, regexp.MustCompile("^" + quoted + "$")
}

func (d *BlobstoreDestination) markWritten(table, key string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if state, ok := d.replaces[table]; ok {
		state.written[key] = struct{}{}
	}
}

// FinalizeReplace removes the files a replace of table found before writing, except the
// ones it rewrote. The replace strategy calls it only after every row was written.
func (d *BlobstoreDestination) FinalizeReplace(ctx context.Context, table string) error {
	d.mu.Lock()
	state, ok := d.replaces[table]
	delete(d.replaces, table)
	d.mu.Unlock()
	if !ok {
		return nil
	}

	var stale []string
	for _, key := range state.previous {
		if _, ok := state.written[key]; !ok {
			stale = append(stale, key)
		}
	}
	if len(stale) == 0 {
		return nil
	}

	config.Debug("[BLOBSTORE] Removing %d files from the previous load of %s", len(stale), table)
	if err := d.deleteBlobs(ctx, state.bucket, stale); err != nil {
		return fmt.Errorf("failed to remove the previous files of %s, so some may remain next to the new ones; %s: %w", table, replacePermissions, err)
	}
	return nil
}

func (d *BlobstoreDestination) listBlobs(ctx context.Context, bucket, prefix string, recursive bool) ([]string, error) {
	var keys []string
	switch d.provider {
	case ProviderAzure:
		if recursive {
			pager := d.azureClient.NewListBlobsFlatPager(bucket, &azblob.ListBlobsFlatOptions{Prefix: &prefix})
			for pager.More() {
				page, err := pager.NextPage(ctx)
				if err != nil {
					return nil, err
				}
				for _, item := range page.Segment.BlobItems {
					if item.Name != nil {
						keys = append(keys, *item.Name)
					}
				}
			}
			return keys, nil
		}
		pager := d.azureClient.ServiceClient().NewContainerClient(bucket).
			NewListBlobsHierarchyPager("/", &container.ListBlobsHierarchyOptions{Prefix: &prefix})
		for pager.More() {
			page, err := pager.NextPage(ctx)
			if err != nil {
				return nil, err
			}
			for _, item := range page.Segment.BlobItems {
				if item.Name != nil {
					keys = append(keys, *item.Name)
				}
			}
		}
		return keys, nil

	case ProviderAzureDatalake:
		fsClient, err := d.adlsClient.newFileSystemClient(adlsutil.FilesystemURL(d.adlsClient.accountName, bucket))
		if err != nil {
			return nil, fmt.Errorf("failed to create filesystem client: %w", err)
		}
		dir := strings.TrimSuffix(prefix, "/")
		pager := fsClient.NewListPathsPager(recursive, &datalakefilesystem.ListPathsOptions{Prefix: &dir})
		for pager.More() {
			page, err := pager.NextPage(ctx)
			if datalakeerror.HasCode(err, datalakeerror.PathNotFound) {
				return nil, nil
			}
			if err != nil {
				return nil, err
			}
			for _, p := range page.Paths {
				if p == nil || p.Name == nil || (p.IsDirectory != nil && *p.IsDirectory) {
					continue
				}
				keys = append(keys, strings.TrimPrefix(*p.Name, "/"))
			}
		}
		return keys, nil
	}

	return nil, fmt.Errorf("listing files is not supported for %s", d.provider)
}

func (d *BlobstoreDestination) deleteBlobs(ctx context.Context, bucket string, keys []string) error {
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(deleteParallelism)
	for _, key := range keys {
		g.Go(func() error {
			if err := d.deleteBlob(ctx, bucket, key); err != nil {
				return fmt.Errorf("failed to delete %s/%s: %w", bucket, key, err)
			}
			return nil
		})
	}
	return g.Wait()
}

func (d *BlobstoreDestination) deleteBlob(ctx context.Context, bucket, key string) error {
	switch d.provider {
	case ProviderAzure:
		_, err := d.azureClient.DeleteBlob(ctx, bucket, key, nil)
		if bloberror.HasCode(err, bloberror.BlobNotFound) {
			return nil
		}
		return err

	case ProviderAzureDatalake:
		pathURL, err := buildAzureDatalakePathURL(d.adlsClient.accountName, bucket, key)
		if err != nil {
			return err
		}
		fileClient, err := d.adlsClient.newFileClient(pathURL)
		if err != nil {
			return fmt.Errorf("failed to create file client: %w", err)
		}
		_, err = fileClient.Delete(ctx, nil)
		if datalakeerror.HasCode(err, datalakeerror.PathNotFound) {
			return nil
		}
		return err
	}

	return fmt.Errorf("deleting files is not supported for %s", d.provider)
}
