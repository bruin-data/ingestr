package adlsutil

import (
	"fmt"
	"net/url"
	"strings"
)

const DNSSuffix = ".dfs.core.windows.net"

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
