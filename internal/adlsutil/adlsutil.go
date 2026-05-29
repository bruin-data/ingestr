package adlsutil

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

const DNSSuffix = ".dfs.core.windows.net"

type ClientCredentials struct {
	TenantID     string
	ClientID     string
	ClientSecret string
}

func ParseClientCredentials(values url.Values) ClientCredentials {
	return ClientCredentials{
		TenantID:     values.Get("tenant_id"),
		ClientID:     values.Get("client_id"),
		ClientSecret: values.Get("client_secret"),
	}
}

func (c ClientCredentials) IsSet() bool {
	return c.TenantID != "" || c.ClientID != "" || c.ClientSecret != ""
}

func (c ClientCredentials) NewTokenCredential() (azcore.TokenCredential, error) {
	if !c.IsSet() {
		cred, err := azidentity.NewDefaultAzureCredential(nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create default Azure credential: %w", err)
		}
		return cred, nil
	}

	missing := make([]string, 0, 3)
	if c.TenantID == "" {
		missing = append(missing, "tenant_id")
	}
	if c.ClientID == "" {
		missing = append(missing, "client_id")
	}
	if c.ClientSecret == "" {
		missing = append(missing, "client_secret")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("incomplete service principal credentials, missing: %s", strings.Join(missing, ", "))
	}

	cred, err := azidentity.NewClientSecretCredential(c.TenantID, c.ClientID, c.ClientSecret, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create service principal credential: %w", err)
	}
	return cred, nil
}

func AppendSASToken(rawURL, sasToken string) string {
	if sasToken == "" {
		return rawURL
	}
	if strings.Contains(rawURL, "?") {
		return rawURL + "&" + sasToken
	}
	return rawURL + "?" + sasToken
}

func ParseAccountName(u *url.URL) string {
	if accountName := u.Query().Get("account_name"); accountName != "" {
		return accountName
	}

	host := u.Hostname()
	if strings.HasSuffix(host, DNSSuffix) {
		return strings.TrimSuffix(host, DNSSuffix)
	}
	if host != "" && !strings.Contains(host, ".") {
		return host
	}

	return ""
}

func FilesystemURL(accountName, fileSystem string) string {
	u := &url.URL{
		Scheme: "https",
		Host:   accountName + DNSSuffix,
		Path:   "/" + strings.Trim(fileSystem, "/"),
	}
	return u.String()
}

func PathURL(accountName, fileSystem, path string) (string, error) {
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
		Host:   accountName + DNSSuffix,
		Path:   "/" + fileSystem + "/" + path,
	}
	return u.String(), nil
}
