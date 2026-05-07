package primer

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/gong/internal/config"
	"github.com/bruin-data/gong/pkg/arrowconv"
	gonghttp "github.com/bruin-data/gong/pkg/http"
	"github.com/bruin-data/gong/pkg/schema"
	"github.com/bruin-data/gong/pkg/source"
)

const (
	baseURL            = "https://api.primer.io"
	apiVersion         = "2.4"
	defaultPageSize    = 100
	defaultParallelism = 7
)

type paymentResult struct {
	data map[string]interface{}
	err  error
}

type PrimerSource struct {
	client *gonghttp.Client
	apiKey string
}

func NewPrimerSource() *PrimerSource {
	return &PrimerSource{}
}

func (s *PrimerSource) Schemes() []string {
	return []string{"primer"}
}

func (s *PrimerSource) Connect(ctx context.Context, uri string) error {
	apiKey, err := parsePrimerURI(uri)
	if err != nil {
		return err
	}

	s.apiKey = apiKey

	s.client = gonghttp.New(
		gonghttp.WithBaseURL(baseURL),
		gonghttp.WithTimeout(60*time.Second),
		gonghttp.WithDebug(config.DebugMode),
		gonghttp.WithAuth(gonghttp.NewAPIKeyAuth("X-API-KEY", apiKey, true)),
		gonghttp.WithHeader("X-API-VERSION", apiVersion),
	)

	config.Debug("[PRIMER] Connected successfully")
	return nil
}

func parsePrimerURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, "primer://") {
		return "", fmt.Errorf("invalid primer URI: must start with primer://")
	}

	rest := strings.TrimPrefix(uri, "primer://")
	if rest == "" || rest == "?" {
		return "", fmt.Errorf("api_key is required in primer URI")
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", fmt.Errorf("failed to parse primer URI query: %w", err)
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return "", fmt.Errorf("api_key is required in primer URI")
	}

	return apiKey, nil
}

func (s *PrimerSource) Close(ctx context.Context) error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *PrimerSource) HandlesIncrementality() bool {
	return true
}

func (s *PrimerSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName, statuses, err := parseTableName(req.Name)
	if err != nil {
		return nil, err
	}

	return &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    []string{"id"},
		TableIncrementalKey: "dateUpdated",
		TableStrategy:       config.StrategyMerge,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("primer source does not have a predefined schema; schema inference is required")
		},
		ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
			return s.read(ctx, tableName, statuses, opts)
		},
	}, nil
}

func (s *PrimerSource) read(ctx context.Context, table string, statuses []string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		var err error
		switch table {
		case "payments":
			err = s.readPayments(ctx, statuses, opts, results)
		default:
			err = fmt.Errorf("unsupported table: %s, supported tables are: payments", table)
		}

		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

var validStatuses = map[string]bool{
	"AUTHORIZED":        true,
	"CANCELLED":         true,
	"DECLINED":          true,
	"FAILED":            true,
	"PARTIALLY_SETTLED": true,
	"PENDING":           true,
	"SETTLED":           true,
	"SETTLING":          true,
}

func allStatuses() []string {
	all := make([]string, 0, len(validStatuses))
	for k := range validStatuses {
		all = append(all, k)
	}
	sort.Strings(all)
	return all
}

func validStatusList() string {
	return strings.Join(allStatuses(), ", ")
}

func parseTableName(table string) (string, []string, error) {
	parts := strings.SplitN(table, ":", 2)
	if len(parts) == 1 {
		return table, allStatuses(), nil
	}

	rawStatuses := strings.Split(parts[1], ",")
	seen := make(map[string]bool, len(rawStatuses))
	var statuses []string
	for _, s := range rawStatuses {
		s = strings.TrimSpace(strings.ToUpper(s))
		if s == "" || seen[s] {
			continue
		}
		if !validStatuses[s] {
			return "", nil, fmt.Errorf("invalid payment status %q, valid statuses are: %s", s, validStatusList())
		}
		seen[s] = true
		statuses = append(statuses, s)
	}
	if len(statuses) == 0 {
		return "", nil, fmt.Errorf("no payment status provided, use 'payments' for all statuses or 'payments:<status>' to filter, valid statuses are: %s", validStatusList())
	}
	return parts[0], statuses, nil
}

func (s *PrimerSource) readPayments(ctx context.Context, statuses []string, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	startTime, err := toTime(opts.IntervalStart)
	if err != nil {
		return fmt.Errorf("interval start is required for primer payments: provide --interval-start")
	}
	endTime, err := toTime(opts.IntervalEnd)
	if err != nil {
		return fmt.Errorf("interval end is required for primer payments: provide --interval-end")
	}

	startTime = startOfDay(startTime)
	endTime = startOfDay(endTime.AddDate(0, 0, 1))

	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = defaultParallelism
	}

	days := splitIntoDays(startTime, endTime)
	config.Debug("[PRIMER] Split time range into %d daily windows, %d statuses, parallelism=%d", len(days), len(statuses), parallelism)

	type listTask struct {
		status string
		from   time.Time
		to     time.Time
	}

	taskChan := make(chan listTask, len(days)*len(statuses))
	idChan := make(chan string, parallelism*2)
	resultChan := make(chan paymentResult, parallelism*2)

	for _, day := range days {
		for _, status := range statuses {
			taskChan <- listTask{status: status, from: day.start, to: day.end}
		}
	}
	close(taskChan)

	// Detail fetcher workers
	var detailWg sync.WaitGroup
	for i := 0; i < parallelism; i++ {
		detailWg.Add(1)
		go func() {
			defer detailWg.Done()
			for id := range idChan {
				select {
				case <-ctx.Done():
					return
				default:
				}

				var paymentDetail map[string]interface{}
				httpResp, err := s.client.R(ctx).
					SetResult(&paymentDetail).
					Get("/payments/" + id)
				if err != nil {
					resultChan <- paymentResult{err: fmt.Errorf("failed to fetch payment %s: %w", id, err)}
					continue
				}
				if !httpResp.IsSuccess() {
					resultChan <- paymentResult{err: fmt.Errorf("payment detail request for %s failed with status %d: %s", id, httpResp.StatusCode(), httpResp.String())}
					continue
				}

				resultChan <- paymentResult{data: paymentDetail}
			}
		}()
	}

	// List workers: process day+status tasks and feed IDs
	go func() {
		defer close(idChan)
		var listWg sync.WaitGroup
		listWorkers := parallelism
		totalTasks := len(days) * len(statuses)
		if listWorkers > totalTasks {
			listWorkers = totalTasks
		}
		for i := 0; i < listWorkers; i++ {
			listWg.Add(1)
			go func() {
				defer listWg.Done()
				for task := range taskChan {
					select {
					case <-ctx.Done():
						return
					default:
					}
					s.listPaymentIDs(ctx, task.status, task.from, task.to, idChan, resultChan)
				}
			}()
		}
		listWg.Wait()
	}()

	go func() {
		detailWg.Wait()
		close(resultChan)
	}()

	var batch []map[string]any
	totalSent := 0
	for res := range resultChan {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if res.err != nil {
			return res.err
		}
		batch = append(batch, res.data)
		if len(batch) >= defaultPageSize {
			rec, err := arrowconv.ItemsToArrowRecordWithSchema(batch, nil, opts.ExcludeColumns)
			if err != nil {
				return err
			}
			totalSent += len(batch)
			config.Debug("[PRIMER] Sending batch of %d payments (total: %d)", len(batch), totalSent)
			results <- source.RecordBatchResult{Batch: rec}
			batch = nil
		}
	}
	if len(batch) > 0 {
		rec, err := arrowconv.ItemsToArrowRecordWithSchema(batch, nil, opts.ExcludeColumns)
		if err != nil {
			return err
		}
		totalSent += len(batch)
		config.Debug("[PRIMER] Sending batch of %d payments (total: %d)", len(batch), totalSent)
		results <- source.RecordBatchResult{Batch: rec}
	}
	config.Debug("[PRIMER] Finished reading payments, total: %d", totalSent)
	return nil
}

func (s *PrimerSource) listPaymentIDs(ctx context.Context, status string, from, to time.Time, idChan chan<- string, resultChan chan<- paymentResult) {
	cursor := ""
	totalIds := 0
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		params := url.Values{
			"from_date": {from.UTC().Format(time.RFC3339)},
			"to_date":   {to.UTC().Format(time.RFC3339)},
			"limit":     {strconv.Itoa(defaultPageSize)},
			"status":    {status},
		}
		if cursor != "" {
			params.Set("cursor", cursor)
		}

		var result map[string]interface{}
		httpResp, err := s.client.R(ctx).
			SetQueryParamValues(params).
			SetResult(&result).
			Get("/payments")
		if err != nil {
			resultChan <- paymentResult{err: fmt.Errorf("failed to fetch payments (status=%s, date=%s): %w", status, from.Format("2006-01-02"), err)}
			return
		}
		if !httpResp.IsSuccess() {
			resultChan <- paymentResult{err: fmt.Errorf("payments request (status=%s, date=%s) failed with status %d: %s", status, from.Format("2006-01-02"), httpResp.StatusCode(), httpResp.String())}
			return
		}

		data, _ := result["data"].([]interface{})
		for _, item := range data {
			if p, ok := item.(map[string]interface{}); ok {
				if id, ok := p["id"].(string); ok {
					totalIds++
					select {
					case idChan <- id:
					case <-ctx.Done():
						return
					}
				}
			}
		}

		cursor, _ = result["nextCursor"].(string)
		if cursor == "" {
			break
		}
	}
	if totalIds > 0 {
		config.Debug("[PRIMER] Listed %d IDs for status=%s date=%s", totalIds, status, from.Format("2006-01-02"))
	}
}

type timeWindow struct {
	start time.Time
	end   time.Time
}

func splitIntoDays(start, end time.Time) []timeWindow {
	var windows []timeWindow
	current := start
	for current.Before(end) {
		next := startOfDay(current.AddDate(0, 0, 1))
		if next.After(end) {
			next = end
		}
		windows = append(windows, timeWindow{start: current, end: next})
		current = next
	}
	return windows
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func toTime(v any) (time.Time, error) {
	if v == nil {
		return time.Time{}, fmt.Errorf("nil value")
	}
	switch t := v.(type) {
	case time.Time:
		return t, nil
	case *time.Time:
		if t == nil {
			return time.Time{}, fmt.Errorf("nil pointer")
		}
		return *t, nil
	}
	return time.Time{}, fmt.Errorf("unsupported type %T", v)
}

var _ source.Source = (*PrimerSource)(nil)
