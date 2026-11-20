package adlsutil

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
)

// Get Blob echoes a blob's stored Content-Encoding, such as gzip on Cloudflare
// Logpush files. Without an explicit Accept-Encoding, Go's transport asks for
// gzip itself and silently decompresses those bodies, so a .gz key arrives as
// plain text. Asking for identity keeps the stored bytes.
type identityEncodingPolicy struct{}

func (identityEncodingPolicy) Do(req *policy.Request) (*http.Response, error) {
	req.Raw().Header.Set("Accept-Encoding", "identity")
	return req.Next()
}

var blobClientOptions = &azblob.ClientOptions{ClientOptions: policy.ClientOptions{
	PerCallPolicies: []policy.Policy{identityEncodingPolicy{}},
}}

// NewBlobClient builds an Azure Blob Storage client. Credentials are tried in
// order: connection string, account key, SAS token, then service principal or
// DefaultAzureCredential.
func NewBlobClient(accountName, accountKey, sasToken, connectionString string, creds ClientCredentials) (*azblob.Client, error) {
	if connectionString != "" {
		client, err := azblob.NewClientFromConnectionString(connectionString, blobClientOptions)
		if err != nil {
			return nil, fmt.Errorf("failed to create client from connection string: %w", err)
		}
		return client, nil
	}

	if accountName == "" {
		return nil, fmt.Errorf("account_name or connection_string is required for Azure Blob Storage")
	}
	serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net/", accountName)

	if accountKey != "" {
		cred, err := azblob.NewSharedKeyCredential(accountName, accountKey)
		if err != nil {
			return nil, fmt.Errorf("failed to create shared key credential: %w", err)
		}
		return azblob.NewClientWithSharedKeyCredential(serviceURL, cred, blobClientOptions)
	}

	if sasToken != "" {
		return azblob.NewClientWithNoCredential(AppendSASToken(serviceURL, strings.TrimPrefix(sasToken, "?")), blobClientOptions)
	}

	cred, err := creds.NewTokenCredential()
	if err != nil {
		return nil, err
	}
	return azblob.NewClient(serviceURL, cred, blobClientOptions)
}
