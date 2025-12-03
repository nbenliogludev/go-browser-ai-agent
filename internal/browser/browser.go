package browser

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/playwright-community/playwright-go"
)

type Manager struct {
	pw      *playwright.Playwright
	Context playwright.BrowserContext
	Page    playwright.Page
}

func NewManager() (*Manager, error) {
	if err := playwright.Install(); err != nil {
		return nil, fmt.Errorf("could not install Playwright: %w", err)
	}

	pw, err := playwright.Run()
	if err != nil {
		return nil, fmt.Errorf("could not start Playwright: %w", err)
	}

	userDataDir, err := persistentUserDataDir()
	if err != nil {
		_ = pw.Stop()
		return nil, err
	}

	context, err := pw.Chromium.LaunchPersistentContext(
		userDataDir,
		playwright.BrowserTypeLaunchPersistentContextOptions{
			Headless: playwright.Bool(false),
		},
	)
	if err != nil {
		_ = pw.Stop()
		return nil, fmt.Errorf("could not launch persistent context: %w", err)
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
			return nil, fmt.Errorf("could not create page: %w", err)
		}
	}

	log.Printf("browser started with user data dir: %s\n", userDataDir)

	return &Manager{
		pw:      pw,
		Context: context,
		Page:    page,
	}, nil
}

func (m *Manager) Close() {
	if m == nil {
		return
	}
	if m.Context != nil {
		if err := m.Context.Close(); err != nil {
			log.Printf("failed to close browser context: %v", err)
		}
	}
	if m.pw != nil {
		if err := m.pw.Stop(); err != nil {
			log.Printf("failed to stop Playwright: %v", err)
		}
	}
}

func persistentUserDataDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("could not get working directory: %w", err)
	}
	dir := filepath.Join(cwd, ".playwright-user-data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("could not create user data dir: %w", err)
	}
	return dir, nil
}
