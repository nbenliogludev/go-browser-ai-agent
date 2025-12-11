package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/chromedp/chromedp"

	"github.com/nbenliogludev/go-browser-ai-agent/internal/agent"
	"github.com/nbenliogludev/go-browser-ai-agent/internal/browser"
	"github.com/nbenliogludev/go-browser-ai-agent/internal/llm"
)

func main() {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Starting browser agent...")

	fmt.Print("Enter start URL (empty = https://example.com): ")
	startURL, _ := reader.ReadString('\n')
	startURL = strings.TrimSpace(startURL)
	if startURL == "" {
		startURL = "https://example.com"
	}

	fmt.Println("Describe the task for the agent (for example: 'Find the login button and click it'):")
	fmt.Print("> ")
	rawTask, _ := reader.ReadString('\n')
	rawTask = strings.TrimSpace(rawTask)
	if rawTask == "" {
		log.Fatal("Empty task — nothing for the agent to do.")
	}

	task := agent.BuildTaskWithEnvironment(rawTask, startURL)

	bm := browser.NewManager()
	defer bm.Close()

	if err := chromedp.Run(bm.Ctx, chromedp.Navigate(startURL)); err != nil {
		log.Fatalf("Failed to open start URL %s: %v", startURL, err)
	}

	llmClient, err := llm.NewOpenAIClient()
	if err != nil {
		log.Fatalf("Failed to initialize LLM client: %v", err)
	}

	rhythmi := agent.NewAgent(bm, llmClient)

	const maxSteps = 40
	if err := rhythmi.Run(task, maxSteps); err != nil {
		log.Printf("Agent finished with error: %v", err)
	}

	fmt.Println("\nPress Enter to close the browser...")
	_, _ = reader.ReadString('\n')
}
