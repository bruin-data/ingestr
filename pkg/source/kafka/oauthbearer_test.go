package kafka

import (
	"context"
	"errors"
	"testing"
)

func TestOAuthBearerMechanism_Name(t *testing.T) {
	m := oauthBearerMechanism{}
	if m.Name() != "OAUTHBEARER" {
		t.Errorf("Name() = %q, want OAUTHBEARER", m.Name())
	}
}

func TestOAuthBearerMechanism_StartWireFormat(t *testing.T) {
	m := oauthBearerMechanism{
		provider: func(_ context.Context) (string, error) { return "FAKE_TOKEN", nil },
	}

	sess, ir, err := m.Start(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess == nil {
		t.Fatal("session should not be nil")
	}
	want := "n,,\x01auth=Bearer FAKE_TOKEN\x01\x01"
	if string(ir) != want {
		t.Errorf("initial response = %q, want %q", string(ir), want)
	}
}

func TestOAuthBearerMechanism_StartProviderError(t *testing.T) {
	sentinel := errors.New("boom")
	m := oauthBearerMechanism{
		provider: func(_ context.Context) (string, error) { return "", sentinel },
	}

	sess, ir, err := m.Start(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, want it to wrap %v", err, sentinel)
	}
	if sess != nil || ir != nil {
		t.Error("session and initial response should be nil on error")
	}
}

func TestOAuthBearerSession_Next(t *testing.T) {
	done, resp, err := oauthBearerSession{}.Next(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !done {
		t.Error("Next should report done")
	}
	if resp != nil {
		t.Errorf("response = %q, want nil", string(resp))
	}
}

func TestNewOAuthBearerTokenProvider_RequiresRegion(t *testing.T) {
	if _, err := newOAuthBearerTokenProvider(kafkaConfig{}); err == nil {
		t.Fatal("expected error when aws_region is empty")
	}
}

func TestNewOAuthBearerTokenProvider_Selection(t *testing.T) {
	tests := []struct {
		name string
		cfg  kafkaConfig
	}{
		{
			name: "role arn",
			cfg:  kafkaConfig{AWSRegion: "us-east-1", AWSRoleArn: "arn:aws:iam::123:role/msk"},
		},
		{
			name: "profile",
			cfg:  kafkaConfig{AWSRegion: "us-east-1", AWSProfile: "default"},
		},
		{
			name: "static credentials",
			cfg:  kafkaConfig{AWSRegion: "us-east-1", AWSAccessKeyID: "AKID", AWSSecretAccessKey: "SECRET"},
		},
		{
			name: "default credential chain",
			cfg:  kafkaConfig{AWSRegion: "us-east-1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Only assert selection succeeds; do not invoke the provider so no
			// AWS calls are made.
			provider, err := newOAuthBearerTokenProvider(tt.cfg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if provider == nil {
				t.Fatal("provider should not be nil")
			}
		})
	}
}

func TestNewOAuthBearerTokenProvider_PartialStaticCredentials(t *testing.T) {
	cases := []kafkaConfig{
		{AWSRegion: "us-east-1", AWSAccessKeyID: "AKID"},
		{AWSRegion: "us-east-1", AWSSecretAccessKey: "SECRET"},
	}
	for _, cfg := range cases {
		if _, err := newOAuthBearerTokenProvider(cfg); err == nil {
			t.Errorf("expected error for partial static credentials: %+v", cfg)
		}
	}
}
