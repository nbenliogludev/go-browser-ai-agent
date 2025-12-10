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

	fmt.Println("Starting Rhythmi browser agent...")

	fmt.Print("Введите стартовый URL (пусто = https://example.com): ")
	startURL, _ := reader.ReadString('\n')
	startURL = strings.TrimSpace(startURL)
	if startURL == "" {
		startURL = "https://example.com"
	}

	fmt.Println("Опишите задачу для агента (например: 'Найди кнопку входа и нажми её'):")
	fmt.Print("> ")
	rawTask, _ := reader.ReadString('\n')
	rawTask = strings.TrimSpace(rawTask)
	if rawTask == "" {
		log.Fatal("Пустая задача — агенту нечего делать.")
	}

	task := agent.BuildTaskWithEnvironment(rawTask, startURL)

	bm := browser.NewManager()
	defer bm.Close()

	if err := chromedp.Run(bm.Ctx, chromedp.Navigate(startURL)); err != nil {
		log.Fatalf("Не удалось открыть стартовый URL %s: %v", startURL, err)
	}

	llmClient, err := llm.NewOpenAIClient()
	if err != nil {
		log.Fatalf("Ошибка инициализации LLM клиента: %v", err)
	}

	rhythmi := agent.NewAgent(bm, llmClient)

	const maxSteps = 40
	if err := rhythmi.Run(task, maxSteps); err != nil {
		log.Printf("Агент завершил работу с ошибкой: %v", err)
	}

	fmt.Println("\nНажмите Enter, чтобы закрыть браузер...")
	_, _ = reader.ReadString('\n')
}
