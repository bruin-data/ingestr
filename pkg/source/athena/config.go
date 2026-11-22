package athena

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

func parseAthenaConfig(rawURI string) (athenaConfig, error) {
	parsed, err := url.Parse(rawURI)
	if err != nil {
		return athenaConfig{}, fmt.Errorf("athena uri: failed to parse uri: %w", err)
	}

	cfg := athenaConfig{
		OutputLocation:  "",
		Region:          parsed.Query().Get("region_name"),
		Workgroup:       parsed.Query().Get("workgroup"),
		Profile:         parsed.Query().Get("profile"),
		AccessKeyID:     parsed.Query().Get("access_key_id"),
		SecretAccessKey: parsed.Query().Get("secret_access_key"),
		SessionToken:    parsed.Query().Get("session_token"),
		EndpointURL:     strings.TrimSpace(parsed.Query().Get("endpoint_url")),
		DefaultDatabase: strings.TrimPrefix(parsed.Path, "/"),
	}

	bucket := strings.TrimSpace(parsed.Query().Get("bucket"))
	if bucket == "" {
		return athenaConfig{}, errors.New("athena uri: bucket is required")
	}
	outputLocation, err := normalizeS3OutputLocation(bucket)
	if err != nil {
		return athenaConfig{}, err
	}
	cfg.OutputLocation = outputLocation

	if cfg.Profile == "" && cfg.AccessKeyID == "" && cfg.SecretAccessKey == "" && cfg.SessionToken == "" {
		return athenaConfig{}, errors.New("athena uri: provide either access_key_id/secret_access_key (optional session_token) or profile")
	}
	if (cfg.AccessKeyID == "") != (cfg.SecretAccessKey == "") {
		return athenaConfig{}, errors.New("athena uri: both access_key_id and secret_access_key are required when using static credentials")
	}

	if cfg.EndpointURL != "" {
		u, err := url.Parse(cfg.EndpointURL)
		if err != nil || u.Host == "" {
			return athenaConfig{}, errors.New("athena uri: endpoint_url must be a URL like https://host[:port]")
		}
		loopback := u.Hostname() == "localhost" || net.ParseIP(u.Hostname()).IsLoopback()
		if u.Scheme != "https" && (u.Scheme != "http" || !loopback) {
			return athenaConfig{}, errors.New("athena uri: endpoint_url must use https:// (http:// is only allowed for localhost)")
		}
	}

	return cfg, nil
}

func normalizeS3OutputLocation(bucketOrS3URI string) (string, error) {
	s := strings.TrimSpace(bucketOrS3URI)
	if s == "" {
		return "", errors.New("athena uri: bucket is required")
	}

	s = strings.TrimPrefix(s, "s3://")

	s = strings.TrimLeft(s, "/")
	if s == "" {
		return "", fmt.Errorf("athena uri: invalid bucket value %q", bucketOrS3URI)
	}

	out := "s3://" + s
	if !strings.HasSuffix(out, "/") {
		out += "/"
	}
	return out, nil
}
