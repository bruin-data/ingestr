package kafka

import (
	"context"
	"fmt"

	"github.com/aws/aws-msk-iam-sasl-signer-go/signer"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/segmentio/kafka-go/sasl"
)

type oauthBearerMechanism struct {
	generate func(context.Context) (string, error)
}

func newOAuthBearerMechanism(cfg kafkaAWSConfig) (sasl.Mechanism, error) {
	if cfg.Region == "" {
		return nil, fmt.Errorf("aws_region is required for OAUTHBEARER")
	}
	return oauthBearerMechanism{
		generate: func(ctx context.Context) (string, error) {
			return generateMSKIAMAuthToken(ctx, cfg)
		},
	}, nil
}

func (oauthBearerMechanism) Name() string {
	return "OAUTHBEARER"
}

func (m oauthBearerMechanism) Start(ctx context.Context) (sasl.StateMachine, []byte, error) {
	token, err := m.generate(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate MSK IAM auth token: %w", err)
	}
	return oauthBearerState{}, []byte("n,,\x01auth=Bearer " + token + "\x01\x01"), nil
}

type oauthBearerState struct{}

func (oauthBearerState) Next(context.Context, []byte) (bool, []byte, error) {
	return true, nil, nil
}

func generateMSKIAMAuthToken(ctx context.Context, cfg kafkaAWSConfig) (string, error) {
	var (
		token string
		err   error
	)

	switch {
	case cfg.RoleARN != "":
		token, _, err = signer.GenerateAuthTokenFromRole(ctx, cfg.Region, cfg.RoleARN, cfg.RoleSessionName)
	case cfg.Profile != "":
		token, _, err = signer.GenerateAuthTokenFromProfile(ctx, cfg.Region, cfg.Profile)
	case cfg.AccessKeyID != "":
		provider := credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, cfg.SessionToken)
		token, _, err = signer.GenerateAuthTokenFromCredentialsProvider(ctx, cfg.Region, provider)
	default:
		token, _, err = signer.GenerateAuthToken(ctx, cfg.Region)
	}
	if err != nil {
		return "", err
	}
	return token, nil
}
