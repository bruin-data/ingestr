package uri

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
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

	normalizedURI := rawURI
	if strings.Contains(scheme, "+") {
		parts := strings.SplitN(rawURI, "://", 2)
		if len(parts) == 2 {
			baseScheme := strings.Split(scheme, "+")[0]
			normalizedURI = baseScheme + "://" + parts[1]
		}
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
