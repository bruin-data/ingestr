package yfinance

import "github.com/bruin-data/ingestr/internal/registry"

func init() {
	registry.RegisterSource(
		[]string{"yfinance"},
		func() interface{} { return NewYFinanceSource() },
	)
}
