package uri

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/bruin-data/ingestr/pkg/mysqluri"
)

type ParsedURI struct {
	Scheme   string
	Username string
	Password string
	Host     string
	Port     int
	Database string
	Params   map[string]string
	RawURI   string
}

// fileBasedSchemes are schemes that use file paths instead of network URIs
var fileBasedSchemes = map[string]bool{
	"jsonl": true, "ndjson": true, "json": true,
	"csv": true, "parquet": true, "avro": true,
	"sqlite": true, "duckdb": true, "motherduck": true, "md": true, "mmap": true,
}

func Parse(rawURI string) (*ParsedURI, error) {
	scheme, err := ExtractScheme(rawURI)
	if err != nil {
		return nil, err
	}

	normalizedScheme := NormalizeScheme(scheme)

	// For file-based schemes, skip url.Parse to avoid Windows path issues
	if fileBasedSchemes[normalizedScheme] {
		return &ParsedURI{
			Scheme: normalizedScheme,
			RawURI: rawURI,
			Params: make(map[string]string),
		}, nil
	}

	// The scheme was already extracted above, so parse the rest under a
	// placeholder: url.Parse rejects scheme characters ingestr allows, such as
	// the underscore in ps_mysql.
	normalizedURI := rawURI
	if parts := strings.SplitN(rawURI, "://", 2); len(parts) == 2 {
		normalizedURI = "scheme://" + parts[1]
	}

	parsed, err := url.Parse(normalizedURI)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URI: %w", err)
	}

	result := &ParsedURI{
		Scheme: NormalizeScheme(scheme),
		RawURI: rawURI,
		Params: make(map[string]string),
	}

	if parsed.User != nil {
		result.Username = parsed.User.Username()
		result.Password, _ = parsed.User.Password()
	}

	result.Host = parsed.Hostname()
	if portStr := parsed.Port(); portStr != "" {
		result.Port, _ = strconv.Atoi(portStr)
	}

	result.Database = strings.TrimPrefix(parsed.Path, "/")

	for key, values := range parsed.Query() {
		if len(values) > 0 {
			result.Params[key] = values[0]
		}
	}

	return result, nil
}

func ExtractScheme(uri string) (string, error) {
	idx := strings.Index(uri, "://")
	if idx == -1 {
		return "", fmt.Errorf("invalid URI: no scheme found")
	}
	return strings.ToLower(uri[:idx]), nil
}

func NormalizeScheme(scheme string) string {
	aliases := map[string]string{
		"postgresql":          "postgres",
		"postgresql+psycopg2": "postgres",
		"postgresql+asyncpg":  "postgres",
		"pg":                  "postgres",
		"redshift+psycopg2":   "redshift",
		"azure-sql":           "azuresql",
	}
	if canonical, ok := aliases[scheme]; ok {
		return canonical
	}
	return scheme
}

// MaskURI redacts credentials and sensitive query parameters from a connector
// URI while preserving enough shape for logs and stats to identify endpoints.
func MaskURI(uri string) string {
	// mysqluri.ParseURL tolerates scheme characters url.Parse rejects (e.g. the
	// underscore in ps_mysql) so those URIs are masked instead of fully redacted.
	parsed, err := mysqluri.ParseURL(uri)
	if err != nil {
		return "<redacted-uri>"
	}
	if parsed.User != nil {
		username := parsed.User.Username()
		if _, hasPassword := parsed.User.Password(); hasPassword {
			parsed.User = url.UserPassword(username, "xxxxx")
		} else {
			parsed.User = url.User(username)
		}
	}

	query := parsed.Query()
	for key := range query {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "password") ||
			strings.Contains(lower, "pass") ||
			strings.Contains(lower, "credential") ||
			strings.Contains(lower, "secret") ||
			strings.Contains(lower, "token") ||
			strings.Contains(lower, "key") ||
			strings.Contains(lower, "private") ||
			strings.Contains(lower, "sas") {
			query.Set(key, "xxxxx")
		}
	}
	parsed.RawQuery = query.Encode()

	return parsed.String()
}

func (p *ParsedURI) ToConnectionString() string {
	var parts []string

	if p.Host != "" {
		parts = append(parts, fmt.Sprintf("host=%s", p.Host))
	}
	if p.Port > 0 {
		parts = append(parts, fmt.Sprintf("port=%d", p.Port))
	}
	if p.Database != "" {
		parts = append(parts, fmt.Sprintf("dbname=%s", p.Database))
	}
	if p.Username != "" {
		parts = append(parts, fmt.Sprintf("user=%s", p.Username))
	}
	if p.Password != "" {
		parts = append(parts, fmt.Sprintf("password=%s", p.Password))
	}

	for key, value := range p.Params {
		parts = append(parts, fmt.Sprintf("%s=%s", key, value))
	}

	return strings.Join(parts, " ")
}
