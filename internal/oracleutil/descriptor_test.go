package oracleutil

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"

	_ "github.com/sijms/go-ora/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testDescriptor = "(DESCRIPTION=(ADDRESS=(PROTOCOL=TCP)(HOST=db.example.com)(PORT=1521))(CONNECT_DATA=(SERVICE_NAME=ORCLPDB)))"

func TestLiftInlineDescriptor(t *testing.T) {
	escaped := url.QueryEscape(testDescriptor)
	tests := []struct {
		name    string
		uri     string
		want    string
		wantErr string
	}{
		{
			name: "descriptor with credentials",
			uri:  "oracle://user:pass@" + testDescriptor,
			want: "oracle://user:pass@/?dsn=" + escaped,
		},
		{
			name: "descriptor without credentials",
			uri:  "oracle://" + testDescriptor,
			want: "oracle:///?dsn=" + escaped,
		},
		{
			name: "descriptor followed by options",
			uri:  "oracle+cx_oracle://user:pass@" + testDescriptor + "?SSL=true",
			want: "oracle+cx_oracle://user:pass@/?dsn=" + escaped + "&SSL=true",
		},
		{
			name: "descriptor followed by slash and options",
			uri:  "exadata://user:pass@" + testDescriptor + "/?SSL=true",
			want: "exadata://user:pass@/?dsn=" + escaped + "&SSL=true",
		},
		{
			name: "lowercase descriptor list",
			uri:  "oracle://u:p@(description_list=(DESCRIPTION=(ADDRESS=(HOST=a)(PORT=1))))",
			want: "oracle://u:p@/?dsn=" + url.QueryEscape("(description_list=(DESCRIPTION=(ADDRESS=(HOST=a)(PORT=1))))"),
		},
		{
			name: "quoted parentheses do not end the descriptor",
			uri:  `oracle://u:p@(DESCRIPTION=(ADDRESS=(HOST=a)(PORT=1))(SECURITY=(SSL_SERVER_CERT_DN="CN=db (prod)")))`,
			want: "oracle://u:p@/?dsn=" + url.QueryEscape(`(DESCRIPTION=(ADDRESS=(HOST=a)(PORT=1))(SECURITY=(SSL_SERVER_CERT_DN="CN=db (prod)")))`),
		},
		{
			name: "slash inside the descriptor",
			uri:  "oracle://u:p@(DESCRIPTION=(ADDRESS=(PROTOCOL=TCPS)(HOST=a)(PORT=2484))(SECURITY=(MY_WALLET_DIRECTORY=/opt/wallet)))?SSL=true",
			want: "oracle://u:p@/?dsn=" + url.QueryEscape("(DESCRIPTION=(ADDRESS=(PROTOCOL=TCPS)(HOST=a)(PORT=2484))(SECURITY=(MY_WALLET_DIRECTORY=/opt/wallet)))") + "&SSL=true",
		},
		{
			name: "password containing a parenthesis is not a descriptor",
			uri:  "oracle://user:pa(ss@host:1521/ORCL",
			want: "oracle://user:pa(ss@host:1521/ORCL",
		},
		{
			name: "standard URI is unchanged",
			uri:  "oracle://user:pass@host:1521/ORCL?SSL=true",
			want: "oracle://user:pass@host:1521/ORCL?SSL=true",
		},
		{
			name: "descriptor-like text in the query is unchanged",
			uri:  "oracle://user:pass@host:1521/ORCL?note=@" + testDescriptor,
			want: "oracle://user:pass@host:1521/ORCL?note=@" + testDescriptor,
		},
		{
			name: "non-Oracle scheme is unchanged",
			uri:  "postgres://user:pass@" + testDescriptor,
			want: "postgres://user:pass@" + testDescriptor,
		},
		{
			name:    "unbalanced descriptor",
			uri:     "oracle://user:pass@(DESCRIPTION=(ADDRESS=(HOST=a)(PORT=1))",
			wantErr: "unbalanced parentheses",
		},
		{
			name:    "service name after descriptor",
			uri:     "oracle://user:pass@" + testDescriptor + "/ORCL",
			wantErr: "unexpected text after",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LiftInlineDescriptor(tt.uri)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				assert.NotContains(t, err.Error(), "pass", "error must not echo credentials")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDescriptorConnString(t *testing.T) {
	escaped := url.QueryEscape(testDescriptor)
	want := "oracle://user:p%40ss@:0/?connStr=" + escaped
	tests := []struct {
		name    string
		uri     string
		want    string
		wantOK  bool
		wantErr string
	}{
		{
			name:   "dsn parameter",
			uri:    "oracle://user:p%40ss@/?dsn=" + escaped,
			want:   want,
			wantOK: true,
		},
		{
			name:   "go-ora connStr alias",
			uri:    "oracle://user:p%40ss@?connStr=" + escaped,
			want:   want,
			wantOK: true,
		},
		{
			name:   "unencoded descriptor in query",
			uri:    "oracle://user:p%40ss@/?dsn=" + testDescriptor,
			want:   want,
			wantOK: true,
		},
		{
			name:   "driver options are kept",
			uri:    "oracle://user:p%40ss@/?dsn=" + escaped + "&SSL=true&TIMEOUT=30",
			want:   "oracle://user:p%40ss@:0/?SSL=true&TIMEOUT=30&connStr=" + escaped,
			wantOK: true,
		},
		{
			name: "no descriptor",
			uri:  "oracle://user:pass@host:1521/ORCL",
		},
		{
			name:    "TNS alias",
			uri:     "oracle://user:pass@/?dsn=ORCLPDB",
			wantErr: "TNS aliases",
		},
		{
			name:    "host and descriptor",
			uri:     "oracle://user:pass@host/?dsn=" + escaped,
			wantErr: "host or port",
		},
		{
			name:    "port and descriptor",
			uri:     "oracle://user:pass@:1521/?dsn=" + escaped,
			wantErr: "host or port",
		},
		{
			name:    "path and descriptor",
			uri:     "oracle://user:pass@/ORCL?dsn=" + escaped,
			wantErr: "URI path",
		},
		{
			name:    "service_name and descriptor",
			uri:     "oracle://user:pass@/?dsn=" + escaped + "&service_name=X",
			wantErr: "service_name",
		},
		{
			name:    "SID and descriptor",
			uri:     "oracle://user:pass@/?dsn=" + escaped + "&SID=X",
			wantErr: "SID",
		},
		{
			name:    "server and descriptor",
			uri:     "oracle://user:pass@/?dsn=" + escaped + "&server=other%3A1521",
			wantErr: "server",
		},
		{
			name:    "dsn and connStr together",
			uri:     "oracle://user:pass@/?dsn=" + escaped + "&connStr=" + escaped,
			wantErr: "once",
		},
		{
			name:    "trailing text after descriptor",
			uri:     "oracle://user:pass@/?dsn=" + url.QueryEscape(testDescriptor+"(X=1)"),
			wantErr: "unexpected text after",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(tt.uri)
			require.NoError(t, err)
			got, ok, err := DescriptorConnString(u)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

// The driver must dial the listener named in the descriptor and send the
// descriptor itself as the connect data.
func TestDescriptorReachesListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()

	received := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			received <- ""
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		var buf strings.Builder
		_, _ = io.Copy(&buf, conn)
		received <- buf.String()
	}()

	port := listener.Addr().(*net.TCPAddr).Port
	descriptor := fmt.Sprintf("(DESCRIPTION=\n  (ADDRESS=(PROTOCOL=TCP)(HOST=127.0.0.1)(PORT=%d))\n  (CONNECT_DATA=(SERVICE_NAME=ORCLPDB))\n)", port)
	lifted, err := LiftInlineDescriptor("oracle://user:pass@" + descriptor)
	require.NoError(t, err)
	u, err := url.Parse(lifted)
	require.NoError(t, err)
	connStr, ok, err := DescriptorConnString(u)
	require.NoError(t, err)
	require.True(t, ok)

	db, err := sql.Open("oracle", connStr)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.Error(t, db.PingContext(ctx))

	select {
	case packet := <-received:
		assert.Contains(t, packet, fmt.Sprintf("(ADDRESS=(PROTOCOL=TCP)(HOST=127.0.0.1)(PORT=%d))", port))
		assert.Contains(t, packet, "(CONNECT_DATA=(SERVICE_NAME=ORCLPDB))")
	case <-time.After(5 * time.Second):
		t.Fatal("driver never connected to the listener from the descriptor")
	}
}
