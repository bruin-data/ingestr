package kafka

import (
	"context"
	"fmt"

	"github.com/aws/aws-msk-iam-sasl-signer-go/signer"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/segmentio/kafka-go/sasl"
)

// tokenProvider returns a fresh OAUTHBEARER token. It is invoked on every SASL
// handshake, allowing short-lived tokens (e.g. AWS MSK IAM presigned URLs) to be
// regenerated per connection.
type tokenProvider func(ctx context.Context) (string, error)

// oauthBearerMechanism implements sasl.Mechanism for the OAUTHBEARER mechanism
// (RFC 7628). It is stateless and safe for concurrent use: Start generates a new
// token for each connection via its provider.
type oauthBearerMechanism struct {
	provider tokenProvider
}

func (oauthBearerMechanism) Name() string {
	return "OAUTHBEARER"
}

func (m oauthBearerMechanism) Start(ctx context.Context) (sasl.StateMachine, []byte, error) {
	token, err := m.provider(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate OAUTHBEARER token: %w", err)
	}
	ir := []byte(fmt.Sprintf("n,,\x01auth=Bearer %s\x01\x01", token))
	return oauthBearerSession{}, ir, nil
}

type oauthBearerSession struct{}

func (oauthBearerSession) Next(_ context.Context, challenge []byte) (bool, []byte, error) {
	if len(challenge) > 0 {
		return false, nil, fmt.Errorf("unexpected OAUTHBEARER broker challenge: %s", string(challenge))
	}

	// The broker rejects an invalid token by failing the SASL exchange before
	// Next is reached, so arriving here means authentication succeeded. This
	// mirrors kafka-go's built-in plain.Mechanism.
	return true, nil, nil
}

// newOAuthBearerTokenProvider selects an AWS MSK IAM token generator based on the
// supplied configuration. Region is always required. Credential resolution order:
// explicit role ARN, named profile, static credentials, then the default AWS
// credential chain (environment variables, EC2/ECS/EKS instance role, etc.).
func newOAuthBearerTokenProvider(cfg kafkaConfig) (tokenProvider, error) {
	region := cfg.AWSRegion
	if region == "" {
		return nil, fmt.Errorf("kafka OAUTHBEARER (MSK IAM) requires aws_region")
	}

	switch {
	case cfg.AWSRoleArn != "":
		return func(ctx context.Context) (string, error) {
			token, _, err := signer.GenerateAuthTokenFromRole(ctx, region, cfg.AWSRoleArn, cfg.AWSRoleSessionName)
			return token, err
		}, nil

	case cfg.AWSProfile != "":
		return func(ctx context.Context) (string, error) {
			token, _, err := signer.GenerateAuthTokenFromProfile(ctx, region, cfg.AWSProfile)
			return token, err
		}, nil

	case cfg.AWSAccessKeyID != "" || cfg.AWSSecretAccessKey != "" || cfg.AWSSessionToken != "":
		if cfg.AWSAccessKeyID == "" || cfg.AWSSecretAccessKey == "" {
			return nil, fmt.Errorf("kafka OAUTHBEARER: both aws_access_key_id and aws_secret_access_key are required for static credentials")
		}
		provider := credentials.NewStaticCredentialsProvider(cfg.AWSAccessKeyID, cfg.AWSSecretAccessKey, cfg.AWSSessionToken)
		return func(ctx context.Context) (string, error) {
			token, _, err := signer.GenerateAuthTokenFromCredentialsProvider(ctx, region, provider)
			return token, err
		}, nil

	default:
		return func(ctx context.Context) (string, error) {
			token, _, err := signer.GenerateAuthToken(ctx, region)
			return token, err
		}, nil
	}
}
