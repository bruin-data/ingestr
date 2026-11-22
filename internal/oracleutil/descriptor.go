// Package oracleutil holds URI handling shared by the Oracle source and
// destination.
package oracleutil

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// DescriptorParam is the query parameter that carries a full TNS connect
// descriptor, e.g. ?dsn=(DESCRIPTION=(ADDRESS=...)(CONNECT_DATA=...)).
const DescriptorParam = "dsn"

// goOraDescriptorParam is go-ora's own name for the descriptor option; it is
// accepted as an alias.
const goOraDescriptorParam = "connStr"

var schemes = map[string]bool{
	"oracle": true, "oracle+cx_oracle": true,
	"exadata": true, "oracle+exadata": true, "oracle_exadata": true,
}

var descriptorConflicts = []string{"service_name", "service name", "sid", "instance name", "server"}

// LiftInlineDescriptor rewrites a URI that carries a connect descriptor in the
// host position, scheme://user:pass@(DESCRIPTION=...)?opts, into the
// url.Parse-compatible scheme://user:pass@/?dsn=...&opts form. Non-Oracle URIs
// and URIs without an inline descriptor are returned unchanged.
func LiftInlineDescriptor(uri string) (string, error) {
	scheme, rest, ok := strings.Cut(uri, "://")
	if !ok || !schemes[strings.ToLower(scheme)] {
		return uri, nil
	}

	authorityEnd := strings.IndexAny(rest, "/?#")
	if authorityEnd < 0 {
		authorityEnd = len(rest)
	}
	for i := 0; i < authorityEnd; i++ {
		if rest[i] != '(' || (i > 0 && rest[i-1] != '@') {
			continue
		}
		if !looksLikeDescriptor(rest[i:]) {
			continue
		}
		end, err := descriptorEnd(rest[i:])
		if err != nil {
			return "", err
		}
		descriptor := rest[i : i+end]
		tail := rest[i+end:]

		var query string
		switch {
		case tail == "", tail == "/":
		case strings.HasPrefix(tail, "/?"):
			query = tail[2:]
		case strings.HasPrefix(tail, "?"):
			query = tail[1:]
		default:
			return "", errors.New("unexpected text after the Oracle connect descriptor; the service name belongs in its CONNECT_DATA section and driver options go in a ?query")
		}

		lifted := scheme + "://" + rest[:i] + "/?" + DescriptorParam + "=" + url.QueryEscape(descriptor)
		if query != "" {
			lifted += "&" + query
		}
		return lifted, nil
	}

	return uri, nil
}

// DescriptorConnString returns a go-ora connection string when u carries a
// connect descriptor in its query (?dsn= or go-ora's ?connStr=). The second
// return value is false when no descriptor is present.
func DescriptorConnString(u *url.URL) (string, bool, error) {
	query := u.Query()

	var descriptorKeys []string
	for key := range query {
		if strings.EqualFold(key, DescriptorParam) || strings.EqualFold(key, goOraDescriptorParam) {
			descriptorKeys = append(descriptorKeys, key)
		}
	}
	if len(descriptorKeys) == 0 {
		return "", false, nil
	}
	if len(descriptorKeys) > 1 || len(query[descriptorKeys[0]]) > 1 {
		return "", false, fmt.Errorf("pass the Oracle connect descriptor once, via ?%s=", DescriptorParam)
	}

	descriptor := strings.TrimSpace(query.Get(descriptorKeys[0]))
	if !strings.HasPrefix(descriptor, "(") {
		return "", false, fmt.Errorf("?%s= must be a full connect descriptor such as (DESCRIPTION=(ADDRESS=(PROTOCOL=TCP)(HOST=...)(PORT=1521))(CONNECT_DATA=(SERVICE_NAME=...))); TNS aliases from tnsnames.ora are not supported", DescriptorParam)
	}
	end, err := descriptorEnd(descriptor)
	if err != nil {
		return "", false, err
	}
	if end != len(descriptor) {
		return "", false, errors.New("unexpected text after the Oracle connect descriptor")
	}

	if u.Host != "" {
		return "", false, errors.New("a connect descriptor cannot be combined with a host or port in the URI; list the listeners in the descriptor's ADDRESS entries")
	}
	if strings.Trim(u.Path, "/") != "" {
		return "", false, errors.New("a connect descriptor cannot be combined with a database in the URI path; set SERVICE_NAME or SID in the descriptor's CONNECT_DATA")
	}
	for key := range query {
		for _, conflict := range descriptorConflicts {
			if strings.EqualFold(key, conflict) {
				return "", false, fmt.Errorf("?%s= cannot be combined with a connect descriptor; set it in the descriptor instead", key)
			}
		}
	}

	query.Del(descriptorKeys[0])
	query.Set(goOraDescriptorParam, descriptor)
	connURL := &url.URL{
		Scheme:   "oracle",
		User:     u.User,
		Host:     ":0",
		Path:     "/",
		RawQuery: query.Encode(),
	}
	return connURL.String(), true, nil
}

func looksLikeDescriptor(s string) bool {
	keyword := strings.TrimLeft(s[1:], " \t\r\n")
	return len(keyword) >= len("DESCRIPTION") && strings.EqualFold(keyword[:len("DESCRIPTION")], "DESCRIPTION")
}

// descriptorEnd returns the length of the balanced parenthesised expression at
// the start of s. Parentheses inside double-quoted values are ignored.
func descriptorEnd(s string) (int, error) {
	depth := 0
	inQuotes := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			inQuotes = !inQuotes
		case '(':
			if !inQuotes {
				depth++
			}
		case ')':
			if inQuotes {
				continue
			}
			depth--
			if depth < 0 {
				return 0, errors.New("unbalanced parentheses in the Oracle connect descriptor")
			}
			if depth == 0 {
				return i + 1, nil
			}
		}
	}
	return 0, errors.New("unbalanced parentheses in the Oracle connect descriptor")
}
