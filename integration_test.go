package main

import (
	"log"
	"os"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/nbenliogludev/go-browser-ai-agent/internal/agent"
	"github.com/nbenliogludev/go-browser-ai-agent/internal/browser"
	"github.com/nbenliogludev/go-browser-ai-agent/internal/llm"
)

func TestGetir(t *testing.T) {
	targetURL := "https://getir.com/yemek/"
	userTask := "закажи 2 микс тост"
	maxSteps := 20

	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY is not set, skipping e2e test")
	}

	log.Println("🚀 STARTING AUTOMATED TEST...")

	b := browser.NewManager()
	defer b.Close()

	log.Printf("Navigating to %s...", targetURL)
	if err := chromedp.Run(
		b.Ctx,
		chromedp.Navigate(targetURL),
		chromedp.WaitReady("body", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("Failed to navigate: %v", err)
	}
	time.Sleep(3 * time.Second)

	// LLM-клиент
	l, err := llm.NewOpenAIClient()
	if err != nil {
		t.Fatalf("Failed to init LLM: %v", err)
	}

	aiAgent := agent.NewAgent(b, l)

	log.Printf("🤖 AGENT STARTED with task: '%s'", userTask)
	err = aiAgent.Run(userTask, maxSteps)

	if err != nil {
		t.Errorf("Agent finished with error: %v", err)
	} else {
		t.Log("✅ Agent completed the task successfully!")
	}
}
