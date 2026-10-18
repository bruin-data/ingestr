package pipeline

import (
	"context"
	"crypto/sha256"
	"fmt"
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
		canonicalCDCStateURI(p.config.SourceURI), canonicalCDCStateURI(p.config.DestURI),
		p.config.SourceTable, destTable, cdcSelectionIdentity(p.config),
		cdcTableNamingIdentity(p.config), p.config.StagingDataset,
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(parts, "\x00"))))
}
