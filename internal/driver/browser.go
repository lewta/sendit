package driver

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/lewta/sendit/internal/task"
)

// BrowserDriver executes tasks using a headless Chrome browser via chromedp.
// Each Execute call spawns an isolated browser instance to avoid memory leaks.
type BrowserDriver struct{}

// NewBrowserDriver creates a BrowserDriver.
func NewBrowserDriver() *BrowserDriver {
	return &BrowserDriver{}
}

// Execute navigates to t.URL with a headless Chrome instance.
func (d *BrowserDriver) Execute(ctx context.Context, t task.Task) task.Result {
	cfg := t.Config.Browser

	timeoutS := cfg.TimeoutS
	if timeoutS <= 0 {
		timeoutS = 30
	}

	// Isolated allocator per task — prevents memory accumulation.
	allocOpts := append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("no-sandbox", false), // keep sandbox on
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, allocOpts...)
	defer allocCancel()

	taskCtx, taskCancel := chromedp.NewContext(allocCtx)
	defer taskCancel()

	timeoutCtx, timeoutCancel := context.WithTimeout(taskCtx, time.Duration(timeoutS)*time.Second)
	defer timeoutCancel()

	start := time.Now()

	actions := []chromedp.Action{
		chromedp.Navigate(t.URL),
	}

	if cfg.WaitForSelector != "" {
		actions = append(actions, chromedp.WaitVisible(cfg.WaitForSelector, chromedp.ByQuery))
	}

	if cfg.Scroll {
		actions = append(actions,
			chromedp.Evaluate(`window.scrollTo(0, document.body.scrollHeight / 2)`, nil),
			chromedp.Sleep(500*time.Millisecond),
			chromedp.Evaluate(`window.scrollTo(0, document.body.scrollHeight)`, nil),
		)
	}

	err := chromedp.Run(timeoutCtx, actions...)
	elapsed := time.Since(start)

	if err != nil {
		return task.Result{Task: t, Duration: elapsed, Error: fmt.Errorf("browser: %w", err)}
	}

	return task.Result{
		Task:       t,
		StatusCode: 200,
		Duration:   elapsed,
	}
}
