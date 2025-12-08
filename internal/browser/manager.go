package browser

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/playwright-community/playwright-go"
)

const (
	LoadStateLoad             = "load"
	LoadStateDomcontentloaded = "domcontentloaded"
	LoadStateNetworkidle      = "networkidle"
)

type Manager struct {
	pw      *playwright.Playwright
	Context playwright.BrowserContext
	Page    playwright.Page
}

func NewManager() (*Manager, error) {
	if err := playwright.Install(); err != nil {
		return nil, fmt.Errorf("install pw failed: %w", err)
	}

	pw, err := playwright.Run()
	if err != nil {
		return nil, fmt.Errorf("start pw failed: %w", err)
	}

	// user-data-dir в текущей папке проекта
	userDataDir, _ := os.Getwd()
	userDataDir = filepath.Join(userDataDir, ".playwright_data")

	// Настройки для запуска в максимизированном режиме
	context, err := pw.Chromium.LaunchPersistentContext(
		userDataDir,
		playwright.BrowserTypeLaunchPersistentContextOptions{
			Headless: playwright.Bool(false),
			// Viewport: nil отключает эмуляцию мобильного окна и дает браузеру управлять размером
			Viewport: nil,
			Args: []string{
				"--start-maximized",
				"--window-position=0,0",
				"--disable-blink-features=AutomationControlled",
			},
		},
	)
	if err != nil {
		_ = pw.Stop()
		return nil, err
	}

	var page playwright.Page
	pages := context.Pages()
	if len(pages) > 0 {
		page = pages[0]
	} else {
		page, err = context.NewPage()
		if err != nil {
			_ = context.Close()
			_ = pw.Stop()
			return nil, fmt.Errorf("failed to create page: %w", err)
		}
	}

	// JS-хак для гарантии разворота (опционально, но полезно)
	if _, err := page.Evaluate(`window.moveTo(0, 0); window.resizeTo(screen.availWidth, screen.availHeight);`); err != nil {
		fmt.Printf("Warning: failed to resize window via JS: %v\n", err)
	}

	// Убрали page.SetViewportSize(), так как он конфликтует с Viewport: nil

	page.SetDefaultTimeout(60000)
	page.SetDefaultNavigationTimeout(60000)

	return &Manager{
		pw:      pw,
		Context: context,
		Page:    page,
	}, nil
}

func (m *Manager) Close() {
	if m.Context != nil {
		_ = m.Context.Close()
	}
	if m.pw != nil {
		_ = m.pw.Stop()
	}
}
