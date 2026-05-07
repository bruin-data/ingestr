package appstore

import (
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/arrowconv"
	gonghttp "github.com/bruin-data/gong/pkg/http"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/source"
	"github.com/golang-jwt/jwt/v5"
)

const (
	apiBaseURL         = "https://api.appstoreconnect.apple.com"
	defaultBatch       = 10000
	defaultParallelism = 5
	jwtAudience        = "appstoreconnect-v1"
	jwtExpDuration     = 10 * time.Minute
)

type resourceConfig struct {
	reportName     string
	primaryKeys    []string
	columns        []schema.Column
	strategy       config.IncrementalStrategy
	incrementalKey string
}

func col(name string, dt schema.DataType) schema.Column {
	return schema.Column{Name: name, DataType: dt, Nullable: true}
}

var resMeta = map[string]resourceConfig{
	"app-downloads-detailed": {
		reportName: "App Downloads Detailed",
		primaryKeys: []string{
			"app_apple_identifier", "app_name", "app_version", "campaign",
			"date", "device", "download_type", "page_title", "page_type",
			"platform_version", "pre_order", "source_info", "source_type", "territory",
		},
		columns: []schema.Column{
			col("date", schema.TypeDate),
			col("app_apple_identifier", schema.TypeInt64),
			col("counts", schema.TypeInt64),
			col("processing_date", schema.TypeDate),
		},
		strategy:       config.StrategyMerge,
		incrementalKey: "processing_date",
	},
	"app-store-discovery-and-engagement-detailed": {
		reportName: "App Store Discovery and Engagement Detailed",
		primaryKeys: []string{
			"app_apple_identifier", "app_name", "campaign", "date", "device",
			"engagement_type", "event", "page_title", "page_type",
			"platform_version", "source_info", "source_type", "territory",
		},
		columns: []schema.Column{
			col("date", schema.TypeDate),
			col("app_apple_identifier", schema.TypeInt64),
			col("counts", schema.TypeInt64),
			col("unique_counts", schema.TypeInt64),
			col("processing_date", schema.TypeDate),
		},
		strategy:       config.StrategyMerge,
		incrementalKey: "processing_date",
	},
	"app-sessions-detailed": {
		reportName: "App Sessions Detailed",
		primaryKeys: []string{
			"date", "app_name", "app_apple_identifier", "app_version",
			"device", "platform_version", "source_type", "source_info",
			"campaign", "page_type", "page_title", "app_download_date", "territory",
		},
		columns: []schema.Column{
			col("date", schema.TypeDate),
			col("app_apple_identifier", schema.TypeInt64),
			col("sessions", schema.TypeInt64),
			col("total_session_duration", schema.TypeInt64),
			col("unique_devices", schema.TypeInt64),
			col("processing_date", schema.TypeDate),
		},
		strategy:       config.StrategyMerge,
		incrementalKey: "processing_date",
	},
	"app-store-installation-and-deletion-detailed": {
		reportName: "App Store Installation and Deletion Detailed",
		primaryKeys: []string{
			"app_apple_identifier", "app_download_date", "app_name", "app_version",
			"campaign", "counts", "date", "device", "download_type", "event",
			"page_title", "page_type", "platform_version", "source_info",
			"source_type", "territory", "unique_devices",
		},
		columns: []schema.Column{
			col("date", schema.TypeDate),
			col("app_apple_identifier", schema.TypeInt64),
			col("counts", schema.TypeInt64),
			col("unique_devices", schema.TypeInt64),
			col("app_download_date", schema.TypeDate),
			col("processing_date", schema.TypeDate),
		},
		strategy:       config.StrategyMerge,
		incrementalKey: "processing_date",
	},
	"app-store-purchases-detailed": {
		reportName: "App Store Purchases Detailed",
		primaryKeys: []string{
			"app_apple_identifier", "app_download_date", "app_name", "campaign",
			"content_apple_identifier", "content_name", "date", "device",
			"page_title", "page_type", "payment_method", "platform_version",
			"pre_order", "purchase_type", "source_info", "source_type", "territory",
		},
		columns: []schema.Column{
			col("date", schema.TypeDate),
			col("app_apple_identifier", schema.TypeInt64),
			col("app_download_date", schema.TypeDate),
			col("content_apple_identifier", schema.TypeInt64),
			col("purchases", schema.TypeInt64),
			col("proceeds_in_usd", schema.TypeFloat64),
			col("sales_in_usd", schema.TypeFloat64),
			col("paying_users", schema.TypeInt64),
			col("processing_date", schema.TypeDate),
		},
		strategy:       config.StrategyMerge,
		incrementalKey: "processing_date",
	},
	"app-crashes-expanded": {
		reportName: "App Crashes Expanded",
		primaryKeys: []string{
			"app_name", "app_version", "build", "date",
			"device", "platform", "release_type", "territory",
		},
		columns: []schema.Column{
			col("date", schema.TypeDate),
			col("processing_date", schema.TypeDate),
			col("app_apple_identifier", schema.TypeInt64),
			col("count", schema.TypeInt64),
			col("unique_devices", schema.TypeInt64),
		},
		strategy:       config.StrategyMerge,
		incrementalKey: "processing_date",
	},
}

type AppStoreSource struct {
	client   *gonghttp.Client
	key      string
	keyID    string
	issuerID string
	appIDs   []string
}

func NewAppStoreSource() *AppStoreSource {
	return &AppStoreSource{}
}

func (s *AppStoreSource) Schemes() []string {
	return []string{"appstore"}
}

func (s *AppStoreSource) Connect(ctx context.Context, uri string) error {
	keyID, issuerID, appIDs, key, err := parseURI(uri)
	if err != nil {
		return err
	}

	s.key = key
	s.keyID = keyID
	s.issuerID = issuerID
	s.appIDs = appIDs

	token, err := s.generateToken()
	if err != nil {
		return fmt.Errorf("failed to generate JWT: %w", err)
	}

	s.client = gonghttp.New(
		gonghttp.WithBaseURL(apiBaseURL),
		gonghttp.WithTimeout(120*time.Second),
		gonghttp.WithRateLimiter(1, 1),
		gonghttp.WithAuth(gonghttp.NewBearerAuth(token)),
		gonghttp.WithDebug(config.DebugMode),
	)

	config.Debug("[APPSTORE] Connected successfully")
	return nil
}

func parseURI(uri string) (keyID, issuerID string, appIDs []string, key string, err error) {
	if !strings.HasPrefix(uri, "appstore://") {
		return "", "", nil, "", fmt.Errorf("invalid appstore URI: must start with appstore://")
	}

	parsed, err := url.Parse(uri)
	if err != nil {
		return "", "", nil, "", fmt.Errorf("failed to parse appstore URI: %w", err)
	}

	params := parsed.Query()

	keyID = params.Get("key_id")
	if keyID == "" {
		return "", "", nil, "", fmt.Errorf("key_id is required in appstore URI")
	}

	issuerID = params.Get("issuer_id")
	if issuerID == "" {
		return "", "", nil, "", fmt.Errorf("issuer_id is required in appstore URI")
	}

	keyPath := params.Get("key_path")
	keyBase64 := params.Get("key_base64")

	if keyPath == "" && keyBase64 == "" {
		return "", "", nil, "", fmt.Errorf("key_path or key_base64 is required in appstore URI")
	}

	if keyPath != "" {
		data, err := os.ReadFile(keyPath)
		if err != nil {
			return "", "", nil, "", fmt.Errorf("failed to read key file %s: %w", keyPath, err)
		}
		key = string(data)
	} else {
		decoded, err := base64.StdEncoding.DecodeString(keyBase64)
		if err != nil {
			return "", "", nil, "", fmt.Errorf("failed to decode key_base64: %w", err)
		}
		key = string(decoded)
	}

	if appIDParam := params.Get("app_id"); appIDParam != "" {
		appIDs = strings.Split(appIDParam, ",")
	}

	return keyID, issuerID, appIDs, key, nil
}

func (s *AppStoreSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *AppStoreSource) HandlesIncrementality() bool {
	return true
}

func (s *AppStoreSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name
	appIDs := s.appIDs

	if strings.Contains(tableName, ":") {
		parts := strings.SplitN(tableName, ":", 2)
		tableName = parts[0]
		appIDs = strings.Split(parts[1], ",")
	}

	res, ok := resMeta[tableName]
	if !ok {
		available := make([]string, 0, len(resMeta))
		for k := range resMeta {
			available = append(available, k)
		}
		return nil, fmt.Errorf("unsupported table %q, available tables: %s", tableName, strings.Join(available, ", "))
	}

	if len(appIDs) == 0 {
		return nil, fmt.Errorf("app_id is required: specify in URI or use table:app_id1,app_id2 syntax")
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    res.primaryKeys,
		TableIncrementalKey: res.incrementalKey,
		TableStrategy:       res.strategy,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("appstore source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, appIDs, opts)
		},
	}, nil
}

func (s *AppStoreSource) read(ctx context.Context, tableName string, appIDs []string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	if opts.IntervalStart == nil || opts.IntervalEnd == nil {
		return nil, fmt.Errorf("appstore source requires both interval_start and interval_end to be provided")
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		if err := s.refreshToken(); err != nil {
			results <- source.RecordBatchResult{Err: fmt.Errorf("failed to refresh JWT: %w", err)}
			return
		}

		startDate := *opts.IntervalStart
		endDate := *opts.IntervalEnd

		res := resMeta[tableName]
		reportName := res.reportName

		batchSize := defaultBatch
		if opts.PageSize > 0 {
			batchSize = opts.PageSize
		}

		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		var wg sync.WaitGroup
		for _, appID := range appIDs {
			wg.Add(1)
			go func(appID string) {
				defer wg.Done()

				config.Debug("[APPSTORE] Processing app %s for report %q", appID, reportName)

				rows, err := s.fetchReportData(ctx, appID, reportName, startDate, endDate)
				if err != nil {
					select {
					case results <- source.RecordBatchResult{Err: fmt.Errorf("failed to fetch report for app %s: %w", appID, err)}:
					case <-ctx.Done():
					}
					cancel()
					return
				}

				if len(rows) == 0 {
					config.Debug("[APPSTORE] No data for app %s", appID)
					return
				}

				for i := 0; i < len(rows); i += batchSize {
					end := i + batchSize
					if end > len(rows) {
						end = len(rows)
					}

					batch := rows[i:end]
					record, err := arrowconv.ItemsToArrowRecordWithSchema(batch, res.columns, opts.ExcludeColumns)
					if err != nil {
						select {
						case results <- source.RecordBatchResult{Err: fmt.Errorf("failed to convert to Arrow: %w", err)}:
						case <-ctx.Done():
						}
						cancel()
						return
					}

					config.Debug("[APPSTORE] Emitting batch of %d rows for app %s", len(batch), appID)
					select {
					case results <- source.RecordBatchResult{Batch: record}:
					case <-ctx.Done():
						return
					}
				}
			}(appID)
		}

		wg.Wait()
	}()

	return results, nil
}

func (s *AppStoreSource) fetchReportData(ctx context.Context, appID, reportName string, startDate, endDate time.Time) ([]map[string]interface{}, error) {
	requestID, err := s.findReportRequest(ctx, appID)
	if err != nil {
		return nil, err
	}

	reports, err := s.findReports(ctx, requestID, reportName)
	if err != nil {
		return nil, err
	}

	type result struct {
		rows []map[string]interface{}
		err  error
	}

	ch := make(chan result, defaultParallelism)
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for _, reportID := range reports {
		select {
		case <-ctx.Done():
		default:
		}

		wg.Add(1)
		go func(reportID string) {
			defer wg.Done()

			if ctx.Err() != nil {
				return
			}

			instances, err := s.getReportInstances(ctx, reportID, startDate, endDate)
			if err != nil {
				select {
				case ch <- result{err: err}:
				case <-ctx.Done():
				}
				cancel()
				return
			}

			config.Debug("[APPSTORE] Found %d report instances for app %s report %s", len(instances), appID, reportID)

			instCh := make(chan result, len(instances))
			var instWg sync.WaitGroup

			for _, inst := range instances {
				if ctx.Err() != nil {
					break
				}

				instWg.Add(1)
				go func(inst reportInstance) {
					defer instWg.Done()

					if ctx.Err() != nil {
						return
					}

					segments, err := s.getSegments(ctx, inst.id)
					if err != nil {
						instCh <- result{err: fmt.Errorf("failed to get segments for instance %s: %w", inst.id, err)}
						cancel()
						return
					}

					var rows []map[string]interface{}
					for _, segURL := range segments {
						if ctx.Err() != nil {
							return
						}
						parsed, err := s.downloadAndParseReport(ctx, segURL, reportName, inst.processingDate)
						if err != nil {
							instCh <- result{err: fmt.Errorf("failed to download segment: %w", err)}
							cancel()
							return
						}
						rows = append(rows, parsed...)
					}

					instCh <- result{rows: rows}
				}(inst)
			}

			instWg.Wait()
			close(instCh)

			for r := range instCh {
				if r.err != nil {
					select {
					case ch <- r:
					case <-ctx.Done():
					}
					cancel()
					return
				}
				select {
				case ch <- r:
				case <-ctx.Done():
					return
				}
			}
		}(reportID)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	var allRows []map[string]interface{}
	for r := range ch {
		if r.err != nil {
			return nil, r.err
		}
		allRows = append(allRows, r.rows...)
	}

	return allRows, nil
}

type reportInstance struct {
	id             string
	processingDate string
}

func (s *AppStoreSource) findReportRequest(ctx context.Context, appID string) (string, error) {
	endpoint := fmt.Sprintf("/v1/apps/%s/analyticsReportRequests", appID)
	nextURL := ""
	firstRequest := true

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		var response struct {
			Data []struct {
				ID         string `json:"id"`
				Attributes struct {
					AccessType             string `json:"accessType"`
					StoppedDueToInactivity bool   `json:"stoppedDueToInactivity"`
				} `json:"attributes"`
			} `json:"data"`
			Links struct {
				Next string `json:"next"`
			} `json:"links"`
		}

		var resp *gonghttp.Response
		var err error

		if !firstRequest && nextURL != "" {
			resp, err = s.client.R(ctx).Get(nextURL)
		} else {
			resp, err = s.client.R(ctx).
				SetQueryParam("filter[accessType]", "ONGOING").
				Get(endpoint)
		}
		firstRequest = false

		if err != nil {
			return "", fmt.Errorf("failed to list report requests: %w", err)
		}
		if !resp.IsSuccess() {
			return "", fmt.Errorf("failed to list report requests (status %d): %s", resp.StatusCode(), resp.String())
		}

		if err := json.Unmarshal(resp.Body(), &response); err != nil {
			return "", fmt.Errorf("failed to parse report requests response: %w", err)
		}

		for _, req := range response.Data {
			if req.Attributes.AccessType == "ONGOING" && !req.Attributes.StoppedDueToInactivity {
				config.Debug("[APPSTORE] Found report request %s for app %s", req.ID, appID)
				return req.ID, nil
			}
		}

		if response.Links.Next == "" {
			break
		}

		parsed, err := url.Parse(response.Links.Next)
		if err != nil {
			break
		}
		nextURL = parsed.RequestURI()
	}

	return "", fmt.Errorf("no ONGOING report requests found for app %s (or they are stopped due to inactivity)", appID)
}

func (s *AppStoreSource) findReports(ctx context.Context, requestID, reportName string) ([]string, error) {
	endpoint := fmt.Sprintf("/v1/analyticsReportRequests/%s/reports", requestID)
	nextURL := ""
	firstRequest := true

	var ids []string

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		var response struct {
			Data []struct {
				ID         string `json:"id"`
				Attributes struct {
					Name     string `json:"name"`
					Category string `json:"category"`
				} `json:"attributes"`
			} `json:"data"`
			Links struct {
				Next string `json:"next"`
			} `json:"links"`
		}

		var resp *gonghttp.Response
		var err error

		if !firstRequest && nextURL != "" {
			resp, err = s.client.R(ctx).Get(nextURL)
		} else {
			resp, err = s.client.R(ctx).
				SetQueryParam("filter[name]", reportName).
				Get(endpoint)
		}
		firstRequest = false

		if err != nil {
			return nil, fmt.Errorf("failed to list reports: %w", err)
		}
		if !resp.IsSuccess() {
			return nil, fmt.Errorf("failed to list reports (status %d): %s", resp.StatusCode(), resp.String())
		}

		if err := json.Unmarshal(resp.Body(), &response); err != nil {
			return nil, fmt.Errorf("failed to parse reports response: %w", err)
		}

		for _, r := range response.Data {
			config.Debug("[APPSTORE] Found report %s (%s)", r.ID, r.Attributes.Name)
			ids = append(ids, r.ID)
		}

		if response.Links.Next == "" {
			break
		}

		parsed, err := url.Parse(response.Links.Next)
		if err != nil {
			break
		}
		nextURL = parsed.RequestURI()
	}

	if len(ids) == 0 {
		return nil, fmt.Errorf("no such report found: %s", reportName)
	}

	return ids, nil
}

func (s *AppStoreSource) getReportInstances(ctx context.Context, reportID string, startDate, endDate time.Time) ([]reportInstance, error) {
	endpoint := fmt.Sprintf("/v1/analyticsReports/%s/instances", reportID)

	var allInstances []reportInstance
	nextURL := ""
	firstRequest := true

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		var response struct {
			Data []struct {
				ID         string `json:"id"`
				Attributes struct {
					Granularity    string `json:"granularity"`
					ProcessingDate string `json:"processingDate"`
				} `json:"attributes"`
			} `json:"data"`
			Links struct {
				Next string `json:"next"`
			} `json:"links"`
		}

		var resp *gonghttp.Response
		var err error

		if !firstRequest && nextURL != "" {
			resp, err = s.client.R(ctx).Get(nextURL)
		} else {
			resp, err = s.client.R(ctx).
				SetQueryParam("filter[granularity]", "DAILY").
				Get(endpoint)
		}
		firstRequest = false

		if err != nil {
			return nil, fmt.Errorf("failed to list report instances: %w", err)
		}
		if !resp.IsSuccess() {
			return nil, fmt.Errorf("failed to list report instances (status %d): %s", resp.StatusCode(), resp.String())
		}

		if err := json.Unmarshal(resp.Body(), &response); err != nil {
			return nil, fmt.Errorf("failed to parse report instances: %w", err)
		}

		for _, inst := range response.Data {
			procDate := inst.Attributes.ProcessingDate
			if !isInDateRange(procDate, startDate, endDate) {
				continue
			}
			allInstances = append(allInstances, reportInstance{
				id:             inst.ID,
				processingDate: procDate,
			})
		}

		if response.Links.Next == "" {
			break
		}

		parsed, err := url.Parse(response.Links.Next)
		if err != nil {
			break
		}
		nextURL = parsed.RequestURI()
	}

	if len(allInstances) == 0 {
		config.Debug("[AppStore] no report instances found for the given date range")
	}

	return allInstances, nil
}

func (s *AppStoreSource) getSegments(ctx context.Context, instanceID string) ([]string, error) {
	endpoint := fmt.Sprintf("/v1/analyticsReportInstances/%s/segments", instanceID)

	var urls []string
	nextURL := ""
	firstRequest := true

	for {
		var response struct {
			Data []struct {
				ID         string `json:"id"`
				Attributes struct {
					URL         string `json:"url"`
					Checksum    string `json:"checksum"`
					SizeInBytes int64  `json:"sizeInBytes"`
				} `json:"attributes"`
			} `json:"data"`
			Links struct {
				Next string `json:"next"`
			} `json:"links"`
		}

		var resp *gonghttp.Response
		var err error

		if !firstRequest && nextURL != "" {
			resp, err = s.client.R(ctx).Get(nextURL)
		} else {
			resp, err = s.client.R(ctx).Get(endpoint)
		}
		firstRequest = false

		if err != nil {
			return nil, fmt.Errorf("failed to get segments: %w", err)
		}
		if !resp.IsSuccess() {
			return nil, fmt.Errorf("failed to get segments (status %d): %s", resp.StatusCode(), resp.String())
		}

		if err := json.Unmarshal(resp.Body(), &response); err != nil {
			return nil, fmt.Errorf("failed to parse segments: %w", err)
		}

		for _, seg := range response.Data {
			if seg.Attributes.URL != "" {
				urls = append(urls, seg.Attributes.URL)
			}
		}

		if response.Links.Next == "" {
			break
		}

		parsed, err := url.Parse(response.Links.Next)
		if err != nil {
			break
		}
		nextURL = parsed.RequestURI()
	}

	return urls, nil
}

func (s *AppStoreSource) downloadAndParseReport(ctx context.Context, downloadURL, reportName, processingDate string) ([]map[string]interface{}, error) {
	config.Debug("[APPSTORE] Downloading segment from %s", downloadURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create download request: %w", err)
	}

	httpClient := &http.Client{Timeout: 120 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download report: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	gzReader, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer func() {
		_ = gzReader.Close()
	}()

	delimiter := '\t'
	if reportName == "App Crashes Expanded" {
		delimiter = ','
	}

	csvReader := csv.NewReader(gzReader)
	csvReader.Comma = delimiter
	csvReader.LazyQuotes = true

	headers, err := csvReader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read headers: %w", err)
	}

	for i, h := range headers {
		headers[i] = normalizeColumnName(h)
	}

	var rows []map[string]interface{}
	for {
		record, err := csvReader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read row: %w", err)
		}

		row := make(map[string]interface{}, len(headers)+1)
		row["processing_date"] = processingDate
		for i, val := range record {
			if i < len(headers) {
				row[headers[i]] = val
			}
		}
		rows = append(rows, row)
	}

	config.Debug("[APPSTORE] Parsed %d rows from segment", len(rows))
	return rows, nil
}

func (s *AppStoreSource) generateToken() (string, error) {
	privateKey, err := jwt.ParseECPrivateKeyFromPEM([]byte(s.key))
	if err != nil {
		return "", fmt.Errorf("failed to parse EC private key: %w", err)
	}

	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    s.issuerID,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(jwtExpDuration)),
		Audience:  jwt.ClaimStrings{jwtAudience},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = s.keyID

	return token.SignedString(privateKey)
}

func (s *AppStoreSource) refreshToken() error {
	token, err := s.generateToken()
	if err != nil {
		return err
	}

	s.client = gonghttp.New(
		gonghttp.WithBaseURL(apiBaseURL),
		gonghttp.WithTimeout(120*time.Second),
		gonghttp.WithRateLimiter(1, 1),
		gonghttp.WithAuth(gonghttp.NewBearerAuth(token)),
		gonghttp.WithDebug(config.DebugMode),
	)
	return nil
}

func normalizeColumnName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, "-", "_")
	return name
}

func isInDateRange(dateStr string, start, end time.Time) bool {
	d, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return false
	}
	return !d.Before(start) && !d.After(end)
}

var _ source.Source = (*AppStoreSource)(nil)
