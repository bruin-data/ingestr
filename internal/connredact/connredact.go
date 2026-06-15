// Package connredact strips host/user/password leaks from database driver errors.
package connredact

import (
	"errors"
	"net/url"
	"sort"
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
		return &redactedErr{err: err, msg: "invalid connection string"}
	}
	var connErr *pgconn.ConnectError
	if errors.As(err, &connErr) {
		msg := "failed to connect"
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			msg += ": " + pgErr.Error()
		}
		return &redactedErr{err: err, msg: msg}
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

type replacement struct{ needle, label string }

func uriReplacements(uri string) []replacement {
	u, perr := url.Parse(uri)
	if perr != nil {
		return nil
	}
	var r []replacement
	if h := u.Hostname(); h != "" {
		r = append(r, replacement{h, "<host>"})
	}
	if u.User != nil {
		if name := u.User.Username(); name != "" {
			r = append(r, replacement{name, "<user>"})
		}
		if pass, ok := u.User.Password(); ok && pass != "" {
			r = append(r, replacement{pass, "<password>"})
		}
	}
	// Replace longest substrings first so that a needle which contains another
	// (e.g. host "prod" inside password "prod_secret") doesn't get partially
	// rewritten — which would prevent the longer match from firing and leak
	// the tail of the password.
	sort.Slice(r, func(i, j int) bool { return len(r[i].needle) > len(r[j].needle) })
	return r
}

func applyReplacements(s string, repls []replacement) string {
	for _, r := range repls {
		s = strings.ReplaceAll(s, r.needle, r.label)
	}
	return s
}
