package bigquery

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/bruin-data/ingestr/pkg/source/adbc"
)

// ReadAPI selects how the BigQuery source reads table data.
type ReadAPI string

const (
	// ReadAPIStorage reads through the BigQuery Storage Read API, falling back
	// to the ADBC driver. This is the default.
	ReadAPIStorage ReadAPI = "storage"
	// ReadAPIREST reads only through the BigQuery REST API (tabledata.list and
	// jobs.query/getQueryResults) and never touches the Storage Read API.
	ReadAPIREST ReadAPI = "rest"
)

// ParseReadAPI parses the read_api URI parameter. An empty value selects the
// default Storage Read API.
func ParseReadAPI(value string) (ReadAPI, error) {
	switch ReadAPI(value) {
	case "", ReadAPIStorage:
		return ReadAPIStorage, nil
	case ReadAPIREST:
		return ReadAPIREST, nil
	default:
		return "", fmt.Errorf("invalid BigQuery read_api %q: must be %q or %q", value, ReadAPIStorage, ReadAPIREST)
	}
}

func readAPIFromURI(uri string) (ReadAPI, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("invalid BigQuery URI: %w", err)
	}
	return ParseReadAPI(u.Query().Get("read_api"))
}

// Source dispatches to the ADBC/Storage Read API source by default and to the
// REST-only source when the URI sets read_api=rest.
type Source struct {
	active source.Source
}

func NewSource() *Source {
	return &Source{}
}

func (s *Source) Schemes() []string {
	return []string{"bigquery"}
}

func (s *Source) Connect(ctx context.Context, uri string) error {
	readAPI, err := readAPIFromURI(uri)
	if err != nil {
		return err
	}

	if readAPI == ReadAPIREST {
		config.Debug("[BIGQUERY] Using REST API for reads (read_api=rest); Storage Read API is disabled")
		s.active = newRESTSource(NewDialect())
	} else {
		config.Debug("[BIGQUERY] Using Storage Read API for reads (read_api=storage)")
		s.active = adbc.NewADBCSource(NewDialect())
	}
	return s.active.Connect(ctx, uri)
}

func (s *Source) Close(ctx context.Context) error {
	if s.active == nil {
		return nil
	}
	return s.active.Close(ctx)
}

func (s *Source) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	if s.active == nil {
		return nil, errors.New("BigQuery source is not connected")
	}
	return s.active.GetTable(ctx, req)
}

func (s *Source) HandlesIncrementality() bool {
	return false
}

var (
	_ source.Source = (*Source)(nil)
	_ source.Source = (*adbc.ADBCSource)(nil)
	_ source.Source = (*restSource)(nil)
)
