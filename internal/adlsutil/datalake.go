package adlsutil

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azdatalake/datalakeerror"
	datalakedirectory "github.com/Azure/azure-sdk-for-go/sdk/storage/azdatalake/directory"
	datalakefile "github.com/Azure/azure-sdk-for-go/sdk/storage/azdatalake/file"
	datalakefilesystem "github.com/Azure/azure-sdk-for-go/sdk/storage/azdatalake/filesystem"
)

// OneLakeDNSSuffix is the DFS endpoint suffix for Microsoft Fabric OneLake.
// OneLake speaks the ADLS Gen2 protocol with a fixed account name of "onelake".
const OneLakeDNSSuffix = ".dfs.fabric.microsoft.com"

// OneLakeAccountName is the fixed storage account used by OneLake.
const OneLakeAccountName = "onelake"

// DataLakeClient is an ADLS Gen2 client that can talk to any DFS endpoint
// (standard Azure storage or OneLake) by varying the DNS suffix. It bundles the
// file/directory/filesystem client factories together with the upload, directory
// management and listing helpers shared across destinations.
type DataLakeClient struct {
	accountName         string
	dnsSuffix           string
	newFileClient       func(pathURL string) (*datalakefile.Client, error)
	newDirectoryClient  func(pathURL string) (*datalakedirectory.Client, error)
	newFilesystemClient func(fsURL string) (*datalakefilesystem.Client, error)
}

// NewDataLakeClientWithToken builds a client authenticated with an Entra ID token
// credential (service principal, managed identity, az CLI, etc.).
func NewDataLakeClientWithToken(accountName, dnsSuffix string, cred azcore.TokenCredential) *DataLakeClient {
	return &DataLakeClient{
		accountName: accountName,
		dnsSuffix:   dnsSuffix,
		newFileClient: func(pathURL string) (*datalakefile.Client, error) {
			return datalakefile.NewClient(pathURL, cred, nil)
		},
		newDirectoryClient: func(pathURL string) (*datalakedirectory.Client, error) {
			return datalakedirectory.NewClient(pathURL, cred, nil)
		},
		newFilesystemClient: func(fsURL string) (*datalakefilesystem.Client, error) {
			return datalakefilesystem.NewClient(fsURL, cred, nil)
		},
	}
}

// NewDataLakeClientWithSAS builds a client authenticated with a SAS token.
func NewDataLakeClientWithSAS(accountName, dnsSuffix, sasToken string) *DataLakeClient {
	sasToken = strings.TrimPrefix(sasToken, "?")
	return &DataLakeClient{
		accountName: accountName,
		dnsSuffix:   dnsSuffix,
		newFileClient: func(pathURL string) (*datalakefile.Client, error) {
			return datalakefile.NewClientWithNoCredential(AppendSASToken(pathURL, sasToken), nil)
		},
		newDirectoryClient: func(pathURL string) (*datalakedirectory.Client, error) {
			return datalakedirectory.NewClientWithNoCredential(AppendSASToken(pathURL, sasToken), nil)
		},
		newFilesystemClient: func(fsURL string) (*datalakefilesystem.Client, error) {
			return datalakefilesystem.NewClientWithNoCredential(AppendSASToken(fsURL, sasToken), nil)
		},
	}
}

func (c *DataLakeClient) pathURL(fileSystem, path string) (string, error) {
	return PathURLWithSuffix(c.accountName, c.dnsSuffix, fileSystem, path)
}

// UploadBuffer writes data to fileSystem/path, creating parent directories first
// and replacing the file if it already exists.
func (c *DataLakeClient) UploadBuffer(ctx context.Context, fileSystem, path string, data []byte) error {
	if err := c.EnsureDirectories(ctx, fileSystem, parentDir(path)); err != nil {
		return err
	}

	pathURL, err := c.pathURL(fileSystem, path)
	if err != nil {
		return err
	}

	fileClient, err := c.newFileClient(pathURL)
	if err != nil {
		return fmt.Errorf("failed to create file client: %w", err)
	}

	if err := recreateFile(ctx, fileClient, path); err != nil {
		return err
	}

	if err := fileClient.UploadBuffer(ctx, data, nil); err != nil {
		return fmt.Errorf("failed to upload file %s: %w", path, err)
	}
	return nil
}

// EnsureDirectories creates each directory segment in dirPath if missing.
func (c *DataLakeClient) EnsureDirectories(ctx context.Context, fileSystem, dirPath string) error {
	dirPath = strings.Trim(dirPath, "/")
	if dirPath == "" {
		return nil
	}

	var current string
	for _, part := range strings.Split(dirPath, "/") {
		if part == "" {
			continue
		}
		if current == "" {
			current = part
		} else {
			current += "/" + part
		}

		pathURL, err := c.pathURL(fileSystem, current)
		if err != nil {
			return err
		}

		dirClient, err := c.newDirectoryClient(pathURL)
		if err != nil {
			return fmt.Errorf("failed to create directory client for %s: %w", current, err)
		}

		if _, err := dirClient.Create(ctx, nil); err != nil && !isAlreadyExists(err) {
			return fmt.Errorf("failed to create directory %s: %w", current, err)
		}
	}

	return nil
}

// Download reads the full contents of fileSystem/path into memory.
func (c *DataLakeClient) Download(ctx context.Context, fileSystem, path string) ([]byte, error) {
	pathURL, err := c.pathURL(fileSystem, path)
	if err != nil {
		return nil, err
	}
	fileClient, err := c.newFileClient(pathURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create file client: %w", err)
	}
	resp, err := fileClient.DownloadStream(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to download %s: %w", path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}
	return data, nil
}

// DeleteDir recursively removes a directory and its contents. It is a no-op if
// the directory does not exist.
func (c *DataLakeClient) DeleteDir(ctx context.Context, fileSystem, dirPath string) error {
	pathURL, err := c.pathURL(fileSystem, dirPath)
	if err != nil {
		return err
	}
	dirClient, err := c.newDirectoryClient(pathURL)
	if err != nil {
		return fmt.Errorf("failed to create directory client for %s: %w", dirPath, err)
	}
	if _, err := dirClient.Delete(ctx, nil); err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to delete directory %s: %w", dirPath, err)
	}
	return nil
}

// ListLogVersions returns the Delta commit versions found under logDir (the
// numeric prefixes of the "<version>.json" files), sorted ascending. Returns an
// empty slice if the directory does not exist.
func (c *DataLakeClient) ListLogVersions(ctx context.Context, fileSystem, logDir string) ([]int64, error) {
	fsURL := FilesystemURLWithSuffix(c.accountName, c.dnsSuffix, fileSystem)
	fsClient, err := c.newFilesystemClient(fsURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create filesystem client: %w", err)
	}

	prefix := strings.Trim(logDir, "/") + "/"
	pager := fsClient.NewListPathsPager(false, &datalakefilesystem.ListPathsOptions{
		Prefix: &prefix,
	})

	var versions []int64
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			if isNotFound(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("failed to list %s: %w", logDir, err)
		}
		for _, p := range page.Paths {
			if p == nil || p.Name == nil {
				continue
			}
			name := *p.Name
			if idx := strings.LastIndex(name, "/"); idx != -1 {
				name = name[idx+1:]
			}
			if !strings.HasSuffix(name, ".json") {
				continue
			}
			v, err := strconv.ParseInt(strings.TrimSuffix(name, ".json"), 10, 64)
			if err != nil {
				continue
			}
			versions = append(versions, v)
		}
	}

	slices.Sort(versions)
	return versions, nil
}

func recreateFile(ctx context.Context, fileClient *datalakefile.Client, path string) error {
	if _, err := fileClient.Create(ctx, nil); err == nil {
		return nil
	} else if !isAlreadyExists(err) {
		return fmt.Errorf("failed to create file %s: %w", path, err)
	}

	if _, err := fileClient.Delete(ctx, nil); err != nil {
		return fmt.Errorf("failed to delete existing file %s before upload: %w", path, err)
	}
	if _, err := fileClient.Create(ctx, nil); err != nil {
		return fmt.Errorf("failed to recreate file %s: %w", path, err)
	}
	return nil
}

func isAlreadyExists(err error) bool {
	return datalakeerror.HasCode(err, datalakeerror.PathAlreadyExists, datalakeerror.ResourceAlreadyExists)
}

func isNotFound(err error) bool {
	return datalakeerror.HasCode(err, datalakeerror.PathNotFound, datalakeerror.FileSystemNotFound)
}

func parentDir(path string) string {
	path = strings.Trim(path, "/")
	if idx := strings.LastIndex(path, "/"); idx != -1 {
		return path[:idx]
	}
	return ""
}

// PathURLWithSuffix builds a DFS path URL for an arbitrary DNS suffix.
func PathURLWithSuffix(accountName, dnsSuffix, fileSystem, path string) (string, error) {
	accountName = strings.TrimSpace(accountName)
	fileSystem = strings.Trim(fileSystem, "/")
	path = strings.Trim(path, "/")

	if accountName == "" {
		return "", fmt.Errorf("account_name is required for Azure Data Lake Storage Gen2")
	}
	if fileSystem == "" {
		return "", fmt.Errorf("file system is required for Azure Data Lake Storage Gen2")
	}
	if path == "" {
		return "", fmt.Errorf("path is required for Azure Data Lake Storage Gen2")
	}

	u := &url.URL{
		Scheme: "https",
		Host:   accountName + dnsSuffix,
		Path:   "/" + fileSystem + "/" + path,
	}
	return u.String(), nil
}

// FilesystemURLWithSuffix builds a DFS filesystem URL for an arbitrary DNS suffix.
func FilesystemURLWithSuffix(accountName, dnsSuffix, fileSystem string) string {
	u := &url.URL{
		Scheme: "https",
		Host:   accountName + dnsSuffix,
		Path:   "/" + strings.Trim(fileSystem, "/"),
	}
	return u.String()
}
