package athena

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

func parseAthenaConfig(rawURI string) (athenaConfig, error) {
	parsed, err := url.Parse(rawURI)
	if err != nil {
		return athenaConfig{}, fmt.Errorf("athena uri: failed to parse uri: %w", err)
	}

	q := parsed.Query()
	cfg := athenaConfig{
		Region:          q.Get("region_name"),
		Workgroup:       q.Get("workgroup"),
		Profile:         q.Get("profile"),
		AccessKeyID:     q.Get("access_key_id"),
		SecretAccessKey: q.Get("secret_access_key"),
		SessionToken:    q.Get("session_token"),
		DefaultDatabase: strings.TrimPrefix(parsed.Path, "/"),
	}

	bucket := strings.TrimSpace(q.Get("bucket"))
	if bucket == "" {
		return athenaConfig{}, errors.New("athena uri: bucket is required")
	}

	dataRoot, err := normalizeS3Location(bucket)
	if err != nil {
		return athenaConfig{}, err
	}
	cfg.DataRoot = dataRoot
	cfg.OutputLocation = dataRoot + "_athena_results/"

	if cfg.Profile == "" && cfg.AccessKeyID == "" && cfg.SecretAccessKey == "" && cfg.SessionToken == "" {
		return athenaConfig{}, errors.New("athena uri: provide either access_key_id/secret_access_key (optional session_token) or profile")
	}
	if (cfg.AccessKeyID == "") != (cfg.SecretAccessKey == "") {
		return athenaConfig{}, errors.New("athena uri: both access_key_id and secret_access_key are required when using static credentials")
	}

	return cfg, nil
}

func normalizeS3Location(bucketOrS3URI string) (string, error) {
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
