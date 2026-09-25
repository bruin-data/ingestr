package pipeline

import (
	"strings"
	"testing"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/destination/duckdb"
	"github.com/bruin-data/ingestr/pkg/destination/hubspot"
	"github.com/bruin-data/ingestr/pkg/destination/salesforce"
	"github.com/bruin-data/ingestr/pkg/naming"
)

func TestApplyReverseETLNamingPinsDirect(t *testing.T) {
	tests := []struct {
		name      string
		dest      destination.Destination
		requested string
		want      string
	}{
		{"salesforce default auto", salesforce.NewSalesforceDestination(), string(naming.Auto), string(naming.Direct)},
		{"salesforce explicit snake_case", salesforce.NewSalesforceDestination(), string(naming.SnakeCase), string(naming.Direct)},
		{"salesforce explicit direct", salesforce.NewSalesforceDestination(), string(naming.Direct), string(naming.Direct)},
		// HubSpot's lowercase property names rely on snake_case folding EMAIL to email.
		{"hubspot auto untouched", hubspot.NewHubSpotDestination(), string(naming.Auto), string(naming.Auto)},
		// A warehouse destination creates its own columns, so normalization stands.
		{"warehouse untouched", duckdb.NewDuckDBDestination(), string(naming.SnakeCase), string(naming.SnakeCase)},
		{"warehouse auto untouched", duckdb.NewDuckDBDestination(), string(naming.Auto), string(naming.Auto)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.IngestConfig{SchemaNaming: tt.requested}
			if err := applyReverseETLNaming(tt.dest, cfg); err != nil {
				t.Fatalf("applyReverseETLNaming returned error: %v", err)
			}
			if cfg.SchemaNaming != tt.want {
				t.Fatalf("SchemaNaming = %q, want %q", cfg.SchemaNaming, tt.want)
			}
		})
	}
}

func TestApplyReverseETLNamingRejectsUnknownConvention(t *testing.T) {
	cfg := &config.IngestConfig{SchemaNaming: "bogus"}
	err := applyReverseETLNaming(salesforce.NewSalesforceDestination(), cfg)
	if err == nil || !strings.Contains(err.Error(), "unknown naming convention: bogus") {
		t.Fatalf("error = %v, want the unknown convention rejected", err)
	}
}
