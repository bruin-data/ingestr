package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	sfauth "github.com/bruin-data/ingestr/pkg/snowflake"
	_ "github.com/snowflakedb/gosnowflake"
)

func main() {
	mode := flag.String("mode", "scalar", "execution mode: exec, scalar, or list")
	timeout := flag.Duration("timeout", 2*time.Minute, "query timeout")
	uriEnv := flag.String("uri-env", "", "environment variable containing the Snowflake URI")
	flag.Parse()

	var uri string
	var query string
	if *uriEnv != "" {
		if flag.NArg() != 1 {
			fmt.Fprintln(os.Stderr, "usage: snowflake_sql [-mode exec|scalar|list] -uri-env <env> <sql>")
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
			fmt.Fprintln(os.Stderr, "usage: snowflake_sql [-mode exec|scalar|list] <snowflake-uri> <sql>")
			os.Exit(2)
		}
		uri = flag.Arg(0)
		query = flag.Arg(1)
	}

	if query == "" {
		fmt.Fprintln(os.Stderr, "sql query is required")
		os.Exit(2)
	}

	db, err := sfauth.OpenDB(uri)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open Snowflake connection: %v\n", err)
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
