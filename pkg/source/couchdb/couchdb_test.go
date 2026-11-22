package couchdb

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseURI(t *testing.T) {
	for _, tc := range []struct {
		uri, scheme, host, user, password string
	}{
		{"couchdb://localhost", "http", "localhost:5984", "", ""},
		{"couchdb://alice:p%40ss@localhost:15984/", "http", "localhost:15984", "alice", "p@ss"},
		{"couchdb://[::1]", "http", "[::1]:5984", "", ""},
		{"couchdb+https://db.example.com", "https", "db.example.com", "", ""},
		{"couchdb+https://db.example.com:6984", "https", "db.example.com:6984", "", ""},
	} {
		t.Run(tc.uri, func(t *testing.T) {
			u, err := parseURI(tc.uri)
			require.NoError(t, err)
			require.Equal(t, tc.scheme, u.Scheme)
			require.Equal(t, tc.host, u.Host)
			require.Equal(t, tc.user, u.User.Username())
			password, _ := u.User.Password()
			require.Equal(t, tc.password, password)
		})
	}
	for _, uri := range []string{
		"", "http://localhost", "couchdb:///db", "couchdb://host/database",
		"couchdb://host?ssl=true", "couchdb://host#fragment", "couchdb://host:invalid", "couchdb://user:%zz@host",
	} {
		t.Run(uri, func(t *testing.T) {
			_, err := parseURI(uri)
			require.Error(t, err)
		})
	}
}
