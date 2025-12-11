package browser

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/chromedp/chromedp"
)

type Manager struct {
	Ctx    context.Context
	Cancel context.CancelFunc
}

func NewManager() *Manager {
	userDir := filepath.Join(os.TempDir(), "go-browser-ai-agent-profile")

	opts := append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.UserDataDir(userDir),
		chromedp.Flag("headless", false),
		chromedp.Flag("disable-gpu", false),
		chromedp.Flag("enable-automation", false),
		chromedp.Flag("disable-extensions", false),
		chromedp.WindowSize(1280, 800),
	)

	allocCtx, _ := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancel := chromedp.NewContext(allocCtx)

	if err := chromedp.Run(ctx); err != nil {
		cancel()
		panic(err)
	}

	return &Manager{
		Ctx:    ctx,
		Cancel: cancel,
	}
}

func (m *Manager) Close() {
	if m.Cancel != nil {
		m.Cancel()
	}
}

func (m *Manager) WithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(m.Ctx, d)
}
