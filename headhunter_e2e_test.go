package main

import (
	"log"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/nbenliogludev/go-browser-ai-agent/internal/agent"
	"github.com/nbenliogludev/go-browser-ai-agent/internal/browser"
	"github.com/nbenliogludev/go-browser-ai-agent/internal/llm"
)

func TestHeadHunterApply(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping HeadHunter e2e test in short mode")
	}

	startURL := "https://hh.ru/"
	task := "Изучи моё резюме на hh.ru и на основе моих навыков откликнись на четыре подходящие вакансии."

	log.Println("🚀 STARTING HEADHUNTER E2E TEST...")
	log.Printf("🌍 Navigating to %s ...", startURL)

	// Браузер-менеджер (Chromedp, тот же, что и для Getir)
	b := browser.NewManager()
	defer b.Close()

	// Навигация на hh.ru перед запуском агента
	navCtx, navCancel := b.WithTimeout(45 * time.Second)
	defer navCancel()

	if err := chromedp.Run(navCtx, chromedp.Navigate(startURL)); err != nil {
		t.Fatalf("failed to navigate to %s: %v", startURL, err)
	}

	// LLM-клиент
	cli, err := llm.NewOpenAIClient()
	if err != nil {
		t.Fatalf("failed to init OpenAI client: %v", err)
	}

	ag := agent.NewAgent(b, cli)

	log.Printf("🤖 AGENT STARTED with task: '%s'\n", task)

	const maxSteps = 40

	if err := ag.Run(task, maxSteps); err != nil {
		t.Fatalf("Agent finished with error: %v", err)
	}

	log.Println("✅ HeadHunter task finished successfully (agent claims it applied to 4 jobs and produced a report).")
}
