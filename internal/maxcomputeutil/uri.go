package maxcomputeutil

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aliyun/aliyun-odps-go-sdk/odps"
)

type Options struct {
	Endpoint       string
	Project        string
	Schema         string
	EmulatorDBPath string
	StorageAPI     bool
}

func ParseURI(rawURI string) (*odps.Config, Options, error) {
	u, err := url.Parse(rawURI)
	if err != nil {
		return nil, Options{}, err
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "maxcompute" && scheme != "odps" {
		return nil, Options{}, fmt.Errorf("unsupported MaxCompute scheme: %s", u.Scheme)
	}

	query := u.Query()

	endpoint := query.Get("endpoint")
	if endpoint == "" {
		protocol := query.Get("protocol")
		if protocol == "" {
			protocol = "https"
			if isLocalHost(u.Hostname()) {
				protocol = "http"
			}
		}
		if u.Host == "" {
			return nil, Options{}, fmt.Errorf("maxcompute URI requires host or endpoint parameter")
		}
		endpoint = fmt.Sprintf("%s://%s", protocol, u.Host)
	}
	if _, err := url.Parse(endpoint); err != nil {
		return nil, Options{}, fmt.Errorf("invalid MaxCompute endpoint %q: %w", endpoint, err)
	}

	project := firstNonEmpty(query.Get("project"), query.Get("project_name"), strings.Trim(strings.TrimPrefix(u.Path, "/"), "/"))
	if project == "" {
		return nil, Options{}, fmt.Errorf("maxcompute URI requires project parameter")
	}

	accessID := firstNonEmpty(query.Get("access_id"), query.Get("accessId"), query.Get("access_key_id"))
	accessKey := firstNonEmpty(query.Get("access_key"), query.Get("accessKey"), query.Get("access_key_secret"))
	if u.User != nil {
		accessID = firstNonEmpty(u.User.Username(), accessID)
		if password, ok := u.User.Password(); ok {
			accessKey = firstNonEmpty(password, accessKey)
		}
	}
	accessID = firstNonEmpty(accessID, os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_ID"), os.Getenv("ODPS_ACCESS_ID"))
	accessKey = firstNonEmpty(accessKey, os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET"), os.Getenv("ODPS_ACCESS_KEY"))
	if accessID == "" || accessKey == "" {
		return nil, Options{}, fmt.Errorf("maxcompute URI requires access id and access key")
	}

	cfg := odps.NewConfig()
	cfg.AccessId = accessID
	cfg.AccessKey = accessKey
	cfg.Endpoint = endpoint
	cfg.ProjectName = project
	cfg.StsToken = query.Get("sts_token")
	cfg.TunnelEndpoint = firstNonEmpty(query.Get("tunnel_endpoint"), query.Get("tunnelEndpoint"))
	cfg.TunnelQuotaName = firstNonEmpty(query.Get("tunnel_quota_name"), query.Get("tunnelQuotaName"))
	cfg.Hints = parseHints(query)
	cfg.Others = parseOthers(query)

	if timeout := firstNonEmpty(query.Get("tcp_connection_timeout"), query.Get("tcpConnectionTimeout")); timeout != "" {
		if seconds, err := strconv.Atoi(timeout); err == nil {
			cfg.TcpConnectionTimeout = time.Duration(seconds) * time.Second
		}
	}
	if timeout := firstNonEmpty(query.Get("http_timeout"), query.Get("httpTimeout")); timeout != "" {
		if seconds, err := strconv.Atoi(timeout); err == nil {
			cfg.HttpTimeout = time.Duration(seconds) * time.Second
		}
	}

	opts := Options{
		Endpoint:       endpoint,
		Project:        project,
		Schema:         query.Get("schema"),
		EmulatorDBPath: query.Get("emulator_db_path"),
	}
	opts.StorageAPI = parseBool(query.Get("storage_api")) || opts.EmulatorDBPath != ""

	return cfg, opts, nil
}

func parseHints(query url.Values) map[string]string {
	hints := map[string]string{}
	for key, values := range query {
		if len(values) == 0 {
			continue
		}
		if strings.HasPrefix(key, "hints.") {
			hints[strings.TrimPrefix(key, "hints.")] = values[0]
		}
	}
	if len(hints) == 0 {
		return nil
	}
	return hints
}

func parseOthers(query url.Values) map[string]string {
	others := map[string]string{}
	for _, key := range []string{"enableLogview"} {
		if value := query.Get(key); value != "" {
			others[key] = value
		}
	}
	if len(others) == 0 {
		return nil
	}
	return others
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func isLocalHost(host string) bool {
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
