// Package mysqluri converts ingestr's MySQL-family URIs (mysql, mariadb,
// vitess, ps_mysql) into go-sql-driver DSNs. It is shared by the MySQL
// source and destination so scheme handling and the PlanetScale TLS default
// cannot drift between the two.
package mysqluri

import (
	"fmt"
	"net/url"
	"strings"
)

// IsMySQLFamilyScheme reports whether scheme names a MySQL-wire connector:
// MySQL/MariaDB, Vitess, or PlanetScale MySQL.
func IsMySQLFamilyScheme(scheme string) bool {
	switch {
	case strings.HasPrefix(scheme, "mysql"), scheme == "mariadb", scheme == "vitess", scheme == "ps_mysql":
		return true
	default:
		return false
	}
}

// ParseURL parses a MySQL-family URI. url.Parse rejects scheme characters
// ingestr allows (the underscore in ps_mysql), so the scheme is split off
// manually, the remainder is parsed under a placeholder, and the original
// scheme is restored on the result.
func ParseURL(rawURI string) (*url.URL, error) {
	idx := strings.Index(rawURI, "://")
	if idx == -1 {
		return url.Parse(rawURI)
	}
	u, err := url.Parse("scheme" + rawURI[idx:])
	if err != nil {
		return nil, err
	}
	u.Scheme = strings.ToLower(rawURI[:idx])
	return u, nil
}

// ToDSN converts a MySQL-family URI to the DSN format expected by
// go-sql-driver/mysql and returns the DSN and the database name.
//
//	URI format: mysql://user:pass@host:port/database?params
//	DSN format: user:pass@tcp(host:port)/database?params
func ToDSN(uri string) (string, string, error) {
	u, err := ParseURL(uri)
	if err != nil {
		return "", "", err
	}

	scheme := u.Scheme
	if !IsMySQLFamilyScheme(scheme) {
		return "", "", fmt.Errorf("unsupported scheme: %s", scheme)
	}

	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "3306"
	}

	var user, password string
	if u.User != nil {
		user = u.User.Username()
		password, _ = u.User.Password()
	}

	database := strings.TrimPrefix(u.Path, "/")

	dsn := ""
	if user != "" {
		dsn = user
		if password != "" {
			dsn += ":" + password
		}
		dsn += "@"
	}
	dsn += fmt.Sprintf("tcp(%s:%s)/%s", host, port, database)

	query := u.Query()
	query.Set("parseTime", "true")
	// PlanetScale requires TLS on MySQL-wire connections and rejects plaintext
	// with "client must use SSL/TLS". Enable it automatically for the ps_mysql
	// scheme and *.psdb.cloud hosts unless the caller already set a tls value, so
	// tls=skip-verify or a custom config still wins.
	if !query.Has("tls") && (scheme == "ps_mysql" || strings.HasSuffix(strings.ToLower(host), ".psdb.cloud")) {
		query.Set("tls", "true")
	}
	dsn += "?" + query.Encode()

	return dsn, database, nil
}
