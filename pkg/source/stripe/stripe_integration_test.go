//go:build integration

package stripe_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/ingestr/internal/testutil"
	_ "github.com/bruin-data/ingestr/pkg/source/stripe"
	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/coupon"
	"github.com/stripe/stripe-go/v81/customer"
	"github.com/stripe/stripe-go/v81/invoice"
	"github.com/stripe/stripe-go/v81/invoiceitem"
	"github.com/stripe/stripe-go/v81/plan"
	"github.com/stripe/stripe-go/v81/price"
	"github.com/stripe/stripe-go/v81/product"
	"github.com/stripe/stripe-go/v81/setupintent"
	"github.com/stripe/stripe-go/v81/taxrate"
)

func TestStripeEventsIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	apiKey := os.Getenv("STRIPE_API_KEY")
	if apiKey == "" {
		t.Skip("Set STRIPE_API_KEY to run Stripe integration tests")
	}

	stripe.Key = apiKey
	sourceURI := fmt.Sprintf("stripe://?api_key=%s", apiKey)

	t.Run("customer", testCustomerEvents(sourceURI))
	t.Run("product", testProductEvents(sourceURI))
	t.Run("coupon", testCouponEvents(sourceURI))
	t.Run("tax_rate", testTaxRateEvents(sourceURI))
	t.Run("plan", testPlanEvents(sourceURI))
	t.Run("price", testPriceEvents(sourceURI))
	t.Run("setup_intent", testSetupIntentEvents(sourceURI))
	t.Run("invoice", testInvoiceEvents(sourceURI))
	t.Run("invoice_item", testInvoiceItemEvents(sourceURI))
	t.Run("full_refresh_customer", testFullRefreshCustomer(sourceURI))
	t.Run("events_fallback_over_30_days", testEventsFallbackOver30Days(sourceURI))
}

func testCustomerEvents(sourceURI string) func(t *testing.T) {
	return func(t *testing.T) {
		ctx := context.Background()

		c1, err := customer.New(&stripe.CustomerParams{
			Name:  stripe.String("Gong Test Customer 1"),
			Email: stripe.String("gong-test-1@example.com"),
		})
		if err != nil {
			t.Fatalf("failed to create customer 1: %v", err)
		}
		defer customer.Del(c1.ID, nil) //nolint:errcheck

		c2, err := customer.New(&stripe.CustomerParams{
			Name:  stripe.String("Gong Test Customer 2"),
			Email: stripe.String("gong-test-2@example.com"),
		})
		if err != nil {
			t.Fatalf("failed to create customer 2: %v", err)
		}
		defer customer.Del(c2.ID, nil) //nolint:errcheck

		c3, err := customer.New(&stripe.CustomerParams{
			Name:  stripe.String("Gong Test Customer 3"),
			Email: stripe.String("gong-test-3@example.com"),
		})
		if err != nil {
			t.Fatalf("failed to create customer 3: %v", err)
		}
		defer customer.Del(c3.ID, nil) //nolint:errcheck

		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, fmt.Sprintf("stripe_customer_%d.duckdb", time.Now().UnixNano()))
		destURI := fmt.Sprintf("duckdb:///%s", dbPath)

		fullFetchExp := testutil.TableExpectation{
			SourceTable:         "customer",
			DestTable:           "main.customer",
			KeyColumn:           "id",
			MinExpectedRowCount: 3,
			Rows: []testutil.ExpectedRow{
				{ID: c1.ID, Fields: map[string]any{"name": "Gong Test Customer 1", "email": "gong-test-1@example.com"}},
				{ID: c2.ID, Fields: map[string]any{"name": "Gong Test Customer 2", "email": "gong-test-2@example.com"}},
				{ID: c3.ID, Fields: map[string]any{"name": "Gong Test Customer 3", "email": "gong-test-3@example.com"}},
			},
		}
		testutil.RunPipeline(t, ctx, sourceURI, destURI, fullFetchExp)
		testutil.Check(t, destURI, fullFetchExp)

		beforeUpdate := time.Now()
		time.Sleep(2 * time.Second)

		_, err = customer.Update(c1.ID, &stripe.CustomerParams{Name: stripe.String("Gong Updated Customer 1")})
		if err != nil {
			t.Fatalf("failed to update customer 1: %v", err)
		}
		_, err = customer.Update(c2.ID, &stripe.CustomerParams{Name: stripe.String("Gong Updated Customer 2")})
		if err != nil {
			t.Fatalf("failed to update customer 2: %v", err)
		}
		time.Sleep(5 * time.Second)

		mergeExp := testutil.TableExpectation{
			SourceTable:         "customer",
			DestTable:           "main.customer",
			KeyColumn:           "id",
			IntervalStart:       &beforeUpdate,
			MinExpectedRowCount: 3,
			Rows: []testutil.ExpectedRow{
				{ID: c1.ID, Fields: map[string]any{"name": "Gong Updated Customer 1", "email": "gong-test-1@example.com"}},
				{ID: c2.ID, Fields: map[string]any{"name": "Gong Updated Customer 2", "email": "gong-test-2@example.com"}},
				{ID: c3.ID, Fields: map[string]any{"name": "Gong Test Customer 3", "email": "gong-test-3@example.com"}},
			},
		}
		testutil.RunPipeline(t, ctx, sourceURI, destURI, mergeExp)
		testutil.Check(t, destURI, mergeExp)
	}
}

func testProductEvents(sourceURI string) func(t *testing.T) {
	return func(t *testing.T) {
		ctx := context.Background()

		p1, err := product.New(&stripe.ProductParams{Name: stripe.String("Gong Test Product 1")})
		if err != nil {
			t.Fatalf("failed to create product 1: %v", err)
		}
		defer product.Del(p1.ID, nil) //nolint:errcheck

		p2, err := product.New(&stripe.ProductParams{Name: stripe.String("Gong Test Product 2")})
		if err != nil {
			t.Fatalf("failed to create product 2: %v", err)
		}
		defer product.Del(p2.ID, nil) //nolint:errcheck

		p3, err := product.New(&stripe.ProductParams{Name: stripe.String("Gong Test Product 3")})
		if err != nil {
			t.Fatalf("failed to create product 3: %v", err)
		}
		defer product.Del(p3.ID, nil) //nolint:errcheck

		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, fmt.Sprintf("stripe_product_%d.duckdb", time.Now().UnixNano()))
		destURI := fmt.Sprintf("duckdb:///%s", dbPath)

		fullFetchExp := testutil.TableExpectation{
			SourceTable:         "product",
			DestTable:           "main.product",
			KeyColumn:           "id",
			MinExpectedRowCount: 3,
			Rows: []testutil.ExpectedRow{
				{ID: p1.ID, Fields: map[string]any{"name": "Gong Test Product 1"}},
				{ID: p2.ID, Fields: map[string]any{"name": "Gong Test Product 2"}},
				{ID: p3.ID, Fields: map[string]any{"name": "Gong Test Product 3"}},
			},
		}
		testutil.RunPipeline(t, ctx, sourceURI, destURI, fullFetchExp)
		testutil.Check(t, destURI, fullFetchExp)

		beforeUpdate := time.Now()
		time.Sleep(2 * time.Second)

		_, err = product.Update(p1.ID, &stripe.ProductParams{Name: stripe.String("Gong Updated Product 1")})
		if err != nil {
			t.Fatalf("failed to update product 1: %v", err)
		}
		_, err = product.Update(p2.ID, &stripe.ProductParams{Name: stripe.String("Gong Updated Product 2")})
		if err != nil {
			t.Fatalf("failed to update product 2: %v", err)
		}
		time.Sleep(5 * time.Second)

		mergeExp := testutil.TableExpectation{
			SourceTable:         "product",
			DestTable:           "main.product",
			KeyColumn:           "id",
			IntervalStart:       &beforeUpdate,
			MinExpectedRowCount: 3,
			Rows: []testutil.ExpectedRow{
				{ID: p1.ID, Fields: map[string]any{"name": "Gong Updated Product 1"}},
				{ID: p2.ID, Fields: map[string]any{"name": "Gong Updated Product 2"}},
				{ID: p3.ID, Fields: map[string]any{"name": "Gong Test Product 3"}},
			},
		}
		testutil.RunPipeline(t, ctx, sourceURI, destURI, mergeExp)
		testutil.Check(t, destURI, mergeExp)
	}
}

func testCouponEvents(sourceURI string) func(t *testing.T) {
	return func(t *testing.T) {
		ctx := context.Background()

		cp1, err := coupon.New(&stripe.CouponParams{
			PercentOff: stripe.Float64(10),
			Duration:   stripe.String("forever"),
			Name:       stripe.String("Gong Test Coupon 1"),
		})
		if err != nil {
			t.Fatalf("failed to create coupon 1: %v", err)
		}
		defer coupon.Del(cp1.ID, nil) //nolint:errcheck

		cp2, err := coupon.New(&stripe.CouponParams{
			PercentOff: stripe.Float64(20),
			Duration:   stripe.String("forever"),
			Name:       stripe.String("Gong Test Coupon 2"),
		})
		if err != nil {
			t.Fatalf("failed to create coupon 2: %v", err)
		}
		defer coupon.Del(cp2.ID, nil) //nolint:errcheck

		cp3, err := coupon.New(&stripe.CouponParams{
			PercentOff: stripe.Float64(30),
			Duration:   stripe.String("forever"),
			Name:       stripe.String("Gong Test Coupon 3"),
		})
		if err != nil {
			t.Fatalf("failed to create coupon 3: %v", err)
		}
		defer coupon.Del(cp3.ID, nil) //nolint:errcheck

		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, fmt.Sprintf("stripe_coupon_%d.duckdb", time.Now().UnixNano()))
		destURI := fmt.Sprintf("duckdb:///%s", dbPath)

		fullFetchExp := testutil.TableExpectation{
			SourceTable:         "coupon",
			DestTable:           "main.coupon",
			KeyColumn:           "id",
			MinExpectedRowCount: 3,
			Rows: []testutil.ExpectedRow{
				{ID: cp1.ID, Fields: map[string]any{"name": "Gong Test Coupon 1", "percent_off": float64(10)}},
				{ID: cp2.ID, Fields: map[string]any{"name": "Gong Test Coupon 2", "percent_off": float64(20)}},
				{ID: cp3.ID, Fields: map[string]any{"name": "Gong Test Coupon 3", "percent_off": float64(30)}},
			},
		}
		testutil.RunPipeline(t, ctx, sourceURI, destURI, fullFetchExp)
		testutil.Check(t, destURI, fullFetchExp)

		beforeUpdate := time.Now()
		time.Sleep(2 * time.Second)

		_, err = coupon.Update(cp1.ID, &stripe.CouponParams{Name: stripe.String("Gong Updated Coupon 1")})
		if err != nil {
			t.Fatalf("failed to update coupon 1: %v", err)
		}
		_, err = coupon.Update(cp2.ID, &stripe.CouponParams{Name: stripe.String("Gong Updated Coupon 2")})
		if err != nil {
			t.Fatalf("failed to update coupon 2: %v", err)
		}
		time.Sleep(5 * time.Second)

		mergeExp := testutil.TableExpectation{
			SourceTable:         "coupon",
			DestTable:           "main.coupon",
			KeyColumn:           "id",
			IntervalStart:       &beforeUpdate,
			MinExpectedRowCount: 3,
			Rows: []testutil.ExpectedRow{
				{ID: cp1.ID, Fields: map[string]any{"name": "Gong Updated Coupon 1", "percent_off": float64(10)}},
				{ID: cp2.ID, Fields: map[string]any{"name": "Gong Updated Coupon 2", "percent_off": float64(20)}},
				{ID: cp3.ID, Fields: map[string]any{"name": "Gong Test Coupon 3", "percent_off": float64(30)}},
			},
		}
		testutil.RunPipeline(t, ctx, sourceURI, destURI, mergeExp)
		testutil.Check(t, destURI, mergeExp)
	}
}

func testTaxRateEvents(sourceURI string) func(t *testing.T) {
	return func(t *testing.T) {
		ctx := context.Background()

		tr1, err := taxrate.New(&stripe.TaxRateParams{
			DisplayName: stripe.String("Gong Test Tax 1"),
			Percentage:  stripe.Float64(5),
			Inclusive:   stripe.Bool(false),
		})
		if err != nil {
			t.Fatalf("failed to create tax rate 1: %v", err)
		}
		defer taxrate.Update(tr1.ID, &stripe.TaxRateParams{Active: stripe.Bool(false)}) //nolint:errcheck

		tr2, err := taxrate.New(&stripe.TaxRateParams{
			DisplayName: stripe.String("Gong Test Tax 2"),
			Percentage:  stripe.Float64(10),
			Inclusive:   stripe.Bool(false),
		})
		if err != nil {
			t.Fatalf("failed to create tax rate 2: %v", err)
		}
		defer taxrate.Update(tr2.ID, &stripe.TaxRateParams{Active: stripe.Bool(false)}) //nolint:errcheck

		tr3, err := taxrate.New(&stripe.TaxRateParams{
			DisplayName: stripe.String("Gong Test Tax 3"),
			Percentage:  stripe.Float64(15),
			Inclusive:   stripe.Bool(true),
		})
		if err != nil {
			t.Fatalf("failed to create tax rate 3: %v", err)
		}
		defer taxrate.Update(tr3.ID, &stripe.TaxRateParams{Active: stripe.Bool(false)}) //nolint:errcheck

		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, fmt.Sprintf("stripe_taxrate_%d.duckdb", time.Now().UnixNano()))
		destURI := fmt.Sprintf("duckdb:///%s", dbPath)

		fullFetchExp := testutil.TableExpectation{
			SourceTable:         "tax_rate",
			DestTable:           "main.tax_rate",
			KeyColumn:           "id",
			MinExpectedRowCount: 3,
			Rows: []testutil.ExpectedRow{
				{ID: tr1.ID, Fields: map[string]any{"display_name": "Gong Test Tax 1", "percentage": float64(5), "inclusive": false}},
				{ID: tr2.ID, Fields: map[string]any{"display_name": "Gong Test Tax 2", "percentage": float64(10), "inclusive": false}},
				{ID: tr3.ID, Fields: map[string]any{"display_name": "Gong Test Tax 3", "percentage": float64(15), "inclusive": true}},
			},
		}
		testutil.RunPipeline(t, ctx, sourceURI, destURI, fullFetchExp)
		testutil.Check(t, destURI, fullFetchExp)

		beforeUpdate := time.Now()
		time.Sleep(2 * time.Second)

		_, err = taxrate.Update(tr1.ID, &stripe.TaxRateParams{DisplayName: stripe.String("Gong Updated Tax 1")})
		if err != nil {
			t.Fatalf("failed to update tax rate 1: %v", err)
		}
		_, err = taxrate.Update(tr2.ID, &stripe.TaxRateParams{DisplayName: stripe.String("Gong Updated Tax 2")})
		if err != nil {
			t.Fatalf("failed to update tax rate 2: %v", err)
		}
		time.Sleep(5 * time.Second)

		mergeExp := testutil.TableExpectation{
			SourceTable:         "tax_rate",
			DestTable:           "main.tax_rate",
			KeyColumn:           "id",
			IntervalStart:       &beforeUpdate,
			MinExpectedRowCount: 3,
			Rows: []testutil.ExpectedRow{
				{ID: tr1.ID, Fields: map[string]any{"display_name": "Gong Updated Tax 1", "percentage": float64(5), "inclusive": false}},
				{ID: tr2.ID, Fields: map[string]any{"display_name": "Gong Updated Tax 2", "percentage": float64(10), "inclusive": false}},
				{ID: tr3.ID, Fields: map[string]any{"display_name": "Gong Test Tax 3", "percentage": float64(15), "inclusive": true}},
			},
		}
		testutil.RunPipeline(t, ctx, sourceURI, destURI, mergeExp)
		testutil.Check(t, destURI, mergeExp)
	}
}

func testPlanEvents(sourceURI string) func(t *testing.T) {
	return func(t *testing.T) {
		ctx := context.Background()

		prod, err := product.New(&stripe.ProductParams{Name: stripe.String("Gong Test Plan Product")})
		if err != nil {
			t.Fatalf("failed to create product for plans: %v", err)
		}
		defer product.Del(prod.ID, nil) //nolint:errcheck

		pl1, err := plan.New(&stripe.PlanParams{
			Amount:    stripe.Int64(1000),
			Currency:  stripe.String("usd"),
			Interval:  stripe.String("month"),
			ProductID: stripe.String(prod.ID),
			Nickname:  stripe.String("Gong Test Plan 1"),
		})
		if err != nil {
			t.Fatalf("failed to create plan 1: %v", err)
		}
		defer plan.Del(pl1.ID, nil) //nolint:errcheck

		pl2, err := plan.New(&stripe.PlanParams{
			Amount:    stripe.Int64(2000),
			Currency:  stripe.String("usd"),
			Interval:  stripe.String("month"),
			ProductID: stripe.String(prod.ID),
			Nickname:  stripe.String("Gong Test Plan 2"),
		})
		if err != nil {
			t.Fatalf("failed to create plan 2: %v", err)
		}
		defer plan.Del(pl2.ID, nil) //nolint:errcheck

		pl3, err := plan.New(&stripe.PlanParams{
			Amount:    stripe.Int64(3000),
			Currency:  stripe.String("usd"),
			Interval:  stripe.String("month"),
			ProductID: stripe.String(prod.ID),
			Nickname:  stripe.String("Gong Test Plan 3"),
		})
		if err != nil {
			t.Fatalf("failed to create plan 3: %v", err)
		}
		defer plan.Del(pl3.ID, nil) //nolint:errcheck

		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, fmt.Sprintf("stripe_plan_%d.duckdb", time.Now().UnixNano()))
		destURI := fmt.Sprintf("duckdb:///%s", dbPath)

		fullFetchExp := testutil.TableExpectation{
			SourceTable:         "plan",
			DestTable:           "main.plan",
			KeyColumn:           "id",
			MinExpectedRowCount: 3,
			Rows: []testutil.ExpectedRow{
				{ID: pl1.ID, Fields: map[string]any{"nickname": "Gong Test Plan 1"}},
				{ID: pl2.ID, Fields: map[string]any{"nickname": "Gong Test Plan 2"}},
				{ID: pl3.ID, Fields: map[string]any{"nickname": "Gong Test Plan 3"}},
			},
		}
		testutil.RunPipeline(t, ctx, sourceURI, destURI, fullFetchExp)
		testutil.Check(t, destURI, fullFetchExp)

		beforeUpdate := time.Now()
		time.Sleep(2 * time.Second)

		_, err = plan.Update(pl1.ID, &stripe.PlanParams{Nickname: stripe.String("Gong Updated Plan 1")})
		if err != nil {
			t.Fatalf("failed to update plan 1: %v", err)
		}
		_, err = plan.Update(pl2.ID, &stripe.PlanParams{Nickname: stripe.String("Gong Updated Plan 2")})
		if err != nil {
			t.Fatalf("failed to update plan 2: %v", err)
		}
		time.Sleep(5 * time.Second)

		mergeExp := testutil.TableExpectation{
			SourceTable:         "plan",
			DestTable:           "main.plan",
			KeyColumn:           "id",
			IntervalStart:       &beforeUpdate,
			MinExpectedRowCount: 3,
			Rows: []testutil.ExpectedRow{
				{ID: pl1.ID, Fields: map[string]any{"nickname": "Gong Updated Plan 1"}},
				{ID: pl2.ID, Fields: map[string]any{"nickname": "Gong Updated Plan 2"}},
				{ID: pl3.ID, Fields: map[string]any{"nickname": "Gong Test Plan 3"}},
			},
		}
		testutil.RunPipeline(t, ctx, sourceURI, destURI, mergeExp)
		testutil.Check(t, destURI, mergeExp)
	}
}

func testPriceEvents(sourceURI string) func(t *testing.T) {
	return func(t *testing.T) {
		ctx := context.Background()

		prod, err := product.New(&stripe.ProductParams{Name: stripe.String("Gong Test Price Product")})
		if err != nil {
			t.Fatalf("failed to create product for prices: %v", err)
		}
		defer product.Del(prod.ID, nil) //nolint:errcheck

		pr1, err := price.New(&stripe.PriceParams{
			UnitAmount: stripe.Int64(1000),
			Currency:   stripe.String("usd"),
			Product:    stripe.String(prod.ID),
			Nickname:   stripe.String("Gong Test Price 1"),
		})
		if err != nil {
			t.Fatalf("failed to create price 1: %v", err)
		}
		defer price.Update(pr1.ID, &stripe.PriceParams{Active: stripe.Bool(false)}) //nolint:errcheck

		pr2, err := price.New(&stripe.PriceParams{
			UnitAmount: stripe.Int64(2000),
			Currency:   stripe.String("usd"),
			Product:    stripe.String(prod.ID),
			Nickname:   stripe.String("Gong Test Price 2"),
		})
		if err != nil {
			t.Fatalf("failed to create price 2: %v", err)
		}
		defer price.Update(pr2.ID, &stripe.PriceParams{Active: stripe.Bool(false)}) //nolint:errcheck

		pr3, err := price.New(&stripe.PriceParams{
			UnitAmount: stripe.Int64(3000),
			Currency:   stripe.String("usd"),
			Product:    stripe.String(prod.ID),
			Nickname:   stripe.String("Gong Test Price 3"),
		})
		if err != nil {
			t.Fatalf("failed to create price 3: %v", err)
		}
		defer price.Update(pr3.ID, &stripe.PriceParams{Active: stripe.Bool(false)}) //nolint:errcheck

		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, fmt.Sprintf("stripe_price_%d.duckdb", time.Now().UnixNano()))
		destURI := fmt.Sprintf("duckdb:///%s", dbPath)

		fullFetchExp := testutil.TableExpectation{
			SourceTable:         "price",
			DestTable:           "main.price",
			KeyColumn:           "id",
			MinExpectedRowCount: 3,
			Rows: []testutil.ExpectedRow{
				{ID: pr1.ID, Fields: map[string]any{"nickname": "Gong Test Price 1"}},
				{ID: pr2.ID, Fields: map[string]any{"nickname": "Gong Test Price 2"}},
				{ID: pr3.ID, Fields: map[string]any{"nickname": "Gong Test Price 3"}},
			},
		}
		testutil.RunPipeline(t, ctx, sourceURI, destURI, fullFetchExp)
		testutil.Check(t, destURI, fullFetchExp)

		beforeUpdate := time.Now()
		time.Sleep(2 * time.Second)

		_, err = price.Update(pr1.ID, &stripe.PriceParams{Nickname: stripe.String("Gong Updated Price 1")})
		if err != nil {
			t.Fatalf("failed to update price 1: %v", err)
		}
		_, err = price.Update(pr2.ID, &stripe.PriceParams{Nickname: stripe.String("Gong Updated Price 2")})
		if err != nil {
			t.Fatalf("failed to update price 2: %v", err)
		}
		time.Sleep(5 * time.Second)

		mergeExp := testutil.TableExpectation{
			SourceTable:         "price",
			DestTable:           "main.price",
			KeyColumn:           "id",
			IntervalStart:       &beforeUpdate,
			MinExpectedRowCount: 3,
			Rows: []testutil.ExpectedRow{
				{ID: pr1.ID, Fields: map[string]any{"nickname": "Gong Updated Price 1"}},
				{ID: pr2.ID, Fields: map[string]any{"nickname": "Gong Updated Price 2"}},
				{ID: pr3.ID, Fields: map[string]any{"nickname": "Gong Test Price 3"}},
			},
		}
		testutil.RunPipeline(t, ctx, sourceURI, destURI, mergeExp)
		testutil.Check(t, destURI, mergeExp)
	}
}

func testSetupIntentEvents(sourceURI string) func(t *testing.T) {
	return func(t *testing.T) {
		ctx := context.Background()

		si1, err := setupintent.New(&stripe.SetupIntentParams{
			Description:        stripe.String("Gong Test SI 1"),
			PaymentMethodTypes: stripe.StringSlice([]string{"card"}),
		})
		if err != nil {
			t.Fatalf("failed to create setup intent 1: %v", err)
		}
		defer setupintent.Cancel(si1.ID, nil) //nolint:errcheck

		si2, err := setupintent.New(&stripe.SetupIntentParams{
			Description:        stripe.String("Gong Test SI 2"),
			PaymentMethodTypes: stripe.StringSlice([]string{"card"}),
		})
		if err != nil {
			t.Fatalf("failed to create setup intent 2: %v", err)
		}
		defer setupintent.Cancel(si2.ID, nil) //nolint:errcheck

		si3, err := setupintent.New(&stripe.SetupIntentParams{
			Description:        stripe.String("Gong Test SI 3"),
			PaymentMethodTypes: stripe.StringSlice([]string{"card"}),
		})
		if err != nil {
			t.Fatalf("failed to create setup intent 3: %v", err)
		}
		defer setupintent.Cancel(si3.ID, nil) //nolint:errcheck

		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, fmt.Sprintf("stripe_si_%d.duckdb", time.Now().UnixNano()))
		destURI := fmt.Sprintf("duckdb:///%s", dbPath)

		fullFetchExp := testutil.TableExpectation{
			SourceTable:         "setup_intent",
			DestTable:           "main.setup_intent",
			KeyColumn:           "id",
			MinExpectedRowCount: 3,
			Rows: []testutil.ExpectedRow{
				{ID: si1.ID, Fields: map[string]any{"description": "Gong Test SI 1"}},
				{ID: si2.ID, Fields: map[string]any{"description": "Gong Test SI 2"}},
				{ID: si3.ID, Fields: map[string]any{"description": "Gong Test SI 3"}},
			},
		}
		testutil.RunPipeline(t, ctx, sourceURI, destURI, fullFetchExp)
		testutil.Check(t, destURI, fullFetchExp)

		beforeCreate := time.Now()
		time.Sleep(2 * time.Second)

		si4, err := setupintent.New(&stripe.SetupIntentParams{
			Description:        stripe.String("Gong Test SI 4"),
			PaymentMethodTypes: stripe.StringSlice([]string{"card"}),
		})
		if err != nil {
			t.Fatalf("failed to create setup intent 4: %v", err)
		}
		defer setupintent.Cancel(si4.ID, nil) //nolint:errcheck

		si5, err := setupintent.New(&stripe.SetupIntentParams{
			Description:        stripe.String("Gong Test SI 5"),
			PaymentMethodTypes: stripe.StringSlice([]string{"card"}),
		})
		if err != nil {
			t.Fatalf("failed to create setup intent 5: %v", err)
		}
		defer setupintent.Cancel(si5.ID, nil) //nolint:errcheck

		time.Sleep(5 * time.Second)

		mergeExp := testutil.TableExpectation{
			SourceTable:         "setup_intent",
			DestTable:           "main.setup_intent",
			KeyColumn:           "id",
			IntervalStart:       &beforeCreate,
			MinExpectedRowCount: 5,
			Rows: []testutil.ExpectedRow{
				{ID: si1.ID, Fields: map[string]any{"description": "Gong Test SI 1"}},
				{ID: si2.ID, Fields: map[string]any{"description": "Gong Test SI 2"}},
				{ID: si3.ID, Fields: map[string]any{"description": "Gong Test SI 3"}},
				{ID: si4.ID, Fields: map[string]any{"description": "Gong Test SI 4"}},
				{ID: si5.ID, Fields: map[string]any{"description": "Gong Test SI 5"}},
			},
		}
		testutil.RunPipeline(t, ctx, sourceURI, destURI, mergeExp)
		testutil.Check(t, destURI, mergeExp)
	}
}

func testInvoiceEvents(sourceURI string) func(t *testing.T) {
	return func(t *testing.T) {
		ctx := context.Background()

		cust, err := customer.New(&stripe.CustomerParams{
			Name: stripe.String("Gong Test Invoice Customer"),
		})
		if err != nil {
			t.Fatalf("failed to create customer for invoices: %v", err)
		}
		defer customer.Del(cust.ID, nil) //nolint:errcheck

		inv1, err := invoice.New(&stripe.InvoiceParams{
			Customer:    stripe.String(cust.ID),
			Description: stripe.String("Gong Test Invoice 1"),
		})
		if err != nil {
			t.Fatalf("failed to create invoice 1: %v", err)
		}
		defer invoice.Del(inv1.ID, nil) //nolint:errcheck

		inv2, err := invoice.New(&stripe.InvoiceParams{
			Customer:    stripe.String(cust.ID),
			Description: stripe.String("Gong Test Invoice 2"),
		})
		if err != nil {
			t.Fatalf("failed to create invoice 2: %v", err)
		}
		defer invoice.Del(inv2.ID, nil) //nolint:errcheck

		inv3, err := invoice.New(&stripe.InvoiceParams{
			Customer:    stripe.String(cust.ID),
			Description: stripe.String("Gong Test Invoice 3"),
		})
		if err != nil {
			t.Fatalf("failed to create invoice 3: %v", err)
		}
		defer invoice.Del(inv3.ID, nil) //nolint:errcheck

		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, fmt.Sprintf("stripe_invoice_%d.duckdb", time.Now().UnixNano()))
		destURI := fmt.Sprintf("duckdb:///%s", dbPath)

		fullFetchExp := testutil.TableExpectation{
			SourceTable:         "invoice",
			DestTable:           "main.invoice",
			KeyColumn:           "id",
			MinExpectedRowCount: 3,
			Rows: []testutil.ExpectedRow{
				{ID: inv1.ID, Fields: map[string]any{"description": "Gong Test Invoice 1"}},
				{ID: inv2.ID, Fields: map[string]any{"description": "Gong Test Invoice 2"}},
				{ID: inv3.ID, Fields: map[string]any{"description": "Gong Test Invoice 3"}},
			},
		}
		testutil.RunPipeline(t, ctx, sourceURI, destURI, fullFetchExp)
		testutil.Check(t, destURI, fullFetchExp)

		beforeUpdate := time.Now()
		time.Sleep(2 * time.Second)

		_, err = invoice.Update(inv1.ID, &stripe.InvoiceParams{Description: stripe.String("Gong Updated Invoice 1")})
		if err != nil {
			t.Fatalf("failed to update invoice 1: %v", err)
		}
		_, err = invoice.Update(inv2.ID, &stripe.InvoiceParams{Description: stripe.String("Gong Updated Invoice 2")})
		if err != nil {
			t.Fatalf("failed to update invoice 2: %v", err)
		}
		time.Sleep(5 * time.Second)

		mergeExp := testutil.TableExpectation{
			SourceTable:         "invoice",
			DestTable:           "main.invoice",
			KeyColumn:           "id",
			IntervalStart:       &beforeUpdate,
			MinExpectedRowCount: 3,
			Rows: []testutil.ExpectedRow{
				{ID: inv1.ID, Fields: map[string]any{"description": "Gong Updated Invoice 1"}},
				{ID: inv2.ID, Fields: map[string]any{"description": "Gong Updated Invoice 2"}},
				{ID: inv3.ID, Fields: map[string]any{"description": "Gong Test Invoice 3"}},
			},
		}
		testutil.RunPipeline(t, ctx, sourceURI, destURI, mergeExp)
		testutil.Check(t, destURI, mergeExp)
	}
}

func testFullRefreshCustomer(sourceURI string) func(t *testing.T) {
	return func(t *testing.T) {
		ctx := context.Background()

		c1, err := customer.New(&stripe.CustomerParams{
			Name:  stripe.String("Gong FullRefresh Customer 1"),
			Email: stripe.String("gong-fr-1@example.com"),
		})
		if err != nil {
			t.Fatalf("failed to create customer 1: %v", err)
		}
		defer customer.Del(c1.ID, nil) //nolint:errcheck

		c2, err := customer.New(&stripe.CustomerParams{
			Name:  stripe.String("Gong FullRefresh Customer 2"),
			Email: stripe.String("gong-fr-2@example.com"),
		})
		if err != nil {
			t.Fatalf("failed to create customer 2: %v", err)
		}
		defer customer.Del(c2.ID, nil) //nolint:errcheck

		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, fmt.Sprintf("stripe_customer_fr_%d.duckdb", time.Now().UnixNano()))
		destURI := fmt.Sprintf("duckdb:///%s", dbPath)

		fullRefreshExp := testutil.TableExpectation{
			SourceTable:         "customer",
			DestTable:           "main.customer",
			KeyColumn:           "id",
			MinExpectedRowCount: 2,
			FullRefresh:         true,
			Rows: []testutil.ExpectedRow{
				{ID: c1.ID, Fields: map[string]any{"name": "Gong FullRefresh Customer 1", "email": "gong-fr-1@example.com"}},
				{ID: c2.ID, Fields: map[string]any{"name": "Gong FullRefresh Customer 2", "email": "gong-fr-2@example.com"}},
			},
		}
		testutil.RunPipeline(t, ctx, sourceURI, destURI, fullRefreshExp)
		testutil.Check(t, destURI, fullRefreshExp)

		_, err = customer.Update(c1.ID, &stripe.CustomerParams{Name: stripe.String("Gong FR Updated Customer 1")})
		if err != nil {
			t.Fatalf("failed to update customer 1: %v", err)
		}

		fullRefreshAfterUpdateExp := testutil.TableExpectation{
			SourceTable:         "customer",
			DestTable:           "main.customer",
			KeyColumn:           "id",
			MinExpectedRowCount: 2,
			FullRefresh:         true,
			Rows: []testutil.ExpectedRow{
				{ID: c1.ID, Fields: map[string]any{"name": "Gong FR Updated Customer 1", "email": "gong-fr-1@example.com"}},
				{ID: c2.ID, Fields: map[string]any{"name": "Gong FullRefresh Customer 2", "email": "gong-fr-2@example.com"}},
			},
		}
		testutil.RunPipeline(t, ctx, sourceURI, destURI, fullRefreshAfterUpdateExp)
		testutil.Check(t, destURI, fullRefreshAfterUpdateExp)
	}
}

func testInvoiceItemEvents(sourceURI string) func(t *testing.T) {
	return func(t *testing.T) {
		ctx := context.Background()

		cust, err := customer.New(&stripe.CustomerParams{
			Name: stripe.String("Gong Test InvoiceItem Customer"),
		})
		if err != nil {
			t.Fatalf("failed to create customer for invoice items: %v", err)
		}
		defer customer.Del(cust.ID, nil) //nolint:errcheck

		ii1, err := invoiceitem.New(&stripe.InvoiceItemParams{
			Customer:    stripe.String(cust.ID),
			Amount:      stripe.Int64(500),
			Currency:    stripe.String("usd"),
			Description: stripe.String("Gong Test Item 1"),
		})
		if err != nil {
			t.Fatalf("failed to create invoice item 1: %v", err)
		}
		defer invoiceitem.Del(ii1.ID, nil) //nolint:errcheck

		ii2, err := invoiceitem.New(&stripe.InvoiceItemParams{
			Customer:    stripe.String(cust.ID),
			Amount:      stripe.Int64(1000),
			Currency:    stripe.String("usd"),
			Description: stripe.String("Gong Test Item 2"),
		})
		if err != nil {
			t.Fatalf("failed to create invoice item 2: %v", err)
		}
		defer invoiceitem.Del(ii2.ID, nil) //nolint:errcheck

		ii3, err := invoiceitem.New(&stripe.InvoiceItemParams{
			Customer:    stripe.String(cust.ID),
			Amount:      stripe.Int64(1500),
			Currency:    stripe.String("usd"),
			Description: stripe.String("Gong Test Item 3"),
		})
		if err != nil {
			t.Fatalf("failed to create invoice item 3: %v", err)
		}
		defer invoiceitem.Del(ii3.ID, nil) //nolint:errcheck

		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, fmt.Sprintf("stripe_invoiceitem_%d.duckdb", time.Now().UnixNano()))
		destURI := fmt.Sprintf("duckdb:///%s", dbPath)

		fullFetchExp := testutil.TableExpectation{
			SourceTable:         "invoice_item",
			DestTable:           "main.invoice_item",
			KeyColumn:           "id",
			MinExpectedRowCount: 3,
			Rows: []testutil.ExpectedRow{
				{ID: ii1.ID, Fields: map[string]any{"description": "Gong Test Item 1"}},
				{ID: ii2.ID, Fields: map[string]any{"description": "Gong Test Item 2"}},
				{ID: ii3.ID, Fields: map[string]any{"description": "Gong Test Item 3"}},
			},
		}
		testutil.RunPipeline(t, ctx, sourceURI, destURI, fullFetchExp)
		testutil.Check(t, destURI, fullFetchExp)

		beforeCreate := time.Now()
		time.Sleep(2 * time.Second)

		ii4, err := invoiceitem.New(&stripe.InvoiceItemParams{
			Customer:    stripe.String(cust.ID),
			Amount:      stripe.Int64(2000),
			Currency:    stripe.String("usd"),
			Description: stripe.String("Gong Test Item 4"),
		})
		if err != nil {
			t.Fatalf("failed to create invoice item 4: %v", err)
		}
		defer invoiceitem.Del(ii4.ID, nil) //nolint:errcheck

		ii5, err := invoiceitem.New(&stripe.InvoiceItemParams{
			Customer:    stripe.String(cust.ID),
			Amount:      stripe.Int64(2500),
			Currency:    stripe.String("usd"),
			Description: stripe.String("Gong Test Item 5"),
		})
		if err != nil {
			t.Fatalf("failed to create invoice item 5: %v", err)
		}
		defer invoiceitem.Del(ii5.ID, nil) //nolint:errcheck

		time.Sleep(5 * time.Second)

		mergeExp := testutil.TableExpectation{
			SourceTable:         "invoice_item",
			DestTable:           "main.invoice_item",
			KeyColumn:           "id",
			IntervalStart:       &beforeCreate,
			MinExpectedRowCount: 5,
			Rows: []testutil.ExpectedRow{
				{ID: ii1.ID, Fields: map[string]any{"description": "Gong Test Item 1"}},
				{ID: ii2.ID, Fields: map[string]any{"description": "Gong Test Item 2"}},
				{ID: ii3.ID, Fields: map[string]any{"description": "Gong Test Item 3"}},
				{ID: ii4.ID, Fields: map[string]any{"description": "Gong Test Item 4"}},
				{ID: ii5.ID, Fields: map[string]any{"description": "Gong Test Item 5"}},
			},
		}
		testutil.RunPipeline(t, ctx, sourceURI, destURI, mergeExp)
		testutil.Check(t, destURI, mergeExp)
	}
}

func testEventsFallbackOver30Days(sourceURI string) func(t *testing.T) {
	return func(t *testing.T) {
		ctx := context.Background()

		p1, err := product.New(&stripe.ProductParams{Name: stripe.String("Gong Fallback Product 1")})
		if err != nil {
			t.Fatalf("failed to create product 1: %v", err)
		}
		defer product.Del(p1.ID, nil) //nolint:errcheck

		p2, err := product.New(&stripe.ProductParams{Name: stripe.String("Gong Fallback Product 2")})
		if err != nil {
			t.Fatalf("failed to create product 2: %v", err)
		}
		defer product.Del(p2.ID, nil) //nolint:errcheck

		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, fmt.Sprintf("stripe_product_fallback_%d.duckdb", time.Now().UnixNano()))
		destURI := fmt.Sprintf("duckdb:///%s", dbPath)

		sixtyDaysAgo := time.Now().AddDate(0, 0, -60)
		fallbackExp := testutil.TableExpectation{
			SourceTable:         "product",
			DestTable:           "main.product",
			KeyColumn:           "id",
			MinExpectedRowCount: 2,
			IntervalStart:       &sixtyDaysAgo,
			Rows: []testutil.ExpectedRow{
				{ID: p1.ID, Fields: map[string]any{"name": "Gong Fallback Product 1"}},
				{ID: p2.ID, Fields: map[string]any{"name": "Gong Fallback Product 2"}},
			},
		}

		// Capture stdout to verify the fallback warning is printed
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		testutil.RunPipeline(t, ctx, sourceURI, destURI, fallbackExp)

		_ = w.Close()
		os.Stdout = oldStdout
		var buf bytes.Buffer
		io.Copy(&buf, r) //nolint:errcheck
		output := buf.String()

		if !strings.Contains(output, "Falling back to sync incremental mode") {
			t.Error("expected fallback warning message when interval-start > 30 days")
		}

		testutil.Check(t, destURI, fallbackExp)
	}
}
