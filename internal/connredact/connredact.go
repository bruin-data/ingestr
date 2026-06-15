// Package connredact strips host/user/password leaks from database driver errors.
package connredact

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// Redact handles pgx errors by type (their outer framing contains
// DNS-resolved IPs that aren't in uri). For any other error, host/user/
// password substrings from uri are replaced with placeholders.
func Redact(uri string, err error) error {
	if err == nil {
		return nil
	}
	var parseErr *pgconn.ParseConfigError
	if errors.As(err, &parseErr) {
		return errors.New("invalid connection string")
	}
	var connErr *pgconn.ConnectError
	if errors.As(err, &connErr) {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			return fmt.Errorf("failed to connect: %w", pgErr)
		}
		return errors.New("failed to connect")
	}
	if uri == "" {
		return err
	}
	repls := uriReplacements(uri)
	if len(repls) == 0 {
		return err
	}
	msg := err.Error()
	redactedMsg := applyReplacements(msg, repls)
	if redactedMsg == msg {
		return err
	}
	return &redactedErr{err: err, msg: redactedMsg}
}

type redactedErr struct {
	err error
	msg string
}

func (e *redactedErr) Error() string { return e.msg }
func (e *redactedErr) Unwrap() error { return e.err }

func uriReplacements(uri string) []string {
	u, perr := url.Parse(uri)
	if perr != nil {
		return nil
	}
	var r []string
	if h := u.Hostname(); h != "" {
		r = append(r, h, "<host>")
	}
	if u.User != nil {
		if name := u.User.Username(); name != "" {
			r = append(r, name, "<user>")
		}
		if pass, ok := u.User.Password(); ok && pass != "" {
			r = append(r, pass, "<password>")
		}
	}
	return r
}

func applyReplacements(s string, repls []string) string {
	for i := 0; i+1 < len(repls); i += 2 {
		s = strings.ReplaceAll(s, repls[i], repls[i+1])
	}
	return s
}
