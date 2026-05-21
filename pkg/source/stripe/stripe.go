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
)

type loadingMode int

const (
	modeAsync loadingMode = iota
	modeSync
	modeSyncIncremental
)

type StripeSource struct {
	apiKey string
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
	stripe.Key = apiKey
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

// chunkSizeForInterval picks a chunk size that yields ~50-500 chunks for typical intervals.
// Worker count is decoupled from chunk count — workers pull chunks from a queue.
func chunkSizeForInterval(interval time.Duration) time.Duration {
	switch {
	case interval < time.Hour:
		return interval / 10
	case interval < 24*time.Hour:
		return 5 * time.Minute
	case interval < 7*24*time.Hour:
		return time.Hour
	case interval < 90*24*time.Hour:
		return 6 * time.Hour
	default:
		return 24 * time.Hour
	}
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

func (s *StripeSource) hasRecordsInRange(tableName string, start, end time.Time) (bool, error) {
	cr := &stripe.RangeQueryParams{
		GreaterThanOrEqual: start.Unix(),
		LesserThanOrEqual:  end.Unix(),
	}
	lp := stripe.ListParams{Limit: stripe.Int64(1)}

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

func (s *StripeSource) getOldestRecordTime(tableName string, accountCreated time.Time) time.Time {
	start := accountCreated
	end := time.Now()

	for end.Sub(start) > 24*time.Hour {
		mid := start.Add(end.Sub(start) / 2)
		hasRecords, err := s.hasRecordsInRange(tableName, start, mid)
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
				fmt.Printf("Warning: interval-start is %.0f days ago, but the Stripe Events API only retains 30 days of history. Falling back to sync incremental mode. To use the events endpoint, set interval-start to within the last 30 days.\n", hoursSince/24)
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
					acc, err := account.Get()
					if err != nil {
						results <- source.RecordBatchResult{Err: fmt.Errorf("failed to fetch account for time range: %w", err)}
						return
					}
					start = s.getOldestRecordTime(tableName, time.Unix(acc.Created, 0))
				}
				if opts.IntervalEnd != nil {
					end = *opts.IntervalEnd
				} else {
					end = time.Now()
				}
				useParallel = true
			case modeAsync:
				acc, err := account.Get()
				if err != nil {
					results <- source.RecordBatchResult{Err: fmt.Errorf("failed to fetch account for time range: %w", err)}
					return
				}
				start = s.getOldestRecordTime(tableName, time.Unix(acc.Created, 0))
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

	chunkSize := chunkSizeForInterval(end.Sub(start))
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

	params := &stripe.EventListParams{}
	params.Limit = stripe.Int64(int64(defaultBatchSize))
	params.Type = stripe.String(eventTypeFilter)
	params.CreatedRange = &stripe.RangeQueryParams{
		GreaterThanOrEqual: intervalStart.Unix(),
	}
	if intervalEnd != nil {
		params.CreatedRange.LesserThanOrEqual = intervalEnd.Unix()
	}

	// Collect unique parent object IDs from all events
	changedIDs := make(map[string]bool)

	iter := event.List(params)
	for iter.Next() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

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

		if parentID != "" {
			changedIDs[parentID] = true
		}
	}

	if err := iter.Err(); err != nil {
		return fmt.Errorf("failed to fetch events for %s: %w", tableName, err)
	}

	if len(changedIDs) == 0 {
		config.Debug("[STRIPE] No events found for %s in the given interval", tableName)
		return nil
	}

	config.Debug("[STRIPE] Found %d unique %s IDs from events, re-fetching full objects", len(changedIDs), tableName)

	// Re-fetch objects by ID in parallel using a worker pool
	const fetchWorkers = 5
	fetchCtx, cancelFetch := context.WithCancel(ctx)
	defer cancelFetch()

	objChan := make(chan map[string]interface{}, fetchWorkers)
	sem := make(chan struct{}, fetchWorkers)
	var wg sync.WaitGroup

	go func() {
		defer func() {
			wg.Wait()
			close(objChan)
		}()
		for id := range changedIDs {
			select {
			case <-fetchCtx.Done():
				return
			case sem <- struct{}{}:
			}

			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				defer func() { <-sem }()

				select {
				case <-fetchCtx.Done():
					return
				default:
				}

				config.Debug("[STRIPE] Fetching object ID: %s", id)
				obj, err := s.fetchObjectByID(tableName, id)
				if err != nil {
					config.Debug("[STRIPE] Failed to fetch %s %s: %v (skipping)", tableName, id, err)
					return
				}

				select {
				case objChan <- obj:
				case <-fetchCtx.Done():
				}
			}(id)
		}
	}()

	var items []map[string]interface{}
	batchNum := 0
	totalSent := 0

	for obj := range objChan {
		items = append(items, obj)

		if len(items) >= defaultBatchSize {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert %s to Arrow: %w", tableName, err)
			}

			batchNum++
			totalSent += len(items)
			config.Debug("[STRIPE] Sending batch %d with %d %s (total sent: %d)", batchNum, len(items), tableName, totalSent)
			results <- source.RecordBatchResult{Batch: record}
			items = nil

			if opts.Limit > 0 && totalSent >= opts.Limit {
				config.Debug("[STRIPE] Reached limit of %d %s", opts.Limit, tableName)
				return nil
			}
		}
	}

	if len(items) > 0 {
		record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert %s to Arrow: %w", tableName, err)
		}

		batchNum++
		totalSent += len(items)
		config.Debug("[STRIPE] Sending batch %d with %d %s (total sent: %d)", batchNum, len(items), tableName, totalSent)
		results <- source.RecordBatchResult{Batch: record}
	}

	config.Debug("[STRIPE] Total %d %s records re-fetched from %d changed IDs", totalSent, tableName, len(changedIDs))
	return nil
}

func (s *StripeSource) fetchObjectByID(tableName, id string) (map[string]interface{}, error) {
	switch tableName {
	case "account":
		params := &stripe.AccountParams{}
		params.AddExpand("external_accounts")
		obj, err := account.GetByID(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "application_fee":
		params := &stripe.ApplicationFeeParams{}
		params.AddExpand("refunds")
		obj, err := applicationfee.Get(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "charge":
		params := &stripe.ChargeParams{}
		params.AddExpand("refunds")
		obj, err := charge.Get(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "checkout_session":
		params := &stripe.CheckoutSessionParams{}
		params.AddExpand("line_items")
		obj, err := session.Get(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "coupon":
		obj, err := coupon.Get(id, nil)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "credit_note":
		obj, err := creditnote.Get(id, nil)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "customer":
		params := &stripe.CustomerParams{}
		params.AddExpand("tax_ids")
		params.AddExpand("subscriptions")
		params.AddExpand("sources")
		obj, err := customer.Get(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "dispute":
		obj, err := dispute.Get(id, nil)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "invoice":
		params := &stripe.InvoiceParams{}
		params.AddExpand("lines")
		obj, err := invoice.Get(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "invoice_item":
		obj, err := invoiceitem.Get(id, nil)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "payment_intent":
		obj, err := paymentintent.Get(id, nil)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "payment_link":
		obj, err := paymentlink.Get(id, nil)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "payment_method":
		obj, err := paymentmethod.Get(id, nil)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "payout":
		obj, err := payout.Get(id, nil)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "plan":
		obj, err := plan.Get(id, nil)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "price":
		obj, err := price.Get(id, nil)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "product":
		obj, err := product.Get(id, nil)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "promotion_code":
		obj, err := promotioncode.Get(id, nil)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "quote":
		obj, err := quote.Get(id, nil)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "refund":
		obj, err := refund.Get(id, nil)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "review":
		obj, err := review.Get(id, nil)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "setup_intent":
		obj, err := setupintent.Get(id, nil)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "subscription":
		params := &stripe.SubscriptionParams{}
		params.AddExpand("items")
		obj, err := subscription.Get(id, params)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "subscription_schedule":
		obj, err := subscriptionschedule.Get(id, nil)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "tax_rate":
		obj, err := taxrate.Get(id, nil)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "top_up":
		obj, err := topup.Get(id, nil)
		if err != nil {
			return nil, err
		}
		return parseRawResponse(obj.LastResponse.RawJSON)
	case "transfer":
		params := &stripe.TransferParams{}
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
	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}

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

func (s *StripeSource) readAccount(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching account")
	acc, err := account.Get()
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

	customerParams := &stripe.CustomerListParams{}
	customerParams.Limit = stripe.Int64(int64(batchSize))

	customerIter := customer.List(customerParams)
	for customerIter.Next() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		c := customerIter.Customer()

		pmParams := &stripe.PaymentMethodListParams{
			Customer: stripe.String(c.ID),
		}
		pmParams.Limit = stripe.Int64(int64(batchSize))

		err := s.paginateAndSend(ctx, opts, results, "payment_method", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
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
			config.Debug("[STRIPE] Error fetching payment methods for customer %s: %v", c.ID, err)
		}
	}

	if err := customerIter.Err(); err != nil {
		return fmt.Errorf("failed to list customers for payment methods: %w", err)
	}

	return nil
}

func (s *StripeSource) readPayouts(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching payouts")

	params := &stripe.PayoutListParams{}
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

	siParams := &stripe.SetupIntentListParams{}
	siParams.Limit = stripe.Int64(int64(batchSize))

	siIter := setupintent.List(siParams)
	for siIter.Next() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		si := siIter.SetupIntent()

		saParams := &stripe.SetupAttemptListParams{
			SetupIntent: stripe.String(si.ID),
		}
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

		err := s.paginateAndSend(ctx, opts, results, "setup_attempt", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
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
			config.Debug("[STRIPE] Error fetching setup attempts for setup intent %s: %v", si.ID, err)
		}
	}

	if err := siIter.Err(); err != nil {
		return fmt.Errorf("failed to list setup intents for setup attempts: %w", err)
	}

	return nil
}

func (s *StripeSource) readSetupIntents(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching setup intents")

	params := &stripe.SetupIntentListParams{}
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

	subParams := &stripe.SubscriptionListParams{}
	subParams.Limit = stripe.Int64(int64(batchSize))
	subParams.Status = stripe.String("all")

	subIter := subscription.List(subParams)
	for subIter.Next() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		sub := subIter.Subscription()

		siParams := &stripe.SubscriptionItemListParams{
			Subscription: stripe.String(sub.ID),
		}
		siParams.Limit = stripe.Int64(int64(batchSize))

		err := s.paginateAndSend(ctx, opts, results, "subscription_item", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
			if startingAfter != "" {
				siParams.StartingAfter = stripe.String(startingAfter)
			}
			iter := subscriptionitem.List(siParams)
			if !iter.Next() {
				return nil, false, "", iter.Err()
			}
			return extractRawListItems(iter.SubscriptionItemList().LastResponse.RawJSON)
		})
		if err != nil {
			config.Debug("[STRIPE] Error fetching subscription items for subscription %s: %v", sub.ID, err)
		}
	}

	if err := subIter.Err(); err != nil {
		return fmt.Errorf("failed to list subscriptions for subscription items: %w", err)
	}

	return nil
}

func (s *StripeSource) readSubscriptionSchedules(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching subscription schedules")

	params := &stripe.SubscriptionScheduleListParams{}
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
	customerParams.Limit = stripe.Int64(int64(batchSize))

	customerIter := customer.List(customerParams)
	for customerIter.Next() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		c := customerIter.Customer()

		tidParams := &stripe.TaxIDListParams{
			Customer: stripe.String(c.ID),
		}
		tidParams.Limit = stripe.Int64(int64(batchSize))

		err := s.paginateAndSend(ctx, opts, results, "tax_id", func(startingAfter string) ([]map[string]interface{}, bool, string, error) {
			if startingAfter != "" {
				tidParams.StartingAfter = stripe.String(startingAfter)
			}
			iter := taxid.List(tidParams)
			if !iter.Next() {
				return nil, false, "", iter.Err()
			}
			return extractRawListItems(iter.TaxIDList().LastResponse.RawJSON)
		})
		if err != nil {
			config.Debug("[STRIPE] Error fetching tax IDs for customer %s: %v", c.ID, err)
		}
	}

	if err := customerIter.Err(); err != nil {
		return fmt.Errorf("failed to list customers for tax IDs: %w", err)
	}

	return nil
}

func (s *StripeSource) readTaxRates(ctx context.Context, opts source.ReadOptions, batchSize int, intervalStart, intervalEnd *time.Time, results chan<- source.RecordBatchResult) error {
	config.Debug("[STRIPE] Fetching tax rates")

	params := &stripe.TaxRateListParams{}
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
	totalSent := 0
	batchNum := 0
	var startingAfter string

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		items, hasMore, lastID, err := fetch(startingAfter)
		if err != nil {
			return fmt.Errorf("failed to fetch %s: %w", tableName, err)
		}

		if len(items) > 0 {
			record, err := arrowconv.ItemsToArrowRecordWithSchema(items, nil, opts.ExcludeColumns)
			if err != nil {
				return fmt.Errorf("failed to convert %s to Arrow: %w", tableName, err)
			}

			batchNum++
			config.Debug("[STRIPE] Sending batch %d with %d %s (total sent: %d)", batchNum, len(items), tableName, totalSent+len(items))
			results <- source.RecordBatchResult{Batch: record}
			totalSent += len(items)

			if opts.Limit > 0 && totalSent >= opts.Limit {
				config.Debug("[STRIPE] Reached limit of %d %s", opts.Limit, tableName)
				break
			}
		}

		if !hasMore {
			break
		}

		startingAfter = lastID
	}

	if totalSent == 0 {
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

var _ source.Source = (*StripeSource)(nil)
