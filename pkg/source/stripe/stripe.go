package stripe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/internal/output"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/account"
	"github.com/stripe/stripe-go/v81/applepaydomain"
	"github.com/stripe/stripe-go/v81/applicationfee"
	"github.com/stripe/stripe-go/v81/balancetransaction"
	"github.com/stripe/stripe-go/v81/charge"
	"github.com/stripe/stripe-go/v81/checkout/session"
	"github.com/stripe/stripe-go/v81/coupon"
	"github.com/stripe/stripe-go/v81/creditnote"
	"github.com/stripe/stripe-go/v81/customer"
	"github.com/stripe/stripe-go/v81/dispute"
	"github.com/stripe/stripe-go/v81/event"
	"github.com/stripe/stripe-go/v81/invoice"
	"github.com/stripe/stripe-go/v81/invoiceitem"
	"github.com/stripe/stripe-go/v81/paymentintent"
	"github.com/stripe/stripe-go/v81/paymentlink"
	"github.com/stripe/stripe-go/v81/paymentmethod"
	"github.com/stripe/stripe-go/v81/payout"
	"github.com/stripe/stripe-go/v81/plan"
	"github.com/stripe/stripe-go/v81/price"
	"github.com/stripe/stripe-go/v81/product"
	"github.com/stripe/stripe-go/v81/promotioncode"
	"github.com/stripe/stripe-go/v81/quote"
	"github.com/stripe/stripe-go/v81/refund"
	"github.com/stripe/stripe-go/v81/review"
	"github.com/stripe/stripe-go/v81/setupattempt"
	"github.com/stripe/stripe-go/v81/setupintent"
	"github.com/stripe/stripe-go/v81/shippingrate"
	"github.com/stripe/stripe-go/v81/subscription"
	"github.com/stripe/stripe-go/v81/subscriptionitem"
	"github.com/stripe/stripe-go/v81/subscriptionschedule"
	"github.com/stripe/stripe-go/v81/taxcode"
	"github.com/stripe/stripe-go/v81/taxid"
	"github.com/stripe/stripe-go/v81/taxrate"
	"github.com/stripe/stripe-go/v81/topup"
	"github.com/stripe/stripe-go/v81/transfer"
	"github.com/stripe/stripe-go/v81/webhookendpoint"
)

const (
	defaultBatchSize       = 100
	defaultSyncParallelism = 10
	maxFanoutParallelism   = 32
	maxAdaptiveChunks      = 128
)

type loadingMode int

const (
	modeAsync loadingMode = iota
	modeSync
	modeSyncIncremental
)

type StripeSource struct {
	apiKey   string
	governor *requestGovernor
}

func NewStripeSource() *StripeSource {
	return &StripeSource{}
}

func (s *StripeSource) Schemes() []string {
	return []string{"stripe"}
}

func (s *StripeSource) Connect(ctx context.Context, uri string) error {
	apiKey, err := parseAPIKeyFromURI(uri)
	if err != nil {
		return err
	}
	s.apiKey = apiKey
	s.governor = sharedGovernorRegistry.governorForKey(apiKey)
	stripe.Key = apiKey

	// Govern all Stripe requests and retry rate-limit responses.
	wrapWithRetry(stripe.APIBackend)
	wrapWithRetry(stripe.UploadsBackend)

	config.Debug("[STRIPE] Connected successfully")
	return nil
}

func parseAPIKeyFromURI(uri string) (string, error) {
	if !strings.HasPrefix(uri, "stripe://") {
		return "", fmt.Errorf("invalid stripe URI: must start with stripe://")
	}

	rest := strings.TrimPrefix(uri, "stripe://")
	if rest == "" || rest == "?" {
		return "", fmt.Errorf("api_key is required in stripe URI")
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return "", fmt.Errorf("failed to parse stripe URI query: %w", err)
	}

	apiKey := values.Get("api_key")
	if apiKey == "" {
		return "", fmt.Errorf("api_key is required in stripe URI")
	}

	return apiKey, nil
}

func (s *StripeSource) Close(ctx context.Context) error {
	if s.governor != nil {
		config.Debug("[STRIPE] Request governor: %s", s.governor.stats())
		for _, endpoint := range s.governor.endpointStats() {
			config.Debug("[STRIPE] Endpoint %s: %d requests, %d errors, %d rate-limited, %d waits totaling %s, average API time %s",
				endpoint.path,
				endpoint.requests,
				endpoint.errors,
				endpoint.rateLimited,
				endpoint.waitedRequests,
				endpoint.totalWait.Round(time.Millisecond),
				endpoint.averageAPITime().Round(time.Millisecond),
			)
		}
	}
	return nil
}

func (s *StripeSource) HandlesIncrementality() bool {
	return true
}

func (s *StripeSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	tableName := req.Name

	normalizedName, mode := parseTableName(tableName)

	// Strategy per loading mode, matching ingestr semantics:
	//  - :sync:incremental  → merge (upsert date-window slices across runs)
	//  - default / async    → merge for event-stream tables (events carry deltas)
	//  - :sync              → replace (each run is a full snapshot)
	strategy := config.StrategyReplace
	incrementalKey := ""

	switch mode {
	case modeSyncIncremental:
		strategy = config.StrategyMerge
		incrementalKey = "created"
	case modeAsync:
		if tc, ok := tables[normalizedName]; ok && tc.eventTypeFilter != "" {
			strategy = config.StrategyMerge
			incrementalKey = "created"
		}
	}

	tbl := &source.DynamicSourceTable{
		TableName:           tableName,
		TablePrimaryKeys:    []string{"id"},
		TableStrategy:       strategy,
		TableIncrementalKey: incrementalKey,
		KnownSchema:         false,
		SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
			return nil, fmt.Errorf("stripe source does not have a predefined schema; schema inference is required")
		},
	}

	tbl.ReadFn = func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
		return s.read(ctx, tableName, opts)
	}

	return tbl, nil
}

func parseTableName(table string) (tableName string, mode loadingMode) {
	parts := strings.Split(table, ":")
	tableName = normalizeTableName(parts[0])
	mode = modeAsync

	if len(parts) >= 2 && parts[1] == "sync" {
		mode = modeSync
		if len(parts) >= 3 && parts[2] == "incremental" {
			mode = modeSyncIncremental
		}
	}

	return tableName, mode
}

func normalizeTableName(name string) string {
	normalized := strings.ReplaceAll(name, "_", "")
	aliases := map[string]string{
		"checkoutsession":      "checkout_session",
		"paymentintent":        "payment_intent",
		"paymentlink":          "payment_link",
		"paymentmethod":        "payment_method",
		"paymentmethoddomain":  "payment_method_domain",
		"promotioncode":        "promotion_code",
		"setupattempt":         "setup_attempt",
		"setupintent":          "setup_intent",
		"shippingrate":         "shipping_rate",
		"subscriptionitem":     "subscription_item",
		"subscriptionschedule": "subscription_schedule",
		"taxcode":              "tax_code",
		"taxid":                "tax_id",
		"taxrate":              "tax_rate",
		"topup":                "top_up",
		"webhookendpoint":      "webhook_endpoint",
		"applepaydomain":       "apple_pay_domain",
		"applicationfee":       "application_fee",
		"balancetransaction":   "balance_transaction",
		"creditnote":           "credit_note",
		"invoiceitem":          "invoice_item",
		"invoicelineitem":      "invoice_line_item",
	}

	if canonical, ok := aliases[normalized]; ok {
		return canonical
	}

	return name
}

type tableConfig struct {
	noDateFilter    bool
	eventTypeFilter string
	objectType      string
	parentIDField   string
}

var tables = map[string]tableConfig{
	"account":               {noDateFilter: true, eventTypeFilter: "account.*", objectType: "account", parentIDField: "account"},
	"apple_pay_domain":      {noDateFilter: true},
	"application_fee":       {eventTypeFilter: "application_fee.*", objectType: "application_fee", parentIDField: "fee"},
	"balance_transaction":   {},
	"charge":                {eventTypeFilter: "charge.*", objectType: "charge", parentIDField: "charge"},
	"checkout_session":      {eventTypeFilter: "checkout.session.*", objectType: "checkout.session"},
	"coupon":                {eventTypeFilter: "coupon.*", objectType: "coupon"},
	"credit_note":           {noDateFilter: true, eventTypeFilter: "credit_note.*", objectType: "credit_note"},
	"customer":              {eventTypeFilter: "customer.*", objectType: "customer", parentIDField: "customer"},
	"dispute":               {eventTypeFilter: "charge.dispute.*", objectType: "dispute"},
	"event":                 {},
	"invoice":               {eventTypeFilter: "invoice.*", objectType: "invoice"},
	"invoice_item":          {eventTypeFilter: "invoiceitem.*", objectType: "invoiceitem"},
	"payment_intent":        {eventTypeFilter: "payment_intent.*", objectType: "payment_intent"},
	"payment_link":          {noDateFilter: true, eventTypeFilter: "payment_link.*", objectType: "payment_link"},
	"payment_record":        {},
	"payment_method":        {noDateFilter: true, eventTypeFilter: "payment_method.*", objectType: "payment_method"},
	"payout":                {eventTypeFilter: "payout.*", objectType: "payout"},
	"plan":                  {eventTypeFilter: "plan.*", objectType: "plan"},
	"price":                 {eventTypeFilter: "price.*", objectType: "price"},
	"product":               {eventTypeFilter: "product.*", objectType: "product"},
	"promotion_code":        {eventTypeFilter: "promotion_code.*", objectType: "promotion_code"},
	"quote":                 {noDateFilter: true, eventTypeFilter: "quote.*", objectType: "quote"},
	"refund":                {eventTypeFilter: "refund.*", objectType: "refund"},
	"review":                {eventTypeFilter: "review.*", objectType: "review"},
	"setup_attempt":         {noDateFilter: true},
	"setup_intent":          {eventTypeFilter: "setup_intent.*", objectType: "setup_intent"},
	"shipping_rate":         {},
	"subscription":          {eventTypeFilter: "customer.subscription.*", objectType: "subscription"},
	"subscription_item":     {noDateFilter: true},
	"subscription_schedule": {eventTypeFilter: "subscription_schedule.*", objectType: "subscription_schedule"},
	"tax_code":              {noDateFilter: true},
	"tax_id":                {noDateFilter: true},
	"tax_rate":              {eventTypeFilter: "tax_rate.*", objectType: "tax_rate"},
	"top_up":                {eventTypeFilter: "topup.*", objectType: "topup"},
	"transfer":              {eventTypeFilter: "transfer.*", objectType: "transfer"},
	"webhook_endpoint":      {noDateFilter: true},
}

type timeWindow struct {
	start time.Time
	end   time.Time
}

func chunkSizeForParallelism(interval time.Duration, workers int) time.Duration {
	if interval <= 0 {
		return interval
	}
	if workers <= 0 {
		workers = defaultSyncParallelism
	}
	targetChunks := min(workers*2, maxAdaptiveChunks)
	chunkSize := interval / time.Duration(targetChunks)
	if interval%time.Duration(targetChunks) != 0 {
		chunkSize++
	}
	return max(chunkSize, time.Second)
}

func chunkTimeRange(start, end time.Time, chunkSize time.Duration) []timeWindow {
	if chunkSize <= 0 || !start.Before(end) {
		return []timeWindow{{start: start, end: end}}
	}
	chunks := make([]timeWindow, 0, int(end.Sub(start)/chunkSize)+1)
	cursor := start
	for cursor.Before(end) {
		next := cursor.Add(chunkSize)
		if next.After(end) {
			next = end
		}
		chunks = append(chunks, timeWindow{start: cursor, end: next})
		cursor = next
	}
	return chunks
}

func (s *StripeSource) hasRecordsInRange(ctx context.Context, tableName string, start, end time.Time) (bool, error) {
	cr := &stripe.RangeQueryParams{
		GreaterThanOrEqual: start.Unix(),
		LesserThanOrEqual:  end.Unix(),
	}
	lp := stripe.ListParams{Limit: stripe.Int64(1)}
	lp.Context = ctx

	type iter interface {
		Next() bool
		Err() error
	}

	var it iter
	switch tableName {
	case "application_fee":
		it = applicationfee.List(&stripe.ApplicationFeeListParams{ListParams: lp, CreatedRange: cr})
	case "balance_transaction":
		it = balancetransaction.List(&stripe.BalanceTransactionListParams{ListParams: lp, CreatedRange: cr})
	case "charge":
		it = charge.List(&stripe.ChargeListParams{ListParams: lp, CreatedRange: cr})
	case "checkout_session":
		it = session.List(&stripe.CheckoutSessionListParams{ListParams: lp, CreatedRange: cr})
	case "coupon":
		it = coupon.List(&stripe.CouponListParams{ListParams: lp, CreatedRange: cr})
	case "customer":
		it = customer.List(&stripe.CustomerListParams{ListParams: lp, CreatedRange: cr})
	case "dispute":
		it = dispute.List(&stripe.DisputeListParams{ListParams: lp, CreatedRange: cr})
	case "event":
		it = event.List(&stripe.EventListParams{ListParams: lp, CreatedRange: cr})
	case "invoice":
		it = invoice.List(&stripe.InvoiceListParams{ListParams: lp, CreatedRange: cr})
	case "invoice_item":
		it = invoiceitem.List(&stripe.InvoiceItemListParams{ListParams: lp, CreatedRange: cr})
	case "payment_intent":
		it = paymentintent.List(&stripe.PaymentIntentListParams{ListParams: lp, CreatedRange: cr})
	case "payment_record":
		it = paymentintent.List(&stripe.PaymentIntentListParams{ListParams: lp, CreatedRange: cr})
	case "payout":
		it = payout.List(&stripe.PayoutListParams{ListParams: lp, CreatedRange: cr})
	case "plan":
		it = plan.List(&stripe.PlanListParams{ListParams: lp, CreatedRange: cr})
	case "price":
		it = price.List(&stripe.PriceListParams{ListParams: lp, CreatedRange: cr})
	case "product":
		it = product.List(&stripe.ProductListParams{ListParams: lp, CreatedRange: cr})
	case "promotion_code":
		it = promotioncode.List(&stripe.PromotionCodeListParams{ListParams: lp, CreatedRange: cr})
	case "refund":
		it = refund.List(&stripe.RefundListParams{ListParams: lp, CreatedRange: cr})
	case "review":
		it = review.List(&stripe.ReviewListParams{ListParams: lp, CreatedRange: cr})
	case "setup_intent":
		it = setupintent.List(&stripe.SetupIntentListParams{ListParams: lp, CreatedRange: cr})
	case "shipping_rate":
		it = shippingrate.List(&stripe.ShippingRateListParams{ListParams: lp, CreatedRange: cr})
	case "subscription":
		it = subscription.List(&stripe.SubscriptionListParams{ListParams: lp, CreatedRange: cr, Status: stripe.String("all")})
	case "subscription_schedule":
		it = subscriptionschedule.List(&stripe.SubscriptionScheduleListParams{ListParams: lp, CreatedRange: cr})
	case "tax_rate":
		it = taxrate.List(&stripe.TaxRateListParams{ListParams: lp, CreatedRange: cr})
	case "top_up":
		it = topup.List(&stripe.TopupListParams{ListParams: lp, CreatedRange: cr})
	case "transfer":
		it = transfer.List(&stripe.TransferListParams{ListParams: lp, CreatedRange: cr})
	default:
		return false, fmt.Errorf("table %s does not support created range filtering", tableName)
	}

	return it.Next(), it.Err()
}

func (s *StripeSource) getOldestRecordTime(ctx context.Context, tableName string, accountCreated time.Time) time.Time {
	start := accountCreated
	end := time.Now()

	for end.Sub(start) > 24*time.Hour {
		mid := start.Add(end.Sub(start) / 2)
		hasRecords, err := s.hasRecordsInRange(ctx, tableName, start, mid)
		if err != nil {
			config.Debug("[STRIPE] Error during oldest record search for %s, using account creation time: %v", tableName, err)
			return accountCreated
		}
		if hasRecords {
			end = mid
		} else {
			start = mid
		}
	}

	config.Debug("[STRIPE] Oldest record for %s found near %s", tableName, start.Format(time.RFC3339))
	return start
}

func (s *StripeSource) read(ctx context.Context, table string, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	tableName, mode := parseTableName(table)

	if _, ok := tables[tableName]; !ok {
		supported := make([]string, 0, len(tables))
		for t := range tables {
			supported = append(supported, t)
		}
		return nil, fmt.Errorf("unsupported table: %s (supported: %v)", tableName, supported)
	}

	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		if tc := tables[tableName]; tc.eventTypeFilter != "" && mode == modeAsync && !opts.FullRefresh {
			eventTypeFilter := tc.eventTypeFilter
			intervalStart := opts.IntervalStart
			if intervalStart == nil {
				defaultStart := time.Now().Add(-(30*24*time.Hour - 2*time.Minute)) // 2-min buffer prevents race with the >30-day check below
				intervalStart = &defaultStart
			}

			if hoursSince := time.Since(*intervalStart).Hours(); hoursSince > 30*24 {
				output.Warnf("Warning: interval-start is %.0f days ago, but the Stripe Events API only retains 30 days of history. Falling back to sync incremental mode. To use the events endpoint, set interval-start to within the last 30 days.\n", hoursSince/24)
				mode = modeSyncIncremental
			} else {
				intervalEnd := opts.IntervalEnd
				config.Debug("[STRIPE] Using events-based incremental for %s", tableName)
				if err := s.readTableFromEvents(ctx, tableName, eventTypeFilter, opts, intervalStart, intervalEnd, results); err != nil {
					results <- source.RecordBatchResult{Err: err}
				}
				return
			}
		}

		if !tables[tableName].noDateFilter {
			var start, end time.Time
			useParallel := false

			switch mode {
			case modeSyncIncremental:
				if opts.IntervalStart != nil {
					start = *opts.IntervalStart
				} else {
					acc, err := s.getAccount(ctx)
					if err != nil {
						results <- source.RecordBatchResult{Err: fmt.Errorf("failed to fetch account for time range: %w", err)}
						return
					}
					start = s.getOldestRecordTime(ctx, tableName, time.Unix(acc.Created, 0))
				}
				if opts.IntervalEnd != nil {
					end = *opts.IntervalEnd
				} else {
					end = time.Now()
				}
				useParallel = true
			case modeAsync:
				acc, err := s.getAccount(ctx)
				if err != nil {
					results <- source.RecordBatchResult{Err: fmt.Errorf("failed to fetch account for time range: %w", err)}
					return
				}
				start = s.getOldestRecordTime(ctx, tableName, time.Unix(acc.Created, 0))
				end = time.Now()
				useParallel = true
			}

			if useParallel {
				if err := s.readParallelAdaptive(ctx, tableName, opts, start, end, results); err != nil {
					results <- source.RecordBatchResult{Err: err}
				}
				return
			}
		}

		var intervalStart, intervalEnd *time.Time
		if mode == modeSyncIncremental {
			intervalStart = opts.IntervalStart
			intervalEnd = opts.IntervalEnd
		}

		err := s.readTable(ctx, tableName, opts, intervalStart, intervalEnd, results)
		if err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *StripeSource) readParallelAdaptive(ctx context.Context, tableName string, opts source.ReadOptions, start, end time.Time, results chan<- source.RecordBatchResult) error {
	workers := opts.Parallelism
	if workers <= 0 {
		workers = defaultSyncParallelism
	}

	chunkSize := chunkSizeForParallelism(end.Sub(start), workers)
	chunks := chunkTimeRange(start, end, chunkSize)
	if len(chunks) == 0 {
		return nil
	}
	if workers > len(chunks) {
		workers = len(chunks)
	}

	config.Debug("[STRIPE]  Parallel sync for %s: %d chunks of %s across %d workers from %s to %s",
		tableName, len(chunks), chunkSize, workers, start.Format(time.RFC3339), end.Format(time.RFC3339))

	workerCtx, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()

	chunkCh := make(chan timeWindow)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for window := range chunkCh {
				wStart := window.start
				wEnd := window.end
				config.Debug("[STRIPE] Worker %d: %s [%s, %s]",
					idx, tableName, wStart.Format(time.RFC3339), wEnd.Format(time.RFC3339))
				if err := s.readTable(workerCtx, tableName, opts, &wStart, &wEnd, results); err != nil {
					cancelWorkers()
					results <- source.RecordBatchResult{Err: err}
					return
				}
			}
		}(i)
	}

	go func() {
		defer close(chunkCh)
		for _, c := range chunks {
			select {
			case <-workerCtx.Done():
				return
			case chunkCh <- c:
			}
		}
	}()

	wg.Wait()
	return nil
}

func (s *StripeSource) readTableFromEvents(ctx context.Context, tableName, eventTypeFilter string, opts source.ReadOptions, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	tc := tables[tableName]
	config.Debug("[STRIPE] Reading %s from events (type filter: %s)", tableName, eventTypeFilter)

	fetchCtx, cancelFetch := context.WithCancel(ctx)
	defer cancelFetch()

	params := &stripe.EventListParams{}
	params.Context = fetchCtx
	params.Limit = stripe.Int64(int64(defaultBatchSize))
	params.Type = stripe.String(eventTypeFilter)
	params.CreatedRange = &stripe.RangeQueryParams{
		GreaterThanOrEqual: intervalStart.Unix(),
	}
	if intervalEnd != nil {
		params.CreatedRange.LesserThanOrEqual = intervalEnd.Unix()
	}

	workers := stripeFanoutWorkers(opts.Parallelism)
	objectIDs := make(chan string, workers)
	objects := make(chan map[string]interface{}, workers)
	eventErrCh := make(chan error, 1)
	changedCountCh := make(chan int, 1)
	var workersWG sync.WaitGroup

	for i := 0; i < workers; i++ {
		workersWG.Add(1)
		go func() {
			defer workersWG.Done()
			for id := range objectIDs {
				obj, err := s.fetchObjectByID(fetchCtx, tableName, id)
				if err != nil {
					config.Debug("[STRIPE] Failed to fetch %s %s: %v (skipping)", tableName, id, err)
					continue
				}
				select {
				case objects <- obj:
				case <-fetchCtx.Done():
					return
				}
			}
		}()
	}

	go func() {
		seen := make(map[string]struct{})
		iter := event.List(params)
		for iter.Next() {
			e := iter.Event()
			if e.Data == nil || e.Data.Object == nil {
				continue
			}

			obj := e.Data.Object
			var parentID string
			objType, _ := obj["object"].(string)
			if objType == tc.objectType {
				parentID, _ = obj["id"].(string)
			} else if tc.parentIDField != "" {
				parentID, _ = obj[tc.parentIDField].(string)
			}
			if parentID == "" {
				continue
			}
			if _, exists := seen[parentID]; exists {
				continue
			}
			seen[parentID] = struct{}{}

			select {
			case objectIDs <- parentID:
			case <-fetchCtx.Done():
				close(objectIDs)
				workersWG.Wait()
				changedCountCh <- len(seen)
				close(objects)
				return
			}
		}

		if err := iter.Err(); err != nil && fetchCtx.Err() == nil {
			eventErrCh <- fmt.Errorf("failed to fetch events for %s: %w", tableName, err)
		}
		close(objectIDs)
		workersWG.Wait()
		changedCountCh <- len(seen)
		close(objects)
	}()

	var items []map[string]interface{}
	batchNum := 0
	totalSent := 0
	batchSize := stripePageSize(opts.PageSize)

	flush := func() error {
		if len(items) == 0 {
			return nil
		}
		record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert %s to Arrow: %w", tableName, err)
		}

		batchNum++
		config.Debug("[STRIPE] Sending batch %d with %d %s (total sent: %d)", batchNum, len(items), tableName, totalSent)
		select {
		case results <- source.RecordBatchResult{Batch: record}:
			items = nil
			return nil
		case <-fetchCtx.Done():
			record.Release()
			return fetchCtx.Err()
		}
	}

	for obj := range objects {
		items = append(items, obj)
		totalSent++

		reachedLimit := opts.Limit > 0 && totalSent >= opts.Limit
		if len(items) >= batchSize || reachedLimit {
			if err := flush(); err != nil {
				cancelFetch()
				return err
			}
		}
		if reachedLimit {
			cancelFetch()
			for range objects {
			}
			changedCount := <-changedCountCh
			config.Debug("[STRIPE] Reached limit of %d %s after %d changed IDs", opts.Limit, tableName, changedCount)
			return nil
		}
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	if err := flush(); err != nil {
		return err
	}
	select {
	case err := <-eventErrCh:
		return err
	default:
	}

	changedCount := <-changedCountCh
	if changedCount == 0 {
		config.Debug("[STRIPE] No events found for %s in the given interval", tableName)
		return nil
	}
	config.Debug("[STRIPE] Total %d %s records re-fetched from %d changed IDs", totalSent, tableName, changedCount)
	return nil
}

func (s *StripeSource) fetchObjectByID(ctx context.Context, tableName, id string) (map[string]interface{}, error) {
	switch tableName {
	case "account":
		params := &stripe.AccountParams{}
		params.Context = ctx
		params.AddExpand("external_accounts")
		obj, err := account.GetByID(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "application_fee":
		params := &stripe.ApplicationFeeParams{}
		params.Context = ctx
		params.AddExpand("refunds")
		obj, err := applicationfee.Get(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "charge":
		params := &stripe.ChargeParams{}
		params.Context = ctx
		params.AddExpand("refunds")
		obj, err := charge.Get(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "checkout_session":
		params := &stripe.CheckoutSessionParams{}
		params.Context = ctx
		params.AddExpand("line_items")
		obj, err := session.Get(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "coupon":
		params := &stripe.CouponParams{}
		params.Context = ctx
		obj, err := coupon.Get(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "credit_note":
		params := &stripe.CreditNoteParams{}
		params.Context = ctx
		obj, err := creditnote.Get(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "customer":
		params := &stripe.CustomerParams{}
		params.Context = ctx
		params.AddExpand("tax_ids")
		params.AddExpand("subscriptions")
		params.AddExpand("sources")
		obj, err := customer.Get(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "dispute":
		params := &stripe.DisputeParams{}
		params.Context = ctx
		obj, err := dispute.Get(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "invoice":
		params := &stripe.InvoiceParams{}
		params.Context = ctx
		params.AddExpand("lines")
		obj, err := invoice.Get(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "invoice_item":
		params := &stripe.InvoiceItemParams{}
		params.Context = ctx
		obj, err := invoiceitem.Get(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "payment_intent":
		params := &stripe.PaymentIntentParams{}
		params.Context = ctx
		obj, err := paymentintent.Get(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "payment_link":
		params := &stripe.PaymentLinkParams{}
		params.Context = ctx
		obj, err := paymentlink.Get(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "payment_method":
		params := &stripe.PaymentMethodParams{}
		params.Context = ctx
		obj, err := paymentmethod.Get(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "payout":
		params := &stripe.PayoutParams{}
		params.Context = ctx
		obj, err := payout.Get(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "plan":
		params := &stripe.PlanParams{}
		params.Context = ctx
		obj, err := plan.Get(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "price":
		params := &stripe.PriceParams{}
		params.Context = ctx
		obj, err := price.Get(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "product":
		params := &stripe.ProductParams{}
		params.Context = ctx
		obj, err := product.Get(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "promotion_code":
		params := &stripe.PromotionCodeParams{}
		params.Context = ctx
		obj, err := promotioncode.Get(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "quote":
		params := &stripe.QuoteParams{}
		params.Context = ctx
		obj, err := quote.Get(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "refund":
		params := &stripe.RefundParams{}
		params.Context = ctx
		obj, err := refund.Get(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "review":
		params := &stripe.ReviewParams{}
		params.Context = ctx
		obj, err := review.Get(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "setup_intent":
		params := &stripe.SetupIntentParams{}
		params.Context = ctx
		obj, err := setupintent.Get(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "subscription":
		params := &stripe.SubscriptionParams{}
		params.Context = ctx
		params.AddExpand("items")
		obj, err := subscription.Get(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "subscription_schedule":
		params := &stripe.SubscriptionScheduleParams{}
		params.Context = ctx
		obj, err := subscriptionschedule.Get(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "tax_rate":
		params := &stripe.TaxRateParams{}
		params.Context = ctx
		obj, err := taxrate.Get(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "top_up":
		params := &stripe.TopupParams{}
		params.Context = ctx
		obj, err := topup.Get(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "transfer":
		params := &stripe.TransferParams{}
		params.Context = ctx
		params.AddExpand("reversals")
		obj, err := transfer.Get(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	default:
		return nil, fmt.Errorf("fetchObjectByID not supported for table: %s", tableName)
	}
}

func (s *StripeSource) readTable(ctx context.Context, tableName string, opts source.ReadOptions, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	batchSize := stripePageSize(opts.PageSize)

	config.Debug("[STRIPE] Reading table: %s (batch size: %d)", tableName, batchSize)

	switch tableName {
	case "account":
		return s.readAccount(ctx, opts, results)
	case "apple_pay_domain":
		return s.readApplePayDomains(ctx, opts, batchSize, intervalStart, intervalEnd, results)
	case "application_fee":
		return s.readApplicationFees(ctx, opts, batchSize, intervalStart, intervalEnd, results)
	case "balance_transaction":
		return s.readBalanceTransactions(ctx, opts, batchSize, intervalStart, intervalEnd, results)
	case "charge":
		return s.readCharges(ctx, opts, batchSize, intervalStart, intervalEnd, results)
	case "checkout_session":
		return s.readCheckoutSessions(ctx, opts, batchSize, intervalStart, intervalEnd, results)
	case "coupon":
		return s.readCoupons(ctx, opts, batchSize, intervalStart, intervalEnd, results)
	case "credit_note":
		return s.readCreditNotes(ctx, opts, batchSize, intervalStart, intervalEnd, results)
	case "customer":
		return s.readCustomers(ctx, opts, batchSize, intervalStart, intervalEnd, results)
	case "dispute":
		return s.readDisputes(ctx, opts, batchSize, intervalStart, intervalEnd, results)
	case "event":
		return s.readEvents(ctx, opts, batchSize, intervalStart, intervalEnd, results)
	case "invoice":
		return s.readInvoices(ctx, opts, batchSize, intervalStart, intervalEnd, results)
	case "invoice_item":
		return s.readInvoiceItems(ctx, opts, batchSize, intervalStart, intervalEnd, results)
	case "payment_intent":
		return s.readPaymentIntents(ctx, opts, batchSize, intervalStart, intervalEnd, results)
	case "payment_record":
		return s.readPaymentRecords(ctx, opts, batchSize, intervalStart, intervalEnd, results)
	case "payment_link":
		return s.readPaymentLinks(ctx, opts, batchSize, intervalStart, intervalEnd, results)
	case "payment_method":
		return s.readPaymentMethods(ctx, opts, batchSize, results)
	case "payout":
		return s.readPayouts(ctx, opts, batchSize, intervalStart, intervalEnd, results)
	case "plan":
		return s.readPlans(ctx, opts, batchSize, intervalStart, intervalEnd, results)
	case "price":
		return s.readPrices(ctx, opts, batchSize, intervalStart, intervalEnd, results)
	case "product":
		return s.readProducts(ctx, opts, batchSize, intervalStart, intervalEnd, results)
	case "promotion_code":
		return s.readPromotionCodes(ctx, opts, batchSize, intervalStart, intervalEnd, results)
	case "quote":
		return s.readQuotes(ctx, opts, batchSize, intervalStart, intervalEnd, results)
	case "refund":
		return s.readRefunds(ctx, opts, batchSize, intervalStart, intervalEnd, results)
	case "review":
		return s.readReviews(ctx, opts, batchSize, intervalStart, intervalEnd, results)
	case "setup_attempt":
		return s.readSetupAttempts(ctx, opts, batchSize, intervalStart, intervalEnd, results)
	case "setup_intent":
		return s.readSetupIntents(ctx, opts, batchSize, intervalStart, intervalEnd, results)
	case "shipping_rate":
		return s.readShippingRates(ctx, opts, batchSize, intervalStart, intervalEnd, results)
	case "subscription":
		return s.readSubscriptions(ctx, opts, batchSize, intervalStart, intervalEnd, results)
	case "subscription_item":
		return s.readSubscriptionItems(ctx, opts, batchSize, results)
	case "subscription_schedule":
		return s.readSubscriptionSchedules(ctx, opts, batchSize, intervalStart, intervalEnd, results)
	case "tax_code":
		return s.readTaxCodes(ctx, opts, batchSize, results)
	case "tax_id":
		return s.readTaxIDs(ctx, opts, batchSize, intervalStart, intervalEnd, results)
	case "tax_rate":
		return s.readTaxRates(ctx, opts, batchSize, intervalStart, intervalEnd, results)
	case "top_up":
		return s.readTopUps(ctx, opts, batchSize, intervalStart, intervalEnd, results)
	case "transfer":
		return s.readTransfers(ctx, opts, batchSize, intervalStart, intervalEnd, results)
	case "webhook_endpoint":
		return s.readWebhookEndpoints(ctx, opts, batchSize, results)
	default:
		return fmt.Errorf("unsupported table: %s", tableName)
	}
}

func stripePageSize(requested int) int {
	if requested <= 0 || requested > defaultBatchSize {
		return defaultBatchSize
	}
	return requested
}

func stripeFanoutWorkers(requested int) int {
	if requested <= 0 {
		return defaultSyncParallelism
	}
	if requested > maxFanoutParallelism {
		return maxFanoutParallelism
	}
	return requested
}

func (s *StripeSource) getAccount(ctx context.Context) (*stripe.Account, error) {
	params := &stripe.Params{Context: ctx}
	acc := &stripe.Account{}
	if err := stripe.GetBackend(stripe.APIBackend).Call(http.MethodGet, "/v1/account", s.apiKey, params, acc); err != nil {
		return nil, err
	}
	return acc, nil
}

func (s *StripeSource) readAccount(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching account")
	acc, err := s.getAccount(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch account: %w", err)
	}

	accMap, err := parseRawResponse(acc.LastResponse.RawJSON)
	if err != nil {
		return fmt.Errorf("failed to parse account response: %w", err)
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema([]map[string]interface{}{accMap}, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert account to Arrow: %w", err)
	}

	results <- source.RecordBatchResult{Batch: record}
	config.Debug("[STRIPE] Sent 1 account")
	return nil
}

func (s *StripeSource) readApplePayDomains(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching apple pay domains")

	params := &stripe.ApplePayDomainListParams{}
	params.Context = ctx
	params.Limit = stripe.Int64(int64(batchSize))

	return s.paginateAndSend(ctx, opts, results, "apple_pay_domain", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
		if startingAfter != "" {
			params.StartingAfter = stripe.String(startingAfter)
		}

		iter := applepaydomain.List(params)
		if !iter.Next() {
			return nil, false, "", iter.Err()
		}
		return extractRawListItems(iter.ApplePayDomainList().LastResponse.RawJSON)
	})
}

func (s *StripeSource) readApplicationFees(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching application fees")

	params := &stripe.ApplicationFeeListParams{}
	params.Context = ctx
	params.Limit = stripe.Int64(int64(batchSize))
	params.AddExpand("data.refunds")

	if intervalStart != nil || intervalEnd != nil {
		params.CreatedRange = &stripe.RangeQueryParams{}
		if intervalStart != nil {
			params.CreatedRange.GreaterThanOrEqual = intervalStart.Unix()
		}
		if intervalEnd != nil {
			params.CreatedRange.LesserThanOrEqual = intervalEnd.Unix()
		}
	}

	return s.paginateAndSend(ctx, opts, results, "application_fee", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
		if startingAfter != "" {
			params.StartingAfter = stripe.String(startingAfter)
		}

		iter := applicationfee.List(params)
		if !iter.Next() {
			return nil, false, "", iter.Err()
		}
		return extractRawListItems(iter.ApplicationFeeList().LastResponse.RawJSON)
	})
}

func (s *StripeSource) readBalanceTransactions(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching balance transactions")

	params := &stripe.BalanceTransactionListParams{}
	params.Context = ctx
	params.Limit = stripe.Int64(int64(batchSize))

	if intervalStart != nil || intervalEnd != nil {
		params.CreatedRange = &stripe.RangeQueryParams{}
		if intervalStart != nil {
			params.CreatedRange.GreaterThanOrEqual = intervalStart.Unix()
		}
		if intervalEnd != nil {
			params.CreatedRange.LesserThanOrEqual = intervalEnd.Unix()
		}
	}

	return s.paginateAndSend(ctx, opts, results, "balance_transaction", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
		if startingAfter != "" {
			params.StartingAfter = stripe.String(startingAfter)
		}

		iter := balancetransaction.List(params)
		if !iter.Next() {
			return nil, false, "", iter.Err()
		}
		return extractRawListItems(iter.BalanceTransactionList().LastResponse.RawJSON)
	})
}

func (s *StripeSource) readCharges(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching charges")

	params := &stripe.ChargeListParams{}
	params.Context = ctx
	params.Limit = stripe.Int64(int64(batchSize))
	params.AddExpand("data.refunds")

	if intervalStart != nil || intervalEnd != nil {
		params.CreatedRange = &stripe.RangeQueryParams{}
		if intervalStart != nil {
			params.CreatedRange.GreaterThanOrEqual = intervalStart.Unix()
		}
		if intervalEnd != nil {
			params.CreatedRange.LesserThanOrEqual = intervalEnd.Unix()
		}
	}

	return s.paginateAndSend(ctx, opts, results, "charge", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
		if startingAfter != "" {
			params.StartingAfter = stripe.String(startingAfter)
		}

		iter := charge.List(params)
		if !iter.Next() {
			return nil, false, "", iter.Err()
		}
		return extractRawListItems(iter.ChargeList().LastResponse.RawJSON)
	})
}

func (s *StripeSource) readCheckoutSessions(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching checkout sessions")

	params := &stripe.CheckoutSessionListParams{}
	params.Context = ctx
	params.Limit = stripe.Int64(int64(batchSize))
	params.AddExpand("data.line_items")

	if intervalStart != nil || intervalEnd != nil {
		params.CreatedRange = &stripe.RangeQueryParams{}
		if intervalStart != nil {
			params.CreatedRange.GreaterThanOrEqual = intervalStart.Unix()
		}
		if intervalEnd != nil {
			params.CreatedRange.LesserThanOrEqual = intervalEnd.Unix()
		}
	}

	return s.paginateAndSend(ctx, opts, results, "checkout_session", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
		if startingAfter != "" {
			params.StartingAfter = stripe.String(startingAfter)
		}

		iter := session.List(params)
		if !iter.Next() {
			return nil, false, "", iter.Err()
		}
		return extractRawListItems(iter.CheckoutSessionList().LastResponse.RawJSON)
	})
}

func (s *StripeSource) readCoupons(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching coupons")

	params := &stripe.CouponListParams{}
	params.Context = ctx
	params.Limit = stripe.Int64(int64(batchSize))

	if intervalStart != nil || intervalEnd != nil {
		params.CreatedRange = &stripe.RangeQueryParams{}
		if intervalStart != nil {
			params.CreatedRange.GreaterThanOrEqual = intervalStart.Unix()
		}
		if intervalEnd != nil {
			params.CreatedRange.LesserThanOrEqual = intervalEnd.Unix()
		}
	}

	return s.paginateAndSend(ctx, opts, results, "coupon", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
		if startingAfter != "" {
			params.StartingAfter = stripe.String(startingAfter)
		}

		iter := coupon.List(params)
		if !iter.Next() {
			return nil, false, "", iter.Err()
		}
		return extractRawListItems(iter.CouponList().LastResponse.RawJSON)
	})
}

func (s *StripeSource) readCreditNotes(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching credit notes")

	params := &stripe.CreditNoteListParams{}
	params.Context = ctx
	params.Limit = stripe.Int64(int64(batchSize))

	return s.paginateAndSend(ctx, opts, results, "credit_note", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
		if startingAfter != "" {
			params.StartingAfter = stripe.String(startingAfter)
		}

		iter := creditnote.List(params)
		if !iter.Next() {
			return nil, false, "", iter.Err()
		}
		return extractRawListItems(iter.CreditNoteList().LastResponse.RawJSON)
	})
}

func (s *StripeSource) readCustomers(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching customers")

	params := &stripe.CustomerListParams{}
	params.Context = ctx
	params.Limit = stripe.Int64(int64(batchSize))
	params.AddExpand("data.tax_ids")
	params.AddExpand("data.subscriptions")
	params.AddExpand("data.sources")

	if intervalStart != nil || intervalEnd != nil {
		params.CreatedRange = &stripe.RangeQueryParams{}
		if intervalStart != nil {
			params.CreatedRange.GreaterThanOrEqual = intervalStart.Unix()
		}
		if intervalEnd != nil {
			params.CreatedRange.LesserThanOrEqual = intervalEnd.Unix()
		}
	}

	return s.paginateAndSend(ctx, opts, results, "customer", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
		if startingAfter != "" {
			params.StartingAfter = stripe.String(startingAfter)
		}

		iter := customer.List(params)
		if !iter.Next() {
			return nil, false, "", iter.Err()
		}
		return extractRawListItems(iter.CustomerList().LastResponse.RawJSON)
	})
}

func (s *StripeSource) readDisputes(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching disputes")

	params := &stripe.DisputeListParams{}
	params.Context = ctx
	params.Limit = stripe.Int64(int64(batchSize))

	if intervalStart != nil || intervalEnd != nil {
		params.CreatedRange = &stripe.RangeQueryParams{}
		if intervalStart != nil {
			params.CreatedRange.GreaterThanOrEqual = intervalStart.Unix()
		}
		if intervalEnd != nil {
			params.CreatedRange.LesserThanOrEqual = intervalEnd.Unix()
		}
	}

	return s.paginateAndSend(ctx, opts, results, "dispute", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
		if startingAfter != "" {
			params.StartingAfter = stripe.String(startingAfter)
		}

		iter := dispute.List(params)
		if !iter.Next() {
			return nil, false, "", iter.Err()
		}
		return extractRawListItems(iter.DisputeList().LastResponse.RawJSON)
	})
}

func (s *StripeSource) readEvents(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching events")

	params := &stripe.EventListParams{}
	params.Context = ctx
	params.Limit = stripe.Int64(int64(batchSize))

	if intervalStart != nil || intervalEnd != nil {
		params.CreatedRange = &stripe.RangeQueryParams{}
		if intervalStart != nil {
			params.CreatedRange.GreaterThanOrEqual = intervalStart.Unix()
		}
		if intervalEnd != nil {
			params.CreatedRange.LesserThanOrEqual = intervalEnd.Unix()
		}
	}

	return s.paginateAndSend(ctx, opts, results, "event", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
		if startingAfter != "" {
			params.StartingAfter = stripe.String(startingAfter)
		}

		iter := event.List(params)
		if !iter.Next() {
			return nil, false, "", iter.Err()
		}
		return extractRawListItems(iter.EventList().LastResponse.RawJSON)
	})
}

func (s *StripeSource) readInvoices(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching invoices")

	params := &stripe.InvoiceListParams{}
	params.Context = ctx
	params.Limit = stripe.Int64(int64(batchSize))
	params.AddExpand("data.lines")

	if intervalStart != nil || intervalEnd != nil {
		params.CreatedRange = &stripe.RangeQueryParams{}
		if intervalStart != nil {
			params.CreatedRange.GreaterThanOrEqual = intervalStart.Unix()
		}
		if intervalEnd != nil {
			params.CreatedRange.LesserThanOrEqual = intervalEnd.Unix()
		}
	}

	return s.paginateAndSend(ctx, opts, results, "invoice", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
		if startingAfter != "" {
			params.StartingAfter = stripe.String(startingAfter)
		}

		iter := invoice.List(params)
		if !iter.Next() {
			return nil, false, "", iter.Err()
		}
		return extractRawListItems(iter.InvoiceList().LastResponse.RawJSON)
	})
}

func (s *StripeSource) readInvoiceItems(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching invoice items")

	params := &stripe.InvoiceItemListParams{}
	params.Context = ctx
	params.Limit = stripe.Int64(int64(batchSize))

	if intervalStart != nil || intervalEnd != nil {
		params.CreatedRange = &stripe.RangeQueryParams{}
		if intervalStart != nil {
			params.CreatedRange.GreaterThanOrEqual = intervalStart.Unix()
		}
		if intervalEnd != nil {
			params.CreatedRange.LesserThanOrEqual = intervalEnd.Unix()
		}
	}

	return s.paginateAndSend(ctx, opts, results, "invoice_item", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
		if startingAfter != "" {
			params.StartingAfter = stripe.String(startingAfter)
		}

		iter := invoiceitem.List(params)
		if !iter.Next() {
			return nil, false, "", iter.Err()
		}
		return extractRawListItems(iter.InvoiceItemList().LastResponse.RawJSON)
	})
}

func (s *StripeSource) readPaymentIntents(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching payment intents")

	params := &stripe.PaymentIntentListParams{}
	params.Context = ctx
	params.Limit = stripe.Int64(int64(batchSize))

	if intervalStart != nil || intervalEnd != nil {
		params.CreatedRange = &stripe.RangeQueryParams{}
		if intervalStart != nil {
			params.CreatedRange.GreaterThanOrEqual = intervalStart.Unix()
		}
		if intervalEnd != nil {
			params.CreatedRange.LesserThanOrEqual = intervalEnd.Unix()
		}
	}

	return s.paginateAndSend(ctx, opts, results, "payment_intent", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
		if startingAfter != "" {
			params.StartingAfter = stripe.String(startingAfter)
		}

		iter := paymentintent.List(params)
		if !iter.Next() {
			return nil, false, "", iter.Err()
		}
		return extractRawListItems(iter.PaymentIntentList().LastResponse.RawJSON)
	})
}

// readPaymentRecords lists PaymentIntents in the interval and concurrently
// fetches the corresponding PaymentRecord via /v1/payment_records/{id} for
// each one. PaymentIntents without an associated PaymentRecord (e.g.,
// orchestration not enabled) are skipped.
func (s *StripeSource) readPaymentRecords(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching payment records via payment intents")

	fetchCtx, cancelFetch := context.WithCancel(ctx)
	defer cancelFetch()

	piParams := &stripe.PaymentIntentListParams{}
	piParams.Context = fetchCtx
	piParams.Limit = stripe.Int64(int64(batchSize))

	if intervalStart != nil || intervalEnd != nil {
		piParams.CreatedRange = &stripe.RangeQueryParams{}
		if intervalStart != nil {
			piParams.CreatedRange.GreaterThanOrEqual = intervalStart.Unix()
		}
		if intervalEnd != nil {
			piParams.CreatedRange.LesserThanOrEqual = intervalEnd.Unix()
		}
	}

	const fetchWorkers = 5
	objChan := make(chan map[string]interface{}, fetchWorkers)
	sem := make(chan struct{}, fetchWorkers)
	errChan := make(chan error, 1)
	var wg sync.WaitGroup

	go func() {
		defer func() {
			wg.Wait()
			close(objChan)
		}()

		iter := paymentintent.List(piParams)
		for iter.Next() {
			select {
			case <-fetchCtx.Done():
				return
			case sem <- struct{}{}:
			}

			pi := iter.PaymentIntent()
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				defer func() { <-sem }()

				pr, err := fetchPaymentRecord(fetchCtx, id)
				if err != nil {
					if fetchCtx.Err() == nil {
						config.Debug("[STRIPE] Skipping payment_record for payment_intent %s: %v", id, err)
					}
					return
				}

				select {
				case objChan <- pr:
				case <-fetchCtx.Done():
				}
			}(pi.ID)
		}

		if err := iter.Err(); err != nil && fetchCtx.Err() == nil {
			select {
			case errChan <- fmt.Errorf("failed to list payment_intents for payment_record: %w", err):
			default:
			}
		}
	}()

	var items []map[string]interface{}
	batchNum := 0
	totalSent := 0

	flush := func() error {
		if len(items) == 0 {
			return nil
		}
		record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert payment_record to Arrow: %w", err)
		}
		batchNum++
		totalSent += len(items)
		config.Debug("[STRIPE] Sending batch %d with %d payment_record (total sent: %d)", batchNum, len(items), totalSent)
		results <- source.RecordBatchResult{Batch: record}
		items = nil
		return nil
	}

	for obj := range objChan {
		items = append(items, obj)

		if len(items) >= batchSize {
			if err := flush(); err != nil {
				cancelFetch()
				return err
			}
			if opts.Limit > 0 && totalSent >= opts.Limit {
				config.Debug("[STRIPE] Reached limit of %d payment_record", opts.Limit)
				cancelFetch()
				return nil
			}
		}
	}

	if err := flush(); err != nil {
		return err
	}

	select {
	case err := <-errChan:
		return err
	default:
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	if totalSent == 0 {
		config.Debug("[STRIPE] No payment_record found")
	}
	return nil
}

func fetchPaymentRecord(ctx context.Context, id string) (map[string]interface{}, error) {
	path := fmt.Sprintf("/v1/payment_records/%s", id)
	params := &stripe.RawParams{Params: stripe.Params{Context: ctx}}
	resp, err := stripe.RawRequest(http.MethodGet, path, "", params)
	if err != nil {
		return nil, err
	}
	return parseRawResponse(resp.RawJSON)
}

func (s *StripeSource) readPaymentLinks(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching payment links")

	params := &stripe.PaymentLinkListParams{}
	params.Context = ctx
	params.Limit = stripe.Int64(int64(batchSize))

	return s.paginateAndSend(ctx, opts, results, "payment_link", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
		if startingAfter != "" {
			params.StartingAfter = stripe.String(startingAfter)
		}

		iter := paymentlink.List(params)
		if !iter.Next() {
			return nil, false, "", iter.Err()
		}
		return extractRawListItems(iter.PaymentLinkList().LastResponse.RawJSON)
	})
}

func (s *StripeSource) readPaymentMethods(ctx context.Context, opts source.ReadOptions, batchSize int, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching payment methods")

	workerCtx, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()

	customerParams := &stripe.CustomerListParams{}
	customerParams.Context = workerCtx
	customerParams.Limit = stripe.Int64(int64(batchSize))

	workers := stripeFanoutWorkers(opts.Parallelism)
	customerIDs := make(chan string, workers)
	errCh := make(chan error, 1)
	rowLimit := newStripeRowLimit(opts.Limit)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for customerID := range customerIDs {
				pmParams := &stripe.PaymentMethodListParams{Customer: stripe.String(customerID)}
				pmParams.Context = workerCtx
				pmParams.Limit = stripe.Int64(int64(batchSize))

				err := s.paginateAndSendWithRowLimit(workerCtx, opts, results, "payment_method", rowLimit, func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
					if startingAfter != "" {
						pmParams.StartingAfter = stripe.String(startingAfter)
					}
					iter := paymentmethod.List(pmParams)
					if !iter.Next() {
						return nil, false, "", iter.Err()
					}
					return extractRawListItems(iter.PaymentMethodList().LastResponse.RawJSON)
				})
				if err != nil {
					select {
					case errCh <- fmt.Errorf("failed to fetch payment methods for customer %s: %w", customerID, err):
						cancelWorkers()
					default:
					}
					return
				}
			}
		}()
	}

	customerIter := customer.List(customerParams)
customerLoop:
	for !rowLimit.exhausted() && customerIter.Next() {
		select {
		case <-workerCtx.Done():
			break customerLoop
		case customerIDs <- customerIter.Customer().ID:
		}
	}
	close(customerIDs)
	wg.Wait()

	if err := customerIter.Err(); err != nil && workerCtx.Err() == nil {
		return fmt.Errorf("failed to list customers for payment methods: %w", err)
	}

	select {
	case err := <-errCh:
		return err
	default:
		return ctx.Err()
	}
}

func (s *StripeSource) readPayouts(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching payouts")

	params := &stripe.PayoutListParams{}
	params.Context = ctx
	params.Limit = stripe.Int64(int64(batchSize))

	if intervalStart != nil || intervalEnd != nil {
		params.CreatedRange = &stripe.RangeQueryParams{}
		if intervalStart != nil {
			params.CreatedRange.GreaterThanOrEqual = intervalStart.Unix()
		}
		if intervalEnd != nil {
			params.CreatedRange.LesserThanOrEqual = intervalEnd.Unix()
		}
	}

	return s.paginateAndSend(ctx, opts, results, "payout", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
		if startingAfter != "" {
			params.StartingAfter = stripe.String(startingAfter)
		}

		iter := payout.List(params)
		if !iter.Next() {
			return nil, false, "", iter.Err()
		}
		return extractRawListItems(iter.PayoutList().LastResponse.RawJSON)
	})
}

func (s *StripeSource) readPlans(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching plans")

	params := &stripe.PlanListParams{}
	params.Context = ctx
	params.Limit = stripe.Int64(int64(batchSize))

	if intervalStart != nil || intervalEnd != nil {
		params.CreatedRange = &stripe.RangeQueryParams{}
		if intervalStart != nil {
			params.CreatedRange.GreaterThanOrEqual = intervalStart.Unix()
		}
		if intervalEnd != nil {
			params.CreatedRange.LesserThanOrEqual = intervalEnd.Unix()
		}
	}

	return s.paginateAndSend(ctx, opts, results, "plan", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
		if startingAfter != "" {
			params.StartingAfter = stripe.String(startingAfter)
		}

		iter := plan.List(params)
		if !iter.Next() {
			return nil, false, "", iter.Err()
		}
		return extractRawListItems(iter.PlanList().LastResponse.RawJSON)
	})
}

func (s *StripeSource) readPrices(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching prices")

	params := &stripe.PriceListParams{}
	params.Context = ctx
	params.Limit = stripe.Int64(int64(batchSize))

	if intervalStart != nil || intervalEnd != nil {
		params.CreatedRange = &stripe.RangeQueryParams{}
		if intervalStart != nil {
			params.CreatedRange.GreaterThanOrEqual = intervalStart.Unix()
		}
		if intervalEnd != nil {
			params.CreatedRange.LesserThanOrEqual = intervalEnd.Unix()
		}
	}

	return s.paginateAndSend(ctx, opts, results, "price", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
		if startingAfter != "" {
			params.StartingAfter = stripe.String(startingAfter)
		}

		iter := price.List(params)
		if !iter.Next() {
			return nil, false, "", iter.Err()
		}
		return extractRawListItems(iter.PriceList().LastResponse.RawJSON)
	})
}

func (s *StripeSource) readProducts(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching products")

	params := &stripe.ProductListParams{}
	params.Context = ctx
	params.Limit = stripe.Int64(int64(batchSize))

	if intervalStart != nil || intervalEnd != nil {
		params.CreatedRange = &stripe.RangeQueryParams{}
		if intervalStart != nil {
			params.CreatedRange.GreaterThanOrEqual = intervalStart.Unix()
		}
		if intervalEnd != nil {
			params.CreatedRange.LesserThanOrEqual = intervalEnd.Unix()
		}
	}

	return s.paginateAndSend(ctx, opts, results, "product", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
		if startingAfter != "" {
			params.StartingAfter = stripe.String(startingAfter)
		}

		iter := product.List(params)
		if !iter.Next() {
			return nil, false, "", iter.Err()
		}
		return extractRawListItems(iter.ProductList().LastResponse.RawJSON)
	})
}

func (s *StripeSource) readPromotionCodes(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching promotion codes")

	params := &stripe.PromotionCodeListParams{}
	params.Context = ctx
	params.Limit = stripe.Int64(int64(batchSize))

	if intervalStart != nil || intervalEnd != nil {
		params.CreatedRange = &stripe.RangeQueryParams{}
		if intervalStart != nil {
			params.CreatedRange.GreaterThanOrEqual = intervalStart.Unix()
		}
		if intervalEnd != nil {
			params.CreatedRange.LesserThanOrEqual = intervalEnd.Unix()
		}
	}

	return s.paginateAndSend(ctx, opts, results, "promotion_code", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
		if startingAfter != "" {
			params.StartingAfter = stripe.String(startingAfter)
		}

		iter := promotioncode.List(params)
		if !iter.Next() {
			return nil, false, "", iter.Err()
		}
		return extractRawListItems(iter.PromotionCodeList().LastResponse.RawJSON)
	})
}

func (s *StripeSource) readQuotes(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching quotes")

	params := &stripe.QuoteListParams{}
	params.Context = ctx
	params.Limit = stripe.Int64(int64(batchSize))

	return s.paginateAndSend(ctx, opts, results, "quote", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
		if startingAfter != "" {
			params.StartingAfter = stripe.String(startingAfter)
		}

		iter := quote.List(params)
		if !iter.Next() {
			return nil, false, "", iter.Err()
		}
		return extractRawListItems(iter.QuoteList().LastResponse.RawJSON)
	})
}

func (s *StripeSource) readRefunds(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching refunds")

	params := &stripe.RefundListParams{}
	params.Context = ctx
	params.Limit = stripe.Int64(int64(batchSize))

	if intervalStart != nil || intervalEnd != nil {
		params.CreatedRange = &stripe.RangeQueryParams{}
		if intervalStart != nil {
			params.CreatedRange.GreaterThanOrEqual = intervalStart.Unix()
		}
		if intervalEnd != nil {
			params.CreatedRange.LesserThanOrEqual = intervalEnd.Unix()
		}
	}

	return s.paginateAndSend(ctx, opts, results, "refund", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
		if startingAfter != "" {
			params.StartingAfter = stripe.String(startingAfter)
		}

		iter := refund.List(params)
		if !iter.Next() {
			return nil, false, "", iter.Err()
		}
		return extractRawListItems(iter.RefundList().LastResponse.RawJSON)
	})
}

func (s *StripeSource) readReviews(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching reviews")

	params := &stripe.ReviewListParams{}
	params.Context = ctx
	params.Limit = stripe.Int64(int64(batchSize))

	if intervalStart != nil || intervalEnd != nil {
		params.CreatedRange = &stripe.RangeQueryParams{}
		if intervalStart != nil {
			params.CreatedRange.GreaterThanOrEqual = intervalStart.Unix()
		}
		if intervalEnd != nil {
			params.CreatedRange.LesserThanOrEqual = intervalEnd.Unix()
		}
	}

	return s.paginateAndSend(ctx, opts, results, "review", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
		if startingAfter != "" {
			params.StartingAfter = stripe.String(startingAfter)
		}

		iter := review.List(params)
		if !iter.Next() {
			return nil, false, "", iter.Err()
		}
		return extractRawListItems(iter.ReviewList().LastResponse.RawJSON)
	})
}

func (s *StripeSource) readSetupAttempts(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching setup attempts")

	workerCtx, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()

	siParams := &stripe.SetupIntentListParams{}
	siParams.Context = workerCtx
	siParams.Limit = stripe.Int64(int64(batchSize))

	workers := stripeFanoutWorkers(opts.Parallelism)
	setupIntentIDs := make(chan string, workers)
	errCh := make(chan error, 1)
	rowLimit := newStripeRowLimit(opts.Limit)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for setupIntentID := range setupIntentIDs {
				saParams := &stripe.SetupAttemptListParams{SetupIntent: stripe.String(setupIntentID)}
				saParams.Context = workerCtx
				saParams.Limit = stripe.Int64(int64(batchSize))
				if intervalStart != nil || intervalEnd != nil {
					saParams.CreatedRange = &stripe.RangeQueryParams{}
					if intervalStart != nil {
						saParams.CreatedRange.GreaterThanOrEqual = intervalStart.Unix()
					}
					if intervalEnd != nil {
						saParams.CreatedRange.LesserThanOrEqual = intervalEnd.Unix()
					}
				}

				err := s.paginateAndSendWithRowLimit(workerCtx, opts, results, "setup_attempt", rowLimit, func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
					if startingAfter != "" {
						saParams.StartingAfter = stripe.String(startingAfter)
					}
					iter := setupattempt.List(saParams)
					if !iter.Next() {
						return nil, false, "", iter.Err()
					}
					return extractRawListItems(iter.SetupAttemptList().LastResponse.RawJSON)
				})
				if err != nil {
					select {
					case errCh <- fmt.Errorf("failed to fetch setup attempts for setup intent %s: %w", setupIntentID, err):
						cancelWorkers()
					default:
					}
					return
				}
			}
		}()
	}

	siIter := setupintent.List(siParams)
setupIntentLoop:
	for !rowLimit.exhausted() && siIter.Next() {
		select {
		case <-workerCtx.Done():
			break setupIntentLoop
		case setupIntentIDs <- siIter.SetupIntent().ID:
		}
	}
	close(setupIntentIDs)
	wg.Wait()

	if err := siIter.Err(); err != nil && workerCtx.Err() == nil {
		return fmt.Errorf("failed to list setup intents for setup attempts: %w", err)
	}

	select {
	case err := <-errCh:
		return err
	default:
		return ctx.Err()
	}
}

func (s *StripeSource) readSetupIntents(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching setup intents")

	params := &stripe.SetupIntentListParams{}
	params.Context = ctx
	params.Limit = stripe.Int64(int64(batchSize))

	if intervalStart != nil || intervalEnd != nil {
		params.CreatedRange = &stripe.RangeQueryParams{}
		if intervalStart != nil {
			params.CreatedRange.GreaterThanOrEqual = intervalStart.Unix()
		}
		if intervalEnd != nil {
			params.CreatedRange.LesserThanOrEqual = intervalEnd.Unix()
		}
	}

	return s.paginateAndSend(ctx, opts, results, "setup_intent", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
		if startingAfter != "" {
			params.StartingAfter = stripe.String(startingAfter)
		}

		iter := setupintent.List(params)
		if !iter.Next() {
			return nil, false, "", iter.Err()
		}
		return extractRawListItems(iter.SetupIntentList().LastResponse.RawJSON)
	})
}

func (s *StripeSource) readShippingRates(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching shipping rates")

	params := &stripe.ShippingRateListParams{}
	params.Context = ctx
	params.Limit = stripe.Int64(int64(batchSize))

	if intervalStart != nil || intervalEnd != nil {
		params.CreatedRange = &stripe.RangeQueryParams{}
		if intervalStart != nil {
			params.CreatedRange.GreaterThanOrEqual = intervalStart.Unix()
		}
		if intervalEnd != nil {
			params.CreatedRange.LesserThanOrEqual = intervalEnd.Unix()
		}
	}

	return s.paginateAndSend(ctx, opts, results, "shipping_rate", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
		if startingAfter != "" {
			params.StartingAfter = stripe.String(startingAfter)
		}

		iter := shippingrate.List(params)
		if !iter.Next() {
			return nil, false, "", iter.Err()
		}
		return extractRawListItems(iter.ShippingRateList().LastResponse.RawJSON)
	})
}

func (s *StripeSource) readSubscriptions(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching subscriptions")

	params := &stripe.SubscriptionListParams{}
	params.Context = ctx
	params.Limit = stripe.Int64(int64(batchSize))
	params.Status = stripe.String("all") // Include canceled, incomplete_expired, etc.
	params.AddExpand("data.items")

	if intervalStart != nil || intervalEnd != nil {
		params.CreatedRange = &stripe.RangeQueryParams{}
		if intervalStart != nil {
			params.CreatedRange.GreaterThanOrEqual = intervalStart.Unix()
		}
		if intervalEnd != nil {
			params.CreatedRange.LesserThanOrEqual = intervalEnd.Unix()
		}
	}

	return s.paginateAndSend(ctx, opts, results, "subscription", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
		if startingAfter != "" {
			params.StartingAfter = stripe.String(startingAfter)
		}

		iter := subscription.List(params)
		if !iter.Next() {
			return nil, false, "", iter.Err()
		}
		return extractRawListItems(iter.SubscriptionList().LastResponse.RawJSON)
	})
}

func (s *StripeSource) readSubscriptionItems(ctx context.Context, opts source.ReadOptions, batchSize int, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching subscription items")
	if batchSize <= 0 || batchSize > defaultBatchSize {
		batchSize = defaultBatchSize
	}

	subParams := &stripe.SubscriptionListParams{}
	subParams.Context = ctx
	subParams.Limit = stripe.Int64(int64(batchSize))
	subParams.Status = stripe.String("all")

	parentFetch := func(startingAfter string) (subscriptionItemsPage, error) {
		if startingAfter != "" {
			subParams.StartingAfter = stripe.String(startingAfter)
		}

		iter := subscription.List(subParams)
		if !iter.Next() {
			return subscriptionItemsPage{}, iter.Err()
		}
		return extractRawSubscriptionItems(iter.SubscriptionList().LastResponse.RawJSON)
	}

	overflowFetch := func(subscriptionID, startingAfter string) ([]map[string]interface{}, bool, string, error) {
		siParams := &stripe.SubscriptionItemListParams{
			Subscription: stripe.String(subscriptionID),
		}
		siParams.Context = ctx
		siParams.Limit = stripe.Int64(int64(batchSize))
		if startingAfter != "" {
			siParams.StartingAfter = stripe.String(startingAfter)
		}

		iter := subscriptionitem.List(siParams)
		if !iter.Next() {
			return nil, false, "", iter.Err()
		}
		return extractRawListItems(iter.SubscriptionItemList().LastResponse.RawJSON)
	}

	return readSubscriptionItemsFromPages(ctx, opts, batchSize, results, parentFetch, overflowFetch)
}

type subscriptionItemOverflow struct {
	subscriptionID string
	startingAfter  string
}

type subscriptionItemsPage struct {
	items     []map[string]interface{}
	overflows []subscriptionItemOverflow
	hasMore   bool
	lastID    string
}

type subscriptionItemsPageFetch func(startingAfter string) (subscriptionItemsPage, error)

type subscriptionItemOverflowFetch func(subscriptionID, startingAfter string) ([]map[string]interface{}, bool, string, error)

func readSubscriptionItemsFromPages(
	ctx context.Context,
	opts source.ReadOptions,
	batchSize int,
	results chan<- source.RecordBatchResult,
	parentFetch subscriptionItemsPageFetch,
	overflowFetch subscriptionItemOverflowFetch,
) error {
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}

	var pending []map[string]interface{}
	totalSent := 0
	batchNum := 0

	flush := func() error {
		if len(pending) == 0 {
			return nil
		}

		record, err := arrowconv.ItemsToArrowRecordWithSchema(pending, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert subscription_item to Arrow: %w", err)
		}

		batchNum++
		config.Debug("[STRIPE] Sending batch %d with %d subscription_item (total sent: %d)", batchNum, len(pending), totalSent)
		select {
		case results <- source.RecordBatchResult{Batch: record}:
			pending = nil
			return nil
		case <-ctx.Done():
			record.Release()
			return ctx.Err()
		}
	}

	appendItems := func(items []map[string]interface{}) (bool, error) {
		for _, item := range items {
			if opts.Limit > 0 && totalSent >= opts.Limit {
				return true, flush()
			}

			pending = append(pending, item)
			totalSent++
			if len(pending) >= batchSize {
				if err := flush(); err != nil {
					return false, err
				}
			}
		}

		if opts.Limit > 0 && totalSent >= opts.Limit {
			return true, flush()
		}
		return false, nil
	}

	var parentCursor string
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		page, err := parentFetch(parentCursor)
		if err != nil {
			return fmt.Errorf("failed to fetch subscriptions for subscription items: %w", err)
		}

		reachedLimit, err := appendItems(page.items)
		if err != nil {
			return err
		}
		if reachedLimit {
			config.Debug("[STRIPE] Reached limit of %d subscription_item", opts.Limit)
			return nil
		}

		for _, overflow := range page.overflows {
			itemCursor := overflow.startingAfter
			for {
				items, hasMore, lastID, err := overflowFetch(overflow.subscriptionID, itemCursor)
				if err != nil {
					return fmt.Errorf("failed to fetch subscription items for subscription %s: %w", overflow.subscriptionID, err)
				}

				reachedLimit, err = appendItems(items)
				if err != nil {
					return err
				}
				if reachedLimit {
					config.Debug("[STRIPE] Reached limit of %d subscription_item", opts.Limit)
					return nil
				}
				if !hasMore {
					break
				}
				if lastID == "" {
					return fmt.Errorf("failed to paginate subscription items for subscription %s: missing item cursor", overflow.subscriptionID)
				}
				itemCursor = lastID
			}
		}

		if !page.hasMore {
			break
		}
		if page.lastID == "" {
			return fmt.Errorf("failed to paginate subscriptions for subscription items: missing subscription cursor")
		}
		parentCursor = page.lastID
	}

	if err := flush(); err != nil {
		return err
	}
	if totalSent == 0 {
		config.Debug("[STRIPE] No subscription_item found")
	}
	return nil
}

func (s *StripeSource) readSubscriptionSchedules(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching subscription schedules")

	params := &stripe.SubscriptionScheduleListParams{}
	params.Context = ctx
	params.Limit = stripe.Int64(int64(batchSize))

	if intervalStart != nil || intervalEnd != nil {
		params.CreatedRange = &stripe.RangeQueryParams{}
		if intervalStart != nil {
			params.CreatedRange.GreaterThanOrEqual = intervalStart.Unix()
		}
		if intervalEnd != nil {
			params.CreatedRange.LesserThanOrEqual = intervalEnd.Unix()
		}
	}

	return s.paginateAndSend(ctx, opts, results, "subscription_schedule", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
		if startingAfter != "" {
			params.StartingAfter = stripe.String(startingAfter)
		}

		iter := subscriptionschedule.List(params)
		if !iter.Next() {
			return nil, false, "", iter.Err()
		}
		return extractRawListItems(iter.SubscriptionScheduleList().LastResponse.RawJSON)
	})
}

func (s *StripeSource) readTaxCodes(ctx context.Context, opts source.ReadOptions, batchSize int, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching tax codes")

	params := &stripe.TaxCodeListParams{}
	params.Context = ctx
	params.Limit = stripe.Int64(int64(batchSize))

	return s.paginateAndSend(ctx, opts, results, "tax_code", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
		if startingAfter != "" {
			params.StartingAfter = stripe.String(startingAfter)
		}

		iter := taxcode.List(params)
		if !iter.Next() {
			return nil, false, "", iter.Err()
		}
		return extractRawListItems(iter.TaxCodeList().LastResponse.RawJSON)
	})
}

func (s *StripeSource) readTaxIDs(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching tax IDs")

	customerParams := &stripe.CustomerListParams{}
	customerParams.Context = ctx
	customerParams.Limit = stripe.Int64(int64(batchSize))
	customerParams.AddExpand("data.tax_ids")

	return s.paginateAndSend(ctx, opts, results, "tax_id", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
		if startingAfter != "" {
			customerParams.StartingAfter = stripe.String(startingAfter)
		}

		customerIter := customer.List(customerParams)
		if !customerIter.Next() {
			return nil, false, "", customerIter.Err()
		}
		page, err := extractRawCustomerTaxIDs(customerIter.CustomerList().LastResponse.RawJSON)
		if err != nil {
			return nil, false, "", err
		}

		for _, overflow := range page.overflows {
			cursor := overflow.startingAfter
			for {
				tidParams := &stripe.TaxIDListParams{Customer: stripe.String(overflow.customerID)}
				tidParams.Context = ctx
				tidParams.Limit = stripe.Int64(int64(batchSize))
				if cursor != "" {
					tidParams.StartingAfter = stripe.String(cursor)
				}

				iter := taxid.List(tidParams)
				if !iter.Next() {
					if err := iter.Err(); err != nil {
						return nil, false, "", fmt.Errorf("failed to fetch tax IDs for customer %s: %w", overflow.customerID, err)
					}
					break
				}
				items, hasMore, lastID, err := extractRawListItems(iter.TaxIDList().LastResponse.RawJSON)
				if err != nil {
					return nil, false, "", err
				}
				page.items = append(page.items, items...)
				if !hasMore {
					break
				}
				if lastID == "" {
					return nil, false, "", fmt.Errorf("failed to paginate tax IDs for customer %s: missing cursor", overflow.customerID)
				}
				cursor = lastID
			}
		}

		return page.items, page.hasMore, page.lastID, nil
	})
}

func (s *StripeSource) readTaxRates(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching tax rates")

	params := &stripe.TaxRateListParams{}
	params.Context = ctx
	params.Limit = stripe.Int64(int64(batchSize))

	if intervalStart != nil || intervalEnd != nil {
		params.CreatedRange = &stripe.RangeQueryParams{}
		if intervalStart != nil {
			params.CreatedRange.GreaterThanOrEqual = intervalStart.Unix()
		}
		if intervalEnd != nil {
			params.CreatedRange.LesserThanOrEqual = intervalEnd.Unix()
		}
	}

	return s.paginateAndSend(ctx, opts, results, "tax_rate", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
		if startingAfter != "" {
			params.StartingAfter = stripe.String(startingAfter)
		}

		iter := taxrate.List(params)
		if !iter.Next() {
			return nil, false, "", iter.Err()
		}
		return extractRawListItems(iter.TaxRateList().LastResponse.RawJSON)
	})
}

func (s *StripeSource) readTopUps(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching top ups")

	params := &stripe.TopupListParams{}
	params.Context = ctx
	params.Limit = stripe.Int64(int64(batchSize))

	if intervalStart != nil || intervalEnd != nil {
		params.CreatedRange = &stripe.RangeQueryParams{}
		if intervalStart != nil {
			params.CreatedRange.GreaterThanOrEqual = intervalStart.Unix()
		}
		if intervalEnd != nil {
			params.CreatedRange.LesserThanOrEqual = intervalEnd.Unix()
		}
	}

	return s.paginateAndSend(ctx, opts, results, "top_up", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
		if startingAfter != "" {
			params.StartingAfter = stripe.String(startingAfter)
		}

		iter := topup.List(params)
		if !iter.Next() {
			return nil, false, "", iter.Err()
		}
		return extractRawListItems(iter.TopupList().LastResponse.RawJSON)
	})
}

func (s *StripeSource) readTransfers(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching transfers")

	params := &stripe.TransferListParams{}
	params.Context = ctx
	params.Limit = stripe.Int64(int64(batchSize))
	params.AddExpand("data.reversals")

	if intervalStart != nil || intervalEnd != nil {
		params.CreatedRange = &stripe.RangeQueryParams{}
		if intervalStart != nil {
			params.CreatedRange.GreaterThanOrEqual = intervalStart.Unix()
		}
		if intervalEnd != nil {
			params.CreatedRange.LesserThanOrEqual = intervalEnd.Unix()
		}
	}

	return s.paginateAndSend(ctx, opts, results, "transfer", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
		if startingAfter != "" {
			params.StartingAfter = stripe.String(startingAfter)
		}

		iter := transfer.List(params)
		if !iter.Next() {
			return nil, false, "", iter.Err()
		}
		return extractRawListItems(iter.TransferList().LastResponse.RawJSON)
	})
}

func (s *StripeSource) readWebhookEndpoints(ctx context.Context, opts source.ReadOptions, batchSize int, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching webhook endpoints")

	params := &stripe.WebhookEndpointListParams{}
	params.Context = ctx
	params.Limit = stripe.Int64(int64(batchSize))

	return s.paginateAndSend(ctx, opts, results, "webhook_endpoint", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
		if startingAfter != "" {
			params.StartingAfter = stripe.String(startingAfter)
		}

		iter := webhookendpoint.List(params)
		if !iter.Next() {
			return nil, false, "", iter.Err()
		}
		return extractRawListItems(iter.WebhookEndpointList().LastResponse.RawJSON)
	})
}

type paginationFunc func(startingAfter string) (items []map[string]interface{}, hasMore bool, lastID string, err error)

func (s *StripeSource) paginateAndSend(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult, tableName string, fetch paginationFunc) error {
	return s.paginateAndSendWithRowLimit(ctx, opts, results, tableName, newStripeRowLimit(opts.Limit), fetch)
}

type stripeRowLimit struct {
	mu       sync.Mutex
	limit    int
	reserved int
}

func newStripeRowLimit(limit int) *stripeRowLimit {
	return &stripeRowLimit{limit: limit}
}

func (l *stripeRowLimit) reserve(items []map[string]interface{}) ([]map[string]interface{}, int, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.limit > 0 {
		remaining := l.limit - l.reserved
		if remaining <= 0 {
			return nil, l.reserved, true
		}
		if len(items) > remaining {
			items = items[:remaining]
		}
	}

	l.reserved += len(items)
	return items, l.reserved, l.limit > 0 && l.reserved >= l.limit
}

func (l *stripeRowLimit) exhausted() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.limit > 0 && l.reserved >= l.limit
}

func (s *StripeSource) paginateAndSendWithRowLimit(
	ctx context.Context,
	opts source.ReadOptions,
	results chan<- source.RecordBatchResult,
	tableName string,
	rowLimit *stripeRowLimit,
	fetch paginationFunc,
) error {
	localSent := 0
	batchNum := 0
	var startingAfter string

	for {
		if rowLimit.exhausted() {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		items, hasMore, lastID, err := fetch(startingAfter)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", tableName, err)
		}

		items, totalSent, reachedLimit := rowLimit.reserve(items)

		if len(items) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert %s to Arrow: %w", tableName, err)
			}

			batchNum++
			config.Debug("[STRIPE] Sending batch %d with %d %s (total sent: %d)", batchNum, len(items), tableName, totalSent)
			select {
			case results <- source.RecordBatchResult{Batch: record}:
			case <-ctx.Done():
				record.Release()
				return ctx.Err()
			}
			localSent += len(items)

			if reachedLimit {
				config.Debug("[STRIPE] Reached limit of %d %s", opts.Limit, tableName)
				break
			}
		} else if reachedLimit {
			break
		}

		if !hasMore {
			break
		}
		if lastID == "" {
			return fmt.Errorf("failed to paginate %s: Stripe returned has_more without a cursor", tableName)
		}

		startingAfter = lastID
	}

	if localSent == 0 {
		config.Debug("[STRIPE] No %s found", tableName)
	}

	return nil
}

// parseRawResponse decodes a Stripe API raw JSON response into a map,
// using json.Number to preserve large integer precision.
func parseRawResponse(rawJSON []byte) (map[string]interface{}, error) {
	dec := json.NewDecoder(bytes.NewReader(rawJSON))
	dec.UseNumber()
	var result map[string]interface{}
	if err := dec.Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

// extractRawListItems parses a Stripe list response's raw JSON, returning
// the data items, has_more flag, and the last item's ID for cursor pagination.
func extractRawListItems(rawJSON []byte) (items []map[string]interface{}, hasMore bool, lastID string, err error) {
	result, err := parseRawResponse(rawJSON)
	if err != nil {
		return nil, false, "", err
	}

	hasMore, _ = result["has_more"].(bool)
	data, _ := result["data"].([]interface{})

	items = make([]map[string]interface{}, 0, len(data))
	for _, item := range data {
		if m, ok := item.(map[string]interface{}); ok {
			items = append(items, m)
			if id, ok := m["id"].(string); ok {
				lastID = id
			}
		}
	}
	return items, hasMore, lastID, nil
}

func extractRawSubscriptionItems(rawJSON []byte) (subscriptionItemsPage, error) {
	subscriptions, hasMore, lastID, err := extractRawListItems(rawJSON)
	if err != nil {
		return subscriptionItemsPage{}, err
	}

	page := subscriptionItemsPage{
		hasMore: hasMore,
		lastID:  lastID,
	}
	for _, subscription := range subscriptions {
		subscriptionID, _ := subscription["id"].(string)
		if subscriptionID == "" {
			return subscriptionItemsPage{}, fmt.Errorf("subscription response is missing an id")
		}

		itemList, ok := subscription["items"].(map[string]interface{})
		if !ok {
			page.overflows = append(page.overflows, subscriptionItemOverflow{subscriptionID: subscriptionID})
			continue
		}

		data, dataOK := itemList["data"].([]interface{})
		if !dataOK {
			page.overflows = append(page.overflows, subscriptionItemOverflow{subscriptionID: subscriptionID})
			continue
		}

		var itemCursor string
		for _, rawItem := range data {
			item, ok := rawItem.(map[string]interface{})
			if !ok {
				return subscriptionItemsPage{}, fmt.Errorf("subscription %s contains an invalid item", subscriptionID)
			}
			itemID, _ := item["id"].(string)
			if itemID == "" {
				return subscriptionItemsPage{}, fmt.Errorf("subscription %s contains an item without an id", subscriptionID)
			}
			page.items = append(page.items, item)
			itemCursor = itemID
		}

		itemsHaveMore, hasMoreOK := itemList["has_more"].(bool)
		if itemsHaveMore || !hasMoreOK {
			page.overflows = append(page.overflows, subscriptionItemOverflow{
				subscriptionID: subscriptionID,
				startingAfter:  itemCursor,
			})
		}
	}

	return page, nil
}

type taxIDOverflow struct {
	customerID    string
	startingAfter string
}

type customerTaxIDsPage struct {
	items     []map[string]interface{}
	overflows []taxIDOverflow
	hasMore   bool
	lastID    string
}

func extractRawCustomerTaxIDs(rawJSON []byte) (customerTaxIDsPage, error) {
	customers, hasMore, lastID, err := extractRawListItems(rawJSON)
	if err != nil {
		return customerTaxIDsPage{}, err
	}

	page := customerTaxIDsPage{hasMore: hasMore, lastID: lastID}
	for _, customer := range customers {
		customerID, _ := customer["id"].(string)
		if customerID == "" {
			return customerTaxIDsPage{}, fmt.Errorf("customer response is missing an id")
		}

		taxIDList, ok := customer["tax_ids"].(map[string]interface{})
		if !ok {
			page.overflows = append(page.overflows, taxIDOverflow{customerID: customerID})
			continue
		}

		data, dataOK := taxIDList["data"].([]interface{})
		if !dataOK {
			page.overflows = append(page.overflows, taxIDOverflow{customerID: customerID})
			continue
		}

		var taxIDCursor string
		for _, rawTaxID := range data {
			taxID, ok := rawTaxID.(map[string]interface{})
			if !ok {
				return customerTaxIDsPage{}, fmt.Errorf("customer %s contains an invalid tax ID", customerID)
			}
			taxIDValue, _ := taxID["id"].(string)
			if taxIDValue == "" {
				return customerTaxIDsPage{}, fmt.Errorf("customer %s contains a tax ID without an id", customerID)
			}
			page.items = append(page.items, taxID)
			taxIDCursor = taxIDValue
		}

		taxIDsHaveMore, hasMoreOK := taxIDList["has_more"].(bool)
		if taxIDsHaveMore || !hasMoreOK {
			page.overflows = append(page.overflows, taxIDOverflow{
				customerID:    customerID,
				startingAfter: taxIDCursor,
			})
		}
	}

	return page, nil
}

var _ source.Source = (*StripeSource)(nil)
