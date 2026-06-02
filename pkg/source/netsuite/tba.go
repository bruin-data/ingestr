package netsuite

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// tbaUsername is the literal value SuiteAnalytics Connect expects in the user
// field to switch a connection into token-based authentication mode. The four
// token values are never sent as connection properties; they only feed the
// token password computed below.
const tbaUsername = "TBA"

// tbaNonceFunc and tbaTimeFunc are indirected so tests can pin the nonce and
// timestamp that go into the token password.
var (
	tbaNonceFunc = generateNonce
	tbaTimeFunc  = unixNow
)

// tbaCredentials holds the inputs required to compute a SuiteAnalytics Connect
// token password for token-based authentication (TBA).
type tbaCredentials struct {
	accountID      string
	consumerKey    string
	consumerSecret string
	tokenID        string
	tokenSecret    string
}

// extractTBACredentials returns the TBA credentials encoded in the URI, or nil
// when token-based authentication is not requested. It errors when the request
// is partial (some but not all token values present) or missing the account ID
// needed to sign the token password.
func extractTBACredentials(accountID string, values url.Values) (*tbaCredentials, error) {
	consumerKey := firstNonEmpty(values.Get("consumer_key"), values.Get("client_id"))
	consumerSecret := firstNonEmpty(values.Get("consumer_secret"), values.Get("client_secret"))
	tokenID := firstNonEmpty(values.Get("token_id"), values.Get("token"))
	tokenSecret := values.Get("token_secret")

	supplied := []string{consumerKey, consumerSecret, tokenID, tokenSecret}
	provided := 0
	for _, value := range supplied {
		if value != "" {
			provided++
		}
	}
	if provided == 0 {
		return nil, nil
	}
	if provided < len(supplied) {
		return nil, fmt.Errorf("netsuite token-based authentication requires consumer_key, consumer_secret, token_id, and token_secret")
	}

	if accountID == "" {
		return nil, fmt.Errorf("account_id is required for netsuite token-based authentication")
	}

	return &tbaCredentials{
		accountID:      accountID,
		consumerKey:    consumerKey,
		consumerSecret: consumerSecret,
		tokenID:        tokenID,
		tokenSecret:    tokenSecret,
	}, nil
}

// dsnCustomProperties resolves a DSN's CustomProperties (e.g. AccountID,
// RoleID) from the ODBC ini referenced by the ODBCINI environment variable.
// Indirected so tests can supply values without touching the filesystem.
var dsnCustomProperties = readDSNCustomProperties

func readDSNCustomProperties(dsn string) map[string]string {
	path := os.Getenv("ODBCINI")
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return parseINICustomProperties(string(data), dsn)
}

// parseINICustomProperties returns the key/value pairs of the CustomProperties
// entry within the [dsn] section of an ODBC ini file (e.g. AccountID, RoleID).
func parseINICustomProperties(ini, dsn string) map[string]string {
	var raw string
	inSection := false
	for _, line := range strings.Split(ini, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, ";") || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			inSection = strings.EqualFold(section, dsn)
			continue
		}
		if !inSection {
			continue
		}
		key, value, ok := strings.Cut(trimmed, "=")
		if ok && strings.EqualFold(strings.TrimSpace(key), "CustomProperties") {
			raw = strings.TrimSpace(value)
			break
		}
	}
	if raw == "" {
		return nil
	}

	props := map[string]string{}
	for _, pair := range strings.Split(strings.Trim(raw, "{};"), ";") {
		k, v, ok := strings.Cut(pair, "=")
		if ok {
			props[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return props
}

// tokenPassword builds a single-use SuiteAnalytics Connect token password:
//
//	accountID&consumerKey&tokenID&nonce&timestamp&signature&HMAC-SHA256
//
// where signature is the Base64-encoded HMAC-SHA256 of the ampersand-joined
// base string, keyed by "consumerSecret&tokenSecret". The token password is
// valid for five minutes and a single session, so it must be regenerated for
// every physical connection.
func (c tbaCredentials) tokenPassword() (string, error) {
	nonce, err := tbaNonceFunc()
	if err != nil {
		return "", fmt.Errorf("failed to generate netsuite TBA nonce: %w", err)
	}
	timestamp := strconv.FormatInt(tbaTimeFunc(), 10)

	baseString := strings.Join([]string{
		oauthEncode(c.accountID),
		oauthEncode(c.consumerKey),
		oauthEncode(c.tokenID),
		oauthEncode(nonce),
		oauthEncode(timestamp),
	}, "&")

	key := oauthEncode(c.consumerSecret) + "&" + oauthEncode(c.tokenSecret)
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(baseString))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return baseString + "&" + signature + "&HMAC-SHA256", nil
}

// openConnection returns a *sql.DB for the parsed config. Password auth uses a
// static connection string; token-based authentication uses a connector that
// mints a fresh token password for every physical connection.
func openConnection(cfg uriConfig) (*sql.DB, error) {
	if cfg.tba == nil {
		return sql.Open("odbc", cfg.connString)
	}

	// Obtain the registered ODBC driver without importing its (cgo, build
	// constrained) package directly. A real connection is only made later, on
	// demand, by the connector.
	probe, err := sql.Open("odbc", "")
	if err != nil {
		return nil, err
	}
	drv := probe.Driver()
	_ = probe.Close()

	return sql.OpenDB(&tbaConnector{drv: drv, base: cfg.connString, creds: *cfg.tba}), nil
}

// tbaConnector regenerates a fresh token password for every physical
// connection opened by database/sql, then delegates to the underlying ODBC
// driver. This satisfies the single-use/short-lived nature of the token
// password across the connection pool and reconnects.
type tbaConnector struct {
	drv   driver.Driver
	base  string // connection string without credentials, terminated with ';'
	creds tbaCredentials
}

func (c *tbaConnector) Connect(_ context.Context) (driver.Conn, error) {
	password, err := c.creds.tokenPassword()
	if err != nil {
		return nil, err
	}
	dsn := c.base + "UID=" + tbaUsername + ";PWD=" + odbcValue(password) + ";"
	return c.drv.Open(dsn)
}

func (c *tbaConnector) Driver() driver.Driver {
	return c.drv
}

// oauthEncode percent-encodes a value per RFC 5849 / RFC 3986 (unreserved
// characters are left intact, everything else is percent-encoded). Real TBA
// inputs (hex keys, numeric account/timestamp, alphanumeric nonce) are already
// unreserved, so this is a no-op in practice but keeps edge cases correct.
func oauthEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') ||
			ch == '-' || ch == '.' || ch == '_' || ch == '~' {
			b.WriteByte(ch)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", ch)
	}
	return b.String()
}

const nonceAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

func unixNow() int64 {
	return time.Now().Unix()
}

func generateNonce() (string, error) {
	const length = 20
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = nonceAlphabet[int(buf[i])%len(nonceAlphabet)]
	}
	return string(buf), nil
}
