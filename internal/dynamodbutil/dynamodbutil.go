package dynamodbutil

import (
	"context"
	"fmt"
	"net/url"
	"regexp"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

var awsEndpointPattern = regexp.MustCompile(`.*\.(.+)\.amazonaws\.com`)

type Config struct {
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	EndpointURL     string
}

func ParseURI(uri string) (*Config, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("failed to parse dynamodb URI: %w", err)
	}

	query := u.Query()

	cfg := &Config{
		AccessKeyID:     query.Get("access_key_id"),
		SecretAccessKey: query.Get("secret_access_key"),
	}

	if matches := awsEndpointPattern.FindStringSubmatch(u.Host); matches != nil {
		cfg.Region = matches[1]
		cfg.EndpointURL = fmt.Sprintf("https://%s", u.Hostname())
	} else if u.Host != "" {
		cfg.EndpointURL = fmt.Sprintf("http://%s", u.Host)
	}

	if cfg.Region == "" {
		cfg.Region = query.Get("region")
	}

	if cfg.Region == "" {
		return nil, fmt.Errorf("region is required to connect to DynamoDB")
	}
	if cfg.AccessKeyID == "" {
		return nil, fmt.Errorf("access_key_id is required to connect to DynamoDB")
	}
	if cfg.SecretAccessKey == "" {
		return nil, fmt.Errorf("secret_access_key is required to connect to DynamoDB")
	}

	return cfg, nil
}

func NewClient(ctx context.Context, cfg *Config) (*dynamodb.Client, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(
		ctx,
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKeyID, cfg.SecretAccessKey, "",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := dynamodb.NewFromConfig(awsCfg, func(o *dynamodb.Options) {
		if cfg.EndpointURL != "" {
			o.BaseEndpoint = aws.String(cfg.EndpointURL)
		}
	})

	return client, nil
}
