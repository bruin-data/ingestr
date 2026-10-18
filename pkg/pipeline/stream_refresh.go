package pipeline

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/url"
	"strings"

	"github.com/bruin-data/ingestr/pkg/destination"
	"github.com/bruin-data/ingestr/pkg/strategy"
)

func (p *Pipeline) prepareStreamingRefresh(ctx context.Context, manager *strategy.CDCStateManager) error {
	if !p.config.Stream || !p.config.FullRefresh {
		return nil
	}
	if _, ok := p.dest.(destination.TruncateCapable); !ok {
		return fmt.Errorf("--full-refresh --stream requires a destination supporting snapshot truncation")
	}
	if manager == nil {
		return fmt.Errorf("--full-refresh --stream requires destination-managed CDC state")
	}
	if p.config.RefreshID == "" {
		return nil
	}
	fresh, err := manager.PrepareRefresh(ctx, p.config.RefreshID, p.streamingRefreshScope())
	if err != nil {
		return err
	}
	p.config.FullRefresh = fresh
	return nil
}

func (p *Pipeline) streamingRefreshScope() string {
	destTable := p.config.DestTable
	if p.config.SourceTable == "" {
		destTable = ""
	}
	parts := []string{
		canonicalStreamingRefreshURI(p.config.SourceURI), canonicalStreamingRefreshURI(p.config.DestURI),
		p.config.SourceTable, destTable, cdcSelectionIdentity(p.config),
		cdcTableNamingIdentity(p.config), p.config.StagingDataset,
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(parts, "\x00"))))
}

func canonicalStreamingRefreshURI(rawURI string) string {
	canonical := canonicalCDCStateURI(rawURI)
	parsed, err := url.Parse(canonical)
	if err != nil || parsed.Scheme != "bigquery" {
		return canonical
	}
	// Credentials may rotate or move between workload starts without changing
	// the BigQuery project and dataset this refresh targets.
	query := parsed.Query()
	query.Del("credentials_path")
	query.Del("credentials_base64")
	parsed.RawQuery = query.Encode()
	return parsed.String()
}
