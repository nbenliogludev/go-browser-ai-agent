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
)

func main() {
	fmt.Println("Starting AI browser agent (execution loop)...")

	mgr, err := browser.NewManager()
	if err != nil {
		log.Fatalf("failed to start browser manager: %v", err)
	}
	defer mgr.Close()

	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Введите стартовый URL (пусто = https://example.com): ")
	rawURL, _ := reader.ReadString('\n')
	url := strings.TrimSpace(rawURL)
	if url == "" {
		url = "https://example.com"
	}

	_, err = mgr.Page.Goto(url)
	if err != nil {
		log.Fatalf("could not navigate to %s: %v", url, err)
	}

	fmt.Print("Опишите задачу для агента (например: 'Найди кнопку входа и нажми её'):\n> ")
	rawTask, _ := reader.ReadString('\n')
	task := strings.TrimSpace(rawTask)
	if task == "" {
		log.Fatalf("пустая задача — нечего решать")
	}

	llmClient, err := llm.NewOpenAIClient()
	if err != nil {
		log.Fatalf("failed to create OpenAI client: %v", err)
	}

	ag := agent.NewAgent(mgr, llmClient)

	if err := ag.Run(task, 15); err != nil {
		log.Printf("Агент завершил работу с ошибкой: %v", err)
	} else {
		log.Printf("Агент завершил работу без ошибок.")
	}

	fmt.Println("\nНажмите Enter, чтобы закрыть браузер...")
	_, _ = reader.ReadString('\n')
}
