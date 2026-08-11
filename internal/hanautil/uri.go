package hanautil

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	hdbdriver "github.com/SAP/go-hdb/driver"
)

func ParseURI(uri string) (string, string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", "", err
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "hana" && scheme != "saphana" {
		return "", "", fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}
	if u.Hostname() == "" {
		return "", "", errors.New("host is required")
	}

	port := u.Port()
	if port == "" {
		port = "30015"
	}

	dsn := &url.URL{
		Scheme: hdbdriver.DriverName,
		Host:   net.JoinHostPort(u.Hostname(), port),
		User:   u.User,
	}
	database := strings.TrimPrefix(u.Path, "/")
	query := u.Query()
	if database != "" {
		query.Set(hdbdriver.DSNDefaultSchema, database)
	}
	if port == "443" && query.Get("TLSInsecureSkipVerify") == "" && query.Get("TLSServerName") == "" {
		query.Set("TLSServerName", u.Hostname())
	}
	dsn.RawQuery = query.Encode()

	return dsn.String(), database, nil
}
