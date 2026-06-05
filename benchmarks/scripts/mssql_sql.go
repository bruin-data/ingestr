//go:build ignore

package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	_ "github.com/microsoft/go-mssqldb"
)

func main() {
	mode := flag.String("mode", "scalar", "execution mode: exec, scalar, or list")
	timeout := flag.Duration("timeout", 2*time.Minute, "query timeout")
	uriEnv := flag.String("uri-env", "", "environment variable containing the SQL Server URI")
	flag.Parse()

	var uri string
	var query string
	if *uriEnv != "" {
		if flag.NArg() != 1 {
			fmt.Fprintln(os.Stderr, "usage: mssql_sql [-mode exec|scalar|list] -uri-env <env> <sql>")
			os.Exit(2)
		}
		uri = os.Getenv(*uriEnv)
		if uri == "" {
			fmt.Fprintf(os.Stderr, "environment variable %s is not set\n", *uriEnv)
			os.Exit(2)
		}
		query = flag.Arg(0)
	} else {
		if flag.NArg() != 2 {
			fmt.Fprintln(os.Stderr, "usage: mssql_sql [-mode exec|scalar|list] <mssql-uri> <sql>")
			os.Exit(2)
		}
		uri = flag.Arg(0)
		query = flag.Arg(1)
	}

	if query == "" {
		fmt.Fprintln(os.Stderr, "sql query is required")
		os.Exit(2)
	}

	connStr, err := uriToConnString(uri)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse SQL Server URI: %v\n", err)
		os.Exit(1)
	}

	db, err := sql.Open("sqlserver", connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open SQL Server connection: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	switch *mode {
	case "exec":
		err = execStatements(ctx, db, query)
	case "scalar":
		err = printScalar(ctx, db, query)
	case "list":
		err = printList(ctx, db, query)
	default:
		err = fmt.Errorf("unknown mode %q", *mode)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func uriToConnString(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", err
	}

	scheme := strings.ToLower(u.Scheme)
	if !strings.HasPrefix(scheme, "mssql") && scheme != "sqlserver" {
		return "", fmt.Errorf("unsupported scheme: %s", scheme)
	}

	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "1433"
	}

	connURL := &url.URL{
		Scheme: "sqlserver",
		Host:   fmt.Sprintf("%s:%s", host, port),
	}

	if u.User != nil {
		user := u.User.Username()
		password, hasPassword := u.User.Password()
		if hasPassword {
			connURL.User = url.UserPassword(user, password)
		} else {
			connURL.User = url.User(user)
		}
	}

	query := u.Query()
	query.Del("driver")
	if database := strings.TrimPrefix(u.Path, "/"); database != "" {
		query.Set("database", database)
	}
	connURL.RawQuery = query.Encode()

	return connURL.String(), nil
}

func execStatements(ctx context.Context, db *sql.DB, query string) error {
	for _, stmt := range strings.Split(query, ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("failed to execute %q: %w", stmt, err)
		}
	}
	return nil
}

func printScalar(ctx context.Context, db *sql.DB, query string) error {
	var value any
	if err := db.QueryRowContext(ctx, query).Scan(&value); err != nil {
		return fmt.Errorf("failed to query scalar: %w", err)
	}
	fmt.Println(formatValue(value))
	return nil
}

func printList(ctx context.Context, db *sql.DB, query string) error {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	cols, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("failed to inspect columns: %w", err)
	}
	values := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range values {
		ptrs[i] = &values[i]
	}

	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}
		formatted := make([]string, len(values))
		for i, value := range values {
			formatted[i] = formatValue(value)
		}
		fmt.Println(strings.Join(formatted, "\t"))
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed while reading rows: %w", err)
	}
	return nil
}

func formatValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case []byte:
		return string(v)
	case time.Time:
		return v.Format(time.RFC3339Nano)
	default:
		return fmt.Sprint(v)
	}
}
