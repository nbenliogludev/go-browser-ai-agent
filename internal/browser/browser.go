package browser

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/playwright-community/playwright-go"
)

// Константы для load state
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
	// Автоустановка драйверов
	if err := playwright.Install(); err != nil {
		return nil, fmt.Errorf("install pw failed: %w", err)
	}

	pw, err := playwright.Run()
	if err != nil {
		return nil, fmt.Errorf("start pw failed: %w", err)
	}

	userDataDir, _ := os.Getwd()
	userDataDir = filepath.Join(userDataDir, ".playwright_data")

	// Запускаем с Headless: false, чтобы вы видели браузер
	context, err := pw.Chromium.LaunchPersistentContext(userDataDir, playwright.BrowserTypeLaunchPersistentContextOptions{
		Headless: playwright.Bool(false),
		Viewport: &playwright.Size{Width: 1280, Height: 720},
		Args: []string{
			"--disable-blink-features=AutomationControlled", // Попытка скрыть автоматизацию
		},
	})
	if err != nil {
		pw.Stop()
		return nil, err
	}

	page := context.Pages()[0]
	// Увеличим таймаут
	page.SetDefaultTimeout(10000)

	return &Manager{
		pw:      pw,
		Context: context,
		Page:    page,
	}, nil
}

func (m *Manager) Close() {
	if m.Context != nil {
		m.Context.Close()
	}
	if m.pw != nil {
		m.pw.Stop()
	}
}
