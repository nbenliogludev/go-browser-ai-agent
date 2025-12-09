package main

import (
	"log"
	"os"
	"testing"
	"time"

	"github.com/nbenliogludev/go-browser-ai-agent/internal/agent"
	"github.com/nbenliogludev/go-browser-ai-agent/internal/browser"
	"github.com/nbenliogludev/go-browser-ai-agent/internal/llm"
	"github.com/playwright-community/playwright-go"
)

func TestGetirPizza(t *testing.T) {
	targetURL := "https://getir.com/yemek/"
	userTask := "Закажи пиццу маргарита не в качестве меню"
	maxSteps := 15

	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Fatal("OPENAI_API_KEY is not set")
	}

	log.Println("🚀 STARTING AUTOMATED TEST...")

	b, err := browser.NewManager()
	if err != nil {
		t.Fatalf("Failed to init browser: %v", err)
	}
	defer b.Close()

	log.Printf("Navigating to %s...", targetURL)
	if _, err := b.Page.Goto(targetURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(60000),
	}); err != nil {
		t.Fatalf("Failed to navigate: %v", err)
	}
	time.Sleep(3 * time.Second)

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
