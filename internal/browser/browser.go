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
	// 1. Сначала устанавливаем драйверы
	if err := playwright.Install(); err != nil {
		return nil, fmt.Errorf("install pw failed: %w", err)
	}

	// 2. Запускаем движок Playwright
	pw, err := playwright.Run()
	if err != nil {
		return nil, fmt.Errorf("start pw failed: %w", err)
	}

	userDataDir, _ := os.Getwd()
	userDataDir = filepath.Join(userDataDir, ".playwright_data")

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

	// 4. Получаем страницу ИЗ созданного контекста
	var page playwright.Page
	pages := context.Pages()
	if len(pages) > 0 {
		page = pages[0]
	} else {
		// Если страниц нет, создаем новую
		page, err = context.NewPage()
		if err != nil {
			context.Close()
			pw.Stop()
			return nil, fmt.Errorf("failed to create page: %w", err)
		}
	}

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
		m.Context.Close()
	}
	if m.pw != nil {
		m.pw.Stop()
	}
}
