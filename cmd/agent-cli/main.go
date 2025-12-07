package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/nbenliogludev/go-browser-ai-agent/internal/agent"
	"github.com/nbenliogludev/go-browser-ai-agent/internal/browser"
	"github.com/nbenliogludev/go-browser-ai-agent/internal/llm"
	"github.com/nbenliogludev/go-browser-ai-agent/internal/planner"
)

func main() {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Starting AI browser agent (orchestrator mode)...")

	// 1) Начальный URL
	fmt.Print("Введите стартовый URL (пусто = https://example.com): ")
	startURL, _ := reader.ReadString('\n')
	startURL = strings.TrimSpace(startURL)
	if startURL == "" {
		startURL = "https://example.com"
	}

	// 2) Пользовательская задача
	fmt.Println("Опишите задачу для агента (например: 'Найди кнопку входа и нажми её'):")
	fmt.Print("> ")
	rawTask, _ := reader.ReadString('\n')
	rawTask = strings.TrimSpace(rawTask)
	if rawTask == "" {
		log.Fatal("Пустая задача — агенту нечего делать.")
	}

	// 🔴 Обогащаем задачу контекстом про стартовый URL
	task := agent.BuildTaskWithEnvironment(rawTask, startURL)

	// 3) Браузер
	bm, err := browser.NewManager()
	if err != nil {
		log.Fatalf("Не удалось запустить браузер: %v", err)
	}
	defer bm.Close()

	// 4) Открываем стартовый URL
	if _, err := bm.Page.Goto(startURL); err != nil {
		log.Fatalf("Не удалось открыть стартовый URL %s: %v", startURL, err)
	}

	// 5) LLM client
	llmClient, err := llm.NewOpenAIClient()
	if err != nil {
		log.Fatalf("Ошибка инициализации LLM клиента: %v", err)
	}

	// 6) Planner client
	plannerClient, err := planner.NewOpenAIPlanner()
	if err != nil {
		log.Fatalf("Ошибка инициализации planner клиента: %v", err)
	}

	// 7) Orchestrator (planner + navigator + interaction)
	orch := agent.NewOrchestrator(bm, plannerClient, llmClient)

	// 8) Запускаем оркестратор
	const maxSteps = 30
	if err := orch.Run(task, maxSteps); err != nil {
		log.Printf("Агент завершил работу с ошибкой: %v", err)
	}

	fmt.Println("\nНажмите Enter, чтобы закрыть браузер...")
	_, _ = reader.ReadString('\n')
}
