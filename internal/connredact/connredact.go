// Package connredact strips host/user/password leaks from database driver errors.
package connredact

import (
	"errors"
	"net"
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
			// Connect-time PgErrors (auth failure, missing database, missing
			// role) embed the username/database/role from our URI verbatim,
			// e.g. `password authentication failed for user "alice"`. Strip
			// those before appending.
			msg += ": " + applyReplacements(pgErr.Error(), uriReplacements(uri))
		}
		return &redactedErr{err: err, msg: msg}
	}
	// Live-connection PgError — server text from a successful connection
	// (column/table/constraint errors etc.), structurally can't contain URI
	// fields, and substring scan here would false-positive on coincidental
	// overlaps (e.g. hostname "orders" inside the column name "orders_id").
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return err
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
	// u.Host may be comma-separated (mongo replica sets: host1:p1,host2:p2,...).
	// u.Hostname() only handles single-host strings.
	for _, hp := range strings.Split(u.Host, ",") {
		h, _, err := net.SplitHostPort(strings.TrimSpace(hp))
		if err != nil {
			h = strings.TrimSpace(hp)
		}
		if h != "" {
			r = append(r, replacement{h, "<host>"})
		}
	}
	if u.User != nil {
		if name := u.User.Username(); name != "" {
			r = append(r, replacement{name, "<user>"})
		}
		if pass, ok := u.User.Password(); ok && pass != "" {
			r = append(r, replacement{pass, "<password>"})
		}
	}
	// Longest first — prevents partial rewrite when one needle contains another.
	sort.Slice(r, func(i, j int) bool { return len(r[i].needle) > len(r[j].needle) })
	return r
}

func applyReplacements(s string, repls []replacement) string {
	for _, r := range repls {
		s = strings.ReplaceAll(s, r.needle, r.label)
	}
	return s
}
