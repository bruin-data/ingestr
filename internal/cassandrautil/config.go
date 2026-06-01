package cassandrautil

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

const (
	DefaultPort              = 9042
	DefaultPageSize          = 5000
	DefaultReplicationFactor = 1
)

type Config struct {
	Hosts                    []string
	Port                     int
	Keyspace                 string
	Username                 string
	Password                 string
	Consistency              gocql.Consistency
	PageSize                 int
	Timeout                  time.Duration
	ConnectTimeout           time.Duration
	ProtoVersion             int
	DisableInitialHostLookup bool
	SSLEnabled               bool
	SSLCAPath                string
	SSLCertPath              string
	SSLKeyPath               string
	SSLHostVerification      bool
	ReplicationFactor        int
}

func ParseURI(rawURI string) (*Config, error) {
	u, err := url.Parse(rawURI)
	if err != nil {
		return nil, fmt.Errorf("invalid Cassandra URI: %w", err)
	}
	if strings.ToLower(u.Scheme) != "cassandra" {
		return nil, fmt.Errorf("unsupported Cassandra URI scheme: %s", u.Scheme)
	}

	cfg := &Config{
		Port:                DefaultPort,
		Consistency:         gocql.Quorum,
		PageSize:            DefaultPageSize,
		SSLHostVerification: true,
		ReplicationFactor:   DefaultReplicationFactor,
	}

	if u.User != nil {
		cfg.Username = u.User.Username()
		cfg.Password, _ = u.User.Password()
	}

	hostList := u.Host
	if hostsParam := strings.TrimSpace(u.Query().Get("hosts")); hostsParam != "" {
		hostList = hostsParam
	}
	hosts, port, err := parseHosts(hostList, cfg.Port)
	if err != nil {
		return nil, err
	}
	cfg.Hosts = hosts
	cfg.Port = port

	cfg.Keyspace = strings.Trim(strings.TrimPrefix(u.Path, "/"), "/")
	if keyspace := strings.TrimSpace(u.Query().Get("keyspace")); keyspace != "" {
		cfg.Keyspace = keyspace
	}
	if cfg.Keyspace != "" {
		cfg.Keyspace = normalizeIdentifierSegment(cfg.Keyspace)
	}

	q := u.Query()
	if raw := strings.TrimSpace(q.Get("consistency")); raw != "" {
		consistency, err := parseConsistency(raw)
		if err != nil {
			return nil, err
		}
		cfg.Consistency = consistency
	}
	if raw := strings.TrimSpace(q.Get("page_size")); raw != "" {
		cfg.PageSize, err = parsePositiveInt("page_size", raw)
		if err != nil {
			return nil, err
		}
	}
	if raw := strings.TrimSpace(q.Get("timeout")); raw != "" {
		cfg.Timeout, err = time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid timeout %q: %w", raw, err)
		}
	}
	if raw := strings.TrimSpace(q.Get("connect_timeout")); raw != "" {
		cfg.ConnectTimeout, err = time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid connect_timeout %q: %w", raw, err)
		}
	}
	if raw := strings.TrimSpace(q.Get("proto_version")); raw != "" {
		cfg.ProtoVersion, err = parsePositiveInt("proto_version", raw)
		if err != nil {
			return nil, err
		}
	}
	if raw := strings.TrimSpace(q.Get("replication_factor")); raw != "" {
		cfg.ReplicationFactor, err = parsePositiveInt("replication_factor", raw)
		if err != nil {
			return nil, err
		}
	}
	if raw := firstNonEmpty(q.Get("ssl"), q.Get("tls")); raw != "" {
		cfg.SSLEnabled, err = strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid ssl value %q: %w", raw, err)
		}
	}
	if raw := strings.TrimSpace(q.Get("disable_initial_host_lookup")); raw != "" {
		cfg.DisableInitialHostLookup, err = strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid disable_initial_host_lookup value %q: %w", raw, err)
		}
	}
	if raw := strings.TrimSpace(q.Get("ssl_enable_host_verification")); raw != "" {
		cfg.SSLHostVerification, err = strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid ssl_enable_host_verification value %q: %w", raw, err)
		}
	}
	cfg.SSLCAPath = q.Get("ssl_ca_path")
	cfg.SSLCertPath = q.Get("ssl_cert_path")
	cfg.SSLKeyPath = q.Get("ssl_key_path")

	return cfg, nil
}

func NewCluster(cfg *Config) *gocql.ClusterConfig {
	cluster := gocql.NewCluster(cfg.Hosts...)
	cluster.Port = cfg.Port
	cluster.Consistency = cfg.Consistency
	cluster.PageSize = cfg.PageSize
	cluster.DisableInitialHostLookup = cfg.DisableInitialHostLookup
	if cfg.Timeout > 0 {
		cluster.Timeout = cfg.Timeout
	}
	if cfg.ConnectTimeout > 0 {
		cluster.ConnectTimeout = cfg.ConnectTimeout
	}
	if cfg.ProtoVersion > 0 {
		cluster.ProtoVersion = cfg.ProtoVersion
	}
	if cfg.Username != "" {
		cluster.Authenticator = gocql.PasswordAuthenticator{
			Username: cfg.Username,
			Password: cfg.Password,
		}
	}
	if cfg.SSLEnabled {
		cluster.SslOpts = &gocql.SslOptions{
			CaPath:                 cfg.SSLCAPath,
			CertPath:               cfg.SSLCertPath,
			KeyPath:                cfg.SSLKeyPath,
			EnableHostVerification: cfg.SSLHostVerification,
		}
	}
	return cluster
}

func parseHosts(raw string, defaultPort int) ([]string, int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, 0, fmt.Errorf("Cassandra URI must include at least one host")
	}

	port := defaultPort
	var hosts []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		host, hostPort, err := splitHostPort(part)
		if err != nil {
			return nil, 0, err
		}
		if hostPort > 0 {
			if port != defaultPort && port != hostPort {
				return nil, 0, fmt.Errorf("all Cassandra hosts must use the same port, got %d and %d", port, hostPort)
			}
			port = hostPort
		}
		hosts = append(hosts, host)
	}
	if len(hosts) == 0 {
		return nil, 0, fmt.Errorf("Cassandra URI must include at least one host")
	}
	return hosts, port, nil
}

func splitHostPort(raw string) (string, int, error) {
	if strings.HasPrefix(raw, "[") {
		host, portStr, err := net.SplitHostPort(raw)
		if err != nil {
			return "", 0, fmt.Errorf("invalid Cassandra host %q: %w", raw, err)
		}
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return "", 0, fmt.Errorf("invalid Cassandra port %q: %w", portStr, err)
		}
		return host, port, nil
	}

	lastColon := strings.LastIndex(raw, ":")
	if lastColon > -1 && strings.Count(raw, ":") == 1 {
		host := strings.TrimSpace(raw[:lastColon])
		portStr := strings.TrimSpace(raw[lastColon+1:])
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return "", 0, fmt.Errorf("invalid Cassandra port %q: %w", portStr, err)
		}
		if host == "" {
			return "", 0, fmt.Errorf("invalid Cassandra host %q", raw)
		}
		return host, port, nil
	}

	return strings.Trim(raw, "[]"), 0, nil
}

func parseConsistency(raw string) (gocql.Consistency, error) {
	switch strings.ToLower(strings.ReplaceAll(raw, "-", "_")) {
	case "any":
		return gocql.Any, nil
	case "one":
		return gocql.One, nil
	case "two":
		return gocql.Two, nil
	case "three":
		return gocql.Three, nil
	case "quorum":
		return gocql.Quorum, nil
	case "all":
		return gocql.All, nil
	case "local_quorum":
		return gocql.LocalQuorum, nil
	case "each_quorum":
		return gocql.EachQuorum, nil
	case "serial":
		return gocql.Serial, nil
	case "local_serial":
		return gocql.LocalSerial, nil
	case "local_one":
		return gocql.LocalOne, nil
	default:
		return 0, fmt.Errorf("unsupported Cassandra consistency: %s", raw)
	}
}

func parsePositiveInt(name, raw string) (int, error) {
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", name, raw, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be greater than 0", name)
	}
	return value, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
