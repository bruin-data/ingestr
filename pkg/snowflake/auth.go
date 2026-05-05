package snowflake

import (
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net/url"
	"strings"

	sf "github.com/snowflakedb/gosnowflake"
	"github.com/youmark/pkcs8"
)

type Auth struct {
	Account              string
	User                 string
	Password             string
	Database             string
	Schema               string
	Warehouse            string
	Role                 string
	Authenticator        sf.AuthType
	Token                string
	PrivateKey           *rsa.PrivateKey
	PrivateKeyRaw        string
	PrivateKeyPassphrase string
	ExtraParams          url.Values
}

func ParseURI(uri string) (*Auth, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("invalid Snowflake URI: %w", err)
	}

	auth := &Auth{}

	auth.Account = u.Hostname()
	if auth.Account == "" {
		return nil, fmt.Errorf("snowflake URI must include account as host")
	}

	if u.User != nil {
		auth.User = u.User.Username()
		auth.Password, _ = u.User.Password()
	}

	pathParts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(pathParts) >= 1 && pathParts[0] != "" {
		auth.Database = pathParts[0]
	}
	if len(pathParts) >= 2 && pathParts[1] != "" {
		auth.Schema = pathParts[1]
	}

	query := u.Query()
	auth.Warehouse = query.Get("warehouse")
	auth.Role = query.Get("role")
	auth.Token = query.Get("token")
	auth.PrivateKeyRaw = query.Get("private_key")
	auth.PrivateKeyPassphrase = query.Get("private_key_passphrase")
	auth.ExtraParams = StripAuthParams(query)

	authenticator := strings.ToLower(query.Get("authenticator"))
	switch authenticator {
	case "", "snowflake":
		auth.Authenticator = sf.AuthTypeSnowflake
	case "externalbrowser":
		auth.Authenticator = sf.AuthTypeExternalBrowser
	case "oauth":
		auth.Authenticator = sf.AuthTypeOAuth
	case "jwt":
		auth.Authenticator = sf.AuthTypeJwt
	default:
		auth.Authenticator = sf.AuthTypeSnowflake
	}

	if err := auth.resolvePrivateKey(); err != nil {
		return nil, fmt.Errorf("failed to resolve private key: %w", err)
	}

	if err := auth.validate(); err != nil {
		return nil, err
	}

	return auth, nil
}

func (a *Auth) resolvePrivateKey() error {
	if a.PrivateKeyRaw == "" {
		return nil
	}

	key, err := DecodePrivateKey(a.PrivateKeyRaw, a.PrivateKeyPassphrase)
	if err != nil {
		return err
	}

	a.PrivateKey = key
	if a.Authenticator == sf.AuthTypeSnowflake {
		a.Authenticator = sf.AuthTypeJwt
	}

	return nil
}

func (a *Auth) validate() error {
	if a.Authenticator == sf.AuthTypeSnowflake && a.PrivateKey == nil {
		if a.User == "" || a.Password == "" {
			return fmt.Errorf("snowflake password auth requires both username and password in the URI (snowflake://user:password@account/database)")
		}
	}

	if a.Authenticator == sf.AuthTypeOAuth && a.Token == "" {
		return fmt.Errorf("snowflake OAuth auth requires a token parameter (?token=<access_token>)")
	}

	if a.Authenticator == sf.AuthTypeJwt && a.PrivateKey == nil {
		return fmt.Errorf("snowflake JWT auth requires a private_key parameter (?private_key=<key>)")
	}

	return nil
}

func (a *Auth) ToConfig() *sf.Config {
	cfg := &sf.Config{
		Account:   a.Account,
		User:      a.User,
		Password:  a.Password,
		Database:  a.Database,
		Schema:    a.Schema,
		Warehouse: a.Warehouse,
		Role:      a.Role,
	}

	if a.Authenticator != sf.AuthTypeSnowflake {
		cfg.Authenticator = a.Authenticator
	}

	if a.Token != "" {
		cfg.Token = a.Token
	}

	if a.PrivateKey != nil {
		cfg.PrivateKey = a.PrivateKey
	}

	return cfg
}

func (a *Auth) ToDSN() (string, error) {
	return sf.DSN(a.ToConfig())
}

func OpenDB(uri string) (*sql.DB, error) {
	auth, err := ParseURI(uri)
	if err != nil {
		return nil, err
	}

	dsn, err := auth.ToDSN()
	if err != nil {
		return nil, err
	}

	return sql.Open("snowflake", dsn)
}

func StripAuthParams(query url.Values) url.Values {
	authParams := map[string]bool{
		"authenticator":          true,
		"token":                  true,
		"private_key":            true,
		"private_key_passphrase": true,
	}
	clean := make(url.Values)
	for k, v := range query {
		if !authParams[k] {
			clean[k] = v
		}
	}
	return clean
}

func DecodePrivateKey(keyData string, passphrase string) (*rsa.PrivateKey, error) {
	keyData = strings.TrimSpace(keyData)

	var derBytes []byte

	if strings.HasPrefix(keyData, "-----BEGIN") {
		block, _ := pem.Decode([]byte(keyData))
		if block == nil {
			return nil, fmt.Errorf("failed to decode PEM block")
		}
		derBytes = block.Bytes
	} else {
		decoded, err := base64.StdEncoding.DecodeString(keyData)
		if err != nil {
			decoded, err = base64.RawStdEncoding.DecodeString(keyData)
			if err != nil {
				return nil, fmt.Errorf("failed to base64-decode private key: %w", err)
			}
		}

		if block, _ := pem.Decode(decoded); block != nil {
			derBytes = block.Bytes
		} else {
			derBytes = decoded
		}
	}

	if passphrase != "" {
		key, err := pkcs8.ParsePKCS8PrivateKeyRSA(derBytes, []byte(passphrase))
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt private key with provided passphrase: %w", err)
		}
		return key, nil
	}

	if key, err := x509.ParsePKCS8PrivateKey(derBytes); err == nil {
		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
		return nil, fmt.Errorf("private key is not RSA")
	}

	if key, err := x509.ParsePKCS1PrivateKey(derBytes); err == nil {
		return key, nil
	}

	return nil, fmt.Errorf("failed to parse private key (tried PKCS#8 and PKCS#1)")
}
