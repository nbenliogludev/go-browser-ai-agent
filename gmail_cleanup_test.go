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

func TestGmailCleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Gmail e2e test in short mode")
	}

	startURL := "https://mail.google.com/"
	task := "Перейди в мой аккаунт Gmail, открой папку «Входящие», прочитай последние 10 писем (тема, отправитель, краткое содержание), проанализируй каждое письмо на спам, рекламные ссылки, подозрительных отправителей и фишинг. Все письма, которые ты считаешь спамом или фишингом, пометь как спам или удали, а в конце сделай краткий отчёт о всех своих действиях."

	log.Println("🚀 STARTING GMAIL CLEANUP E2E TEST...")
	log.Printf("🌍 Navigating to %s ...", startURL)

	// Браузер-менеджер (Chromedp, тот же, что и для других e2e)
	b := browser.NewManager()
	defer b.Close()

	// Навигация на Gmail перед запуском агента
	navCtx, navCancel := b.WithTimeout(60 * time.Second)
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

	const maxSteps = 50

	if err := ag.Run(task, maxSteps); err != nil {
		t.Fatalf("Agent finished with error: %v", err)
	}

	log.Println("✅ Gmail cleanup task finished successfully (agent claims it reviewed last 10 emails, cleaned spam, and produced a report).")
}
