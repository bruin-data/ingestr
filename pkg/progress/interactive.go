package progress

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
	"github.com/schollz/progressbar/v3"
)

type Spinner struct {
	bar      *progressbar.ProgressBar
	ticker   *time.Ticker
	done     chan struct{}
	wg       sync.WaitGroup
	once     sync.Once
	message  string
	interval time.Duration
}

func (s *Spinner) start(ctx context.Context, tickFn func()) {
	s.ticker = time.NewTicker(s.interval)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.done:
				return
			case <-s.ticker.C:
				tickFn()
			}
		}
	}()
}

func newSpinnerBar(description string, extra ...progressbar.Option) *progressbar.ProgressBar {
	opts := []progressbar.Option{
		progressbar.OptionSetDescription(description),
		progressbar.OptionSpinnerType(14),
		progressbar.OptionSetWidth(15),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionSetPredictTime(false),
		progressbar.OptionSetRenderBlankState(true),
	}
	opts = append(opts, extra...)
	return progressbar.NewOptions(-1, opts...)
}

func StartSpinner(ctx context.Context, message string) *Spinner {
	s := &Spinner{
		bar: newSpinnerBar(
			message,
			progressbar.OptionThrottle(100*time.Millisecond),
			progressbar.OptionClearOnFinish(),
		),
		done:     make(chan struct{}),
		message:  message,
		interval: 100 * time.Millisecond,
	}
	s.start(ctx, func() { _ = s.bar.Add(1) })
	return s
}

func (s *Spinner) SetMessage(msg string) {
	s.bar.Describe(msg)
}

func (s *Spinner) Stop() {
	s.once.Do(func() {
		if s.ticker != nil {
			s.ticker.Stop()
		}
		close(s.done)
		s.wg.Wait()
		_ = s.bar.Finish()
		fmt.Printf("\r%s done.\n", s.message)
	})
}

type spinnerKeyType struct{}

func WithSpinner(ctx context.Context, s *Spinner) context.Context {
	return context.WithValue(ctx, spinnerKeyType{}, s)
}

func UpdateSpinnerMessage(ctx context.Context, msg string) {
	if s, ok := ctx.Value(spinnerKeyType{}).(*Spinner); ok {
		s.SetMessage(msg)
	}
}

// InteractiveDisplay provides a dynamic spinner-based progress display.
// Uses progressbar for a responsive, animated terminal UI.
type InteractiveDisplay struct {
	spinner *Spinner
}

// NewInteractiveDisplay creates a new interactive display with a spinner.
func NewInteractiveDisplay() Display {
	bar := newSpinnerBar(
		"Ingesting",
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
		progressbar.OptionSetItsString("rows/s"),
		progressbar.OptionThrottle(500*time.Millisecond),
	)

	return &InteractiveDisplay{
		spinner: &Spinner{
			bar:      bar,
			done:     make(chan struct{}),
			message:  "Ingesting",
			interval: 500 * time.Millisecond,
		},
	}
}

// Start begins displaying progress updates every 500ms.
func (d *InteractiveDisplay) Start(ctx context.Context, collector *MetricsCollector) {
	d.spinner.start(ctx, func() {
		metrics := collector.Snapshot()
		d.update(metrics)
	})
	config.Debug("[PROGRESS] Interactive display started")
}

// Stop halts the display and shows the final summary.
func (d *InteractiveDisplay) Stop(metrics Metrics) {
	if d.spinner.ticker != nil {
		d.spinner.ticker.Stop()
	}
	close(d.spinner.done)
	d.spinner.wg.Wait()

	d.update(metrics)

	_ = d.spinner.bar.Finish()
	fmt.Println()

	d.printSummary(metrics)

	config.Debug("[PROGRESS] Interactive display stopped")
}

// update updates the progress bar with current metrics.
func (d *InteractiveDisplay) update(m Metrics) {
	_ = d.spinner.bar.Set(int(m.TotalRows))

	desc := fmt.Sprintf(
		"Ingesting | %s rows | %d batches | %.0f rows/s | CPU: %.1f%% | Mem: %.0f MB",
		formatNumber(m.TotalRows),
		m.TotalBatches,
		m.CurrentRowsPerSec,
		m.CPUPercent,
		m.MemoryMB,
	)

	d.spinner.bar.Describe(desc)
}

// printSummary displays the final ingestion summary.
func (d *InteractiveDisplay) printSummary(m Metrics) {
	fmt.Println()

	cfg := tablewriter.NewConfigBuilder().
		Row().
		Alignment().
		WithPerColumn([]tw.Align{tw.AlignLeft, tw.AlignRight}).
		Build().
		Build()

	table := tablewriter.NewTable(os.Stdout, tablewriter.WithConfig(cfg))
	table.Header("Metric", "Value")
	_ = table.Append("Total Rows", formatNumber(m.TotalRows))
	_ = table.Append("Total Batches", fmt.Sprintf("%d", m.TotalBatches))
	_ = table.Append("Duration", formatDuration(m.Duration()))
	_ = table.Append("Avg Throughput", fmt.Sprintf("%s rows/s", formatNumber(int64(m.AverageRowsPerSec()))))
	_ = table.Append("Peak Memory", fmt.Sprintf("%.0f MB", m.PeakMemoryMB))
	_ = table.Render()
}

// formatNumber formats large numbers with thousand separators.
func formatNumber(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}

	str := fmt.Sprintf("%d", n)
	result := ""
	for i, c := range str {
		if i > 0 && (len(str)-i)%3 == 0 {
			result += ","
		}
		result += string(c)
	}
	return result
}

// formatDuration formats a duration in a human-readable way.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Hour {
		minutes := int(d.Minutes())
		seconds := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh %dm", hours, minutes)
}
