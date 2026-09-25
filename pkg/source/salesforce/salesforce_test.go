package salesforce

import (
	"context"
	"testing"
)

func TestConnectWithAccessTokenAuth(t *testing.T) {
	src := NewSalesforceSource()

	if err := src.Connect(context.Background(), "salesforce://?access_token=access-token&domain=https://company.my.salesforce.com"); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	defer func() { _ = src.Close(context.Background()) }()

	if got := src.sessionID; got != "access-token" {
		t.Fatalf("sessionID = %q, want %q", got, "access-token")
	}
	if got := src.instanceURL; got != "https://company.my.salesforce.com" {
		t.Fatalf("instanceURL = %q, want %q", got, "https://company.my.salesforce.com")
	}
}
