package snowflake

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/url"
	"testing"

	sf "github.com/snowflakedb/gosnowflake"
	"github.com/youmark/pkcs8"
)

// --- ParseURI tests ---

func TestParseURI_PasswordAuth(t *testing.T) {
	auth, err := ParseURI("snowflake://myuser:mypass@myaccount/mydb?warehouse=WH&role=ADMIN")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if auth.Account != "myaccount" {
		t.Errorf("expected account 'myaccount', got %q", auth.Account)
	}
	if auth.User != "myuser" {
		t.Errorf("expected user 'myuser', got %q", auth.User)
	}
	if auth.Password != "mypass" {
		t.Errorf("expected password 'mypass', got %q", auth.Password)
	}
	if auth.Database != "mydb" {
		t.Errorf("expected database 'mydb', got %q", auth.Database)
	}
	if auth.Warehouse != "WH" {
		t.Errorf("expected warehouse 'WH', got %q", auth.Warehouse)
	}
	if auth.Role != "ADMIN" {
		t.Errorf("expected role 'ADMIN', got %q", auth.Role)
	}
	if auth.Authenticator != sf.AuthTypeSnowflake {
		t.Errorf("expected AuthTypeSnowflake, got %v", auth.Authenticator)
	}
}

func TestParseURI_PasswordAuthWithSchema(t *testing.T) {
	auth, err := ParseURI("snowflake://myuser:mypass@myaccount/mydb/myschema?warehouse=WH")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if auth.Database != "mydb" {
		t.Errorf("expected database 'mydb', got %q", auth.Database)
	}
	if auth.Schema != "myschema" {
		t.Errorf("expected schema 'myschema', got %q", auth.Schema)
	}
}

func TestParseURI_PasswordWithSpecialChars(t *testing.T) {
	auth, err := ParseURI("snowflake://myuser:p%40ss%23word@myaccount/mydb")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if auth.Password != "p@ss#word" {
		t.Errorf("expected password 'p@ss#word', got %q", auth.Password)
	}
}

func TestParseURI_AccountWithOrgFormat(t *testing.T) {
	auth, err := ParseURI("snowflake://myuser:mypass@myorg-myaccount/mydb")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if auth.Account != "myorg-myaccount" {
		t.Errorf("expected account 'myorg-myaccount', got %q", auth.Account)
	}
}

func TestParseURI_MissingAccount(t *testing.T) {
	_, err := ParseURI("snowflake:///mydb")
	if err == nil {
		t.Fatal("expected error for missing account")
	}
}

func TestParseURI_MissingPassword(t *testing.T) {
	_, err := ParseURI("snowflake://myuser@myaccount/mydb")
	if err == nil {
		t.Fatal("expected error for missing password with default auth")
	}
}

func TestParseURI_MissingUser(t *testing.T) {
	_, err := ParseURI("snowflake://:mypass@myaccount/mydb")
	if err == nil {
		t.Fatal("expected error for missing user with default auth")
	}
}

// --- Authenticator type tests ---

func TestParseURI_ExplicitSnowflakeAuthenticator(t *testing.T) {
	auth, err := ParseURI("snowflake://myuser:mypass@myaccount/mydb?authenticator=snowflake")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if auth.Authenticator != sf.AuthTypeSnowflake {
		t.Errorf("expected AuthTypeSnowflake, got %v", auth.Authenticator)
	}
}

func TestParseURI_ExternalBrowser(t *testing.T) {
	auth, err := ParseURI("snowflake://myuser@myaccount/mydb?authenticator=externalbrowser")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if auth.Authenticator != sf.AuthTypeExternalBrowser {
		t.Errorf("expected AuthTypeExternalBrowser, got %v", auth.Authenticator)
	}
}

func TestParseURI_ExplicitJwtAuthenticator(t *testing.T) {
	key := generateTestRSAKey(t)
	keyPEM := encodeRSAKeyToPEM(t, key)

	uri := "snowflake://myuser@myaccount/mydb?authenticator=jwt&private_key=" + url.QueryEscape(string(keyPEM))
	auth, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if auth.Authenticator != sf.AuthTypeJwt {
		t.Errorf("expected AuthTypeJwt, got %v", auth.Authenticator)
	}
}

func TestParseURI_UnknownAuthenticatorDefaultsToSnowflake(t *testing.T) {
	_, err := ParseURI("snowflake://myuser@myaccount/mydb?authenticator=somethingelse")
	if err == nil {
		t.Fatal("expected error since unknown authenticator defaults to snowflake (requires password)")
	}
}

// --- OAuth tests ---

func TestParseURI_OAuth(t *testing.T) {
	auth, err := ParseURI("snowflake://myuser@myaccount/mydb?authenticator=oauth&token=mytoken123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if auth.Authenticator != sf.AuthTypeOAuth {
		t.Errorf("expected AuthTypeOAuth, got %v", auth.Authenticator)
	}
	if auth.Token != "mytoken123" {
		t.Errorf("expected token 'mytoken123', got %q", auth.Token)
	}
}

func TestParseURI_OAuthMissingToken(t *testing.T) {
	_, err := ParseURI("snowflake://myuser@myaccount/mydb?authenticator=oauth")
	if err == nil {
		t.Fatal("expected error for OAuth without token")
	}
}

// --- Key-pair tests ---

func TestParseURI_KeyPairInlineBase64PEM(t *testing.T) {
	key := generateTestRSAKey(t)
	keyPEM := encodeRSAKeyToPEM(t, key)
	b64Key := base64.StdEncoding.EncodeToString(keyPEM)

	uri := "snowflake://myuser@myaccount/mydb?private_key=" + url.QueryEscape(b64Key)
	auth, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if auth.Authenticator != sf.AuthTypeJwt {
		t.Errorf("expected AuthTypeJwt, got %v", auth.Authenticator)
	}
	if auth.PrivateKey == nil {
		t.Fatal("expected private key to be set")
	}
}

func TestParseURI_KeyPairInlineRawPEM(t *testing.T) {
	key := generateTestRSAKey(t)
	keyPEM := encodeRSAKeyToPEM(t, key)

	uri := "snowflake://myuser@myaccount/mydb?private_key=" + url.QueryEscape(string(keyPEM))
	auth, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if auth.PrivateKey == nil {
		t.Fatal("expected private key to be set")
	}
}

func TestParseURI_KeyPairInlineBase64DER(t *testing.T) {
	key := generateTestRSAKey(t)
	derBytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("failed to marshal key: %v", err)
	}
	b64Key := base64.StdEncoding.EncodeToString(derBytes)

	uri := "snowflake://myuser@myaccount/mydb?private_key=" + url.QueryEscape(b64Key)
	auth, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if auth.PrivateKey == nil {
		t.Fatal("expected private key to be set")
	}
}

func TestParseURI_KeyPairWithPasswordIgnoresPassword(t *testing.T) {
	key := generateTestRSAKey(t)
	keyPEM := encodeRSAKeyToPEM(t, key)

	uri := "snowflake://myuser:shouldbeignored@myaccount/mydb?private_key=" + url.QueryEscape(string(keyPEM))
	auth, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if auth.Authenticator != sf.AuthTypeJwt {
		t.Errorf("expected AuthTypeJwt (key-pair overrides password auth), got %v", auth.Authenticator)
	}
	if auth.PrivateKey == nil {
		t.Fatal("expected private key to be set")
	}
}

// --- ToConfig tests ---

func TestToConfig_PasswordAuth(t *testing.T) {
	auth, err := ParseURI("snowflake://myuser:mypass@myaccount/mydb?warehouse=WH&role=ADMIN")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg := auth.ToConfig()
	if cfg.Account != "myaccount" {
		t.Errorf("expected account 'myaccount', got %q", cfg.Account)
	}
	if cfg.User != "myuser" {
		t.Errorf("expected user 'myuser', got %q", cfg.User)
	}
	if cfg.Password != "mypass" {
		t.Errorf("expected password 'mypass', got %q", cfg.Password)
	}
	if cfg.Warehouse != "WH" {
		t.Errorf("expected warehouse 'WH', got %q", cfg.Warehouse)
	}
	if cfg.Role != "ADMIN" {
		t.Errorf("expected role 'ADMIN', got %q", cfg.Role)
	}
}

func TestToConfig_OAuth(t *testing.T) {
	auth, err := ParseURI("snowflake://myuser@myaccount/mydb?authenticator=oauth&token=tok123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg := auth.ToConfig()
	if cfg.Authenticator != sf.AuthTypeOAuth {
		t.Errorf("expected AuthTypeOAuth, got %v", cfg.Authenticator)
	}
	if cfg.Token != "tok123" {
		t.Errorf("expected token 'tok123', got %q", cfg.Token)
	}
}

func TestToConfig_KeyPair(t *testing.T) {
	key := generateTestRSAKey(t)
	keyPEM := encodeRSAKeyToPEM(t, key)

	uri := "snowflake://myuser@myaccount/mydb?private_key=" + url.QueryEscape(string(keyPEM))
	auth, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg := auth.ToConfig()
	if cfg.Authenticator != sf.AuthTypeJwt {
		t.Errorf("expected AuthTypeJwt, got %v", cfg.Authenticator)
	}
	if cfg.PrivateKey == nil {
		t.Fatal("expected private key to be set on config")
	}
}

func TestToConfig_ExternalBrowser(t *testing.T) {
	auth, err := ParseURI("snowflake://myuser@myaccount/mydb?authenticator=externalbrowser")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg := auth.ToConfig()
	if cfg.Authenticator != sf.AuthTypeExternalBrowser {
		t.Errorf("expected AuthTypeExternalBrowser, got %v", cfg.Authenticator)
	}
}

// --- ToDSN tests ---

func TestToDSN_PasswordAuth(t *testing.T) {
	auth, err := ParseURI("snowflake://myuser:mypass@myaccount/mydb?warehouse=WH")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dsn, err := auth.ToDSN()
	if err != nil {
		t.Fatalf("unexpected error from ToDSN: %v", err)
	}
	if dsn == "" {
		t.Fatal("expected non-empty DSN")
	}
}

func TestToDSN_KeyPair(t *testing.T) {
	key := generateTestRSAKey(t)
	keyPEM := encodeRSAKeyToPEM(t, key)

	uri := "snowflake://myuser@myaccount/mydb?private_key=" + url.QueryEscape(string(keyPEM))
	auth, err := ParseURI(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dsn, err := auth.ToDSN()
	if err != nil {
		t.Fatalf("unexpected error from ToDSN: %v", err)
	}
	if dsn == "" {
		t.Fatal("expected non-empty DSN")
	}
}

func TestToDSN_OAuth(t *testing.T) {
	auth, err := ParseURI("snowflake://myuser@myaccount/mydb?authenticator=oauth&token=tok123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dsn, err := auth.ToDSN()
	if err != nil {
		t.Fatalf("unexpected error from ToDSN: %v", err)
	}
	if dsn == "" {
		t.Fatal("expected non-empty DSN")
	}
}

func TestToDSN_ExternalBrowser(t *testing.T) {
	auth, err := ParseURI("snowflake://myuser@myaccount/mydb?authenticator=externalbrowser")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dsn, err := auth.ToDSN()
	if err != nil {
		t.Fatalf("unexpected error from ToDSN: %v", err)
	}
	if dsn == "" {
		t.Fatal("expected non-empty DSN")
	}
}

// --- DecodePrivateKey tests ---

func TestDecodePrivateKey_PEM(t *testing.T) {
	key := generateTestRSAKey(t)
	keyPEM := encodeRSAKeyToPEM(t, key)

	decoded, err := DecodePrivateKey(string(keyPEM), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decoded == nil {
		t.Fatal("expected non-nil key")
	}
}

func TestDecodePrivateKey_Base64DER(t *testing.T) {
	key := generateTestRSAKey(t)
	derBytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("failed to marshal key: %v", err)
	}
	b64 := base64.StdEncoding.EncodeToString(derBytes)

	decoded, err := DecodePrivateKey(b64, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decoded == nil {
		t.Fatal("expected non-nil key")
	}
}

func TestDecodePrivateKey_Base64PEM(t *testing.T) {
	key := generateTestRSAKey(t)
	keyPEM := encodeRSAKeyToPEM(t, key)
	b64 := base64.StdEncoding.EncodeToString(keyPEM)

	decoded, err := DecodePrivateKey(b64, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decoded == nil {
		t.Fatal("expected non-nil key")
	}
}

func TestDecodePrivateKey_PKCS1(t *testing.T) {
	key := generateTestRSAKey(t)
	derBytes := x509.MarshalPKCS1PrivateKey(key)
	pemBlock := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: derBytes,
	})

	decoded, err := DecodePrivateKey(string(pemBlock), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decoded == nil {
		t.Fatal("expected non-nil key")
	}
}

func TestDecodePrivateKey_EncryptedPKCS8(t *testing.T) {
	key := generateTestRSAKey(t)
	passphrase := "test-passphrase"

	derBytes, err := pkcs8.MarshalPrivateKey(key, []byte(passphrase), nil)
	if err != nil {
		t.Fatalf("failed to marshal encrypted key: %v", err)
	}
	pemData := pem.EncodeToMemory(&pem.Block{
		Type:  "ENCRYPTED PRIVATE KEY",
		Bytes: derBytes,
	})

	decoded, err := DecodePrivateKey(string(pemData), passphrase)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decoded == nil {
		t.Fatal("expected non-nil key")
	}
}

func TestDecodePrivateKey_Invalid(t *testing.T) {
	_, err := DecodePrivateKey("not-a-valid-key", "")
	if err == nil {
		t.Fatal("expected error for invalid key data")
	}
}

// --- helpers ---

func generateTestRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}
	return key
}

func encodeRSAKeyToPEM(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()
	derBytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("failed to marshal key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: derBytes,
	})
}
