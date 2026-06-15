package connredact

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestRedact_PgxConnectError(t *testing.T) {
	uri := "postgres://leaky_user:hunter2@db.example.invalid:5432/leaky_db?connect_timeout=1"
	cfg, err := pgconn.ParseConfig(uri)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, connectErr := pgconn.ConnectConfig(ctx, cfg)
	if connectErr == nil {
		t.Fatal("expected connect to fail")
	}

	got := Redact(uri, connectErr).Error()
	for _, leak := range []string{"leaky_user", "leaky_db", "db.example.invalid", "hunter2", "user=", "database="} {
		if strings.Contains(got, leak) {
			t.Errorf("redacted error still contains %q: %s", leak, got)
		}
	}
	if got != "failed to connect" {
		t.Errorf("expected %q, got: %s", "failed to connect", got)
	}
}

func TestRedact_PgxParseConfigError(t *testing.T) {
	_, err := pgconn.ParseConfig("postgres://leaky_user:hunter2@db.example.invalid/leaky_db?sslmode=bogus")
	if err == nil {
		t.Fatal("expected ParseConfig to fail")
	}
	got := Redact("", err).Error()
	for _, leak := range []string{"leaky_user", "leaky_db", "db.example.invalid"} {
		if strings.Contains(got, leak) {
			t.Errorf("redacted error still contains %q: %s", leak, got)
		}
	}
	if got != "invalid connection string" {
		t.Errorf("expected %q, got: %s", "invalid connection string", got)
	}
}

func TestRedact_NonPgxStripsURISubstrings(t *testing.T) {
	uri := "mysql://leaky_user:hunter2@db.example.invalid:3306/leaky_db"
	cases := []struct {
		name, raw string
	}{
		{"host", "dial tcp: lookup db.example.invalid: no such host"},
		{"user", "Error 1045 (28000): Access denied for user 'leaky_user'@'10.0.0.1'"},
		{"password", "auth failed (password=hunter2)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Redact(uri, errors.New(tc.raw)).Error()
			for _, leak := range []string{"db.example.invalid", "leaky_user", "hunter2"} {
				if strings.Contains(got, leak) {
					t.Errorf("redacted output still contains %q: %s", leak, got)
				}
			}
		})
	}
}

// Greptile P2: post-connection query errors (PgError not inside a
// ConnectError) must pass through unchanged. The server-side message can't
// contain our URI host/user/password, so substring stripping would only
// false-positive when a coincidental substring overlap exists (e.g.
// hostname "orders" inside the column name "orders_id").
func TestRedact_LiveConnectionPgErrorPassesThrough(t *testing.T) {
	uri := "postgres://alice:hunter2@orders.db.example.com:5432/orders_db"
	pgErr := &pgconn.PgError{
		Severity: "ERROR",
		Code:     "42703",
		Message:  `column "orders_id" does not exist`,
	}
	got := Redact(uri, pgErr).Error()
	if !strings.Contains(got, "orders_id") {
		t.Errorf("expected column name preserved verbatim, got: %s", got)
	}
}

// Greptile P2: if the password starts with the hostname (e.g. host "prod",
// password "prod_secret"), naive in-order replacement rewrites the host first
// and leaves the "_secret" tail visible. Longest-first ordering prevents that.
func TestRedact_OverlappingHostAndPassword(t *testing.T) {
	uri := "mysql://user:prod_secret@prod/db"
	raw := errors.New("auth failed for prod_secret on host prod")
	got := Redact(uri, raw).Error()
	if strings.Contains(got, "prod_secret") || strings.Contains(got, "_secret") {
		t.Errorf("password tail leaked: %s", got)
	}
}

func TestRedact_KeepsDriverDetail(t *testing.T) {
	uri := "mysql://leaky_user:hunter2@db.example.invalid:3306/leaky_db"
	got := Redact(uri, errors.New("dial tcp: lookup db.example.invalid: no such host")).Error()
	if !strings.Contains(got, "no such host") || !strings.Contains(got, "<host>") {
		t.Errorf("expected driver detail + placeholder, got: %s", got)
	}
}

func TestRedact_PreservesChain(t *testing.T) {
	uri := "mysql://u:p@h/d"
	sentinel := errors.New("h: no such host")
	if !errors.Is(Redact(uri, sentinel), sentinel) {
		t.Error("errors.Is should still find the wrapped sentinel")
	}
}

func TestRedact_PreservesPgxChainForErrorsAs(t *testing.T) {
	// ConnectError path: caller can still type-assert the pgx error after Redact.
	cfg, err := pgconn.ParseConfig("postgres://leaky_user:hunter2@db.example.invalid:5432/leaky_db?connect_timeout=1")
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, connectErr := pgconn.ConnectConfig(ctx, cfg)
	if connectErr == nil {
		t.Fatal("expected connect to fail")
	}
	wrapped := Redact("", connectErr)
	var ce *pgconn.ConnectError
	if !errors.As(wrapped, &ce) {
		t.Errorf("errors.As should still find *pgconn.ConnectError; got %T (%s)", wrapped, wrapped)
	}

	// ParseConfigError path: caller can still type-assert.
	_, perr := pgconn.ParseConfig("postgres://x:y@h/d?sslmode=bogus")
	if perr == nil {
		t.Fatal("expected ParseConfig to fail")
	}
	wrappedP := Redact("", perr)
	var pe *pgconn.ParseConfigError
	if !errors.As(wrappedP, &pe) {
		t.Errorf("errors.As should still find *pgconn.ParseConfigError; got %T (%s)", wrappedP, wrappedP)
	}
}

func TestRedact_PassesThroughOtherErrors(t *testing.T) {
	if got := Redact("", errors.New("x")); got.Error() != "x" {
		t.Errorf("expected passthrough, got: %v", got)
	}
	if got := Redact("mysql://alice:hunter2@db.example.com/inv", errors.New("driver: bad input")); got.Error() != "driver: bad input" {
		t.Errorf("non-leaking error should passthrough, got: %s", got.Error())
	}
	if Redact("", nil) != nil {
		t.Error("nil err should return nil")
	}
	if got := Redact(":::not-a-uri", errors.New("y")).Error(); got != "y" {
		t.Errorf("bad uri should passthrough, got: %s", got)
	}
}
