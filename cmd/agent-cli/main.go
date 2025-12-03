package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/nbenliogludev/go-browser-ai-agent/internal/browser"
	"github.com/nbenliogludev/go-browser-ai-agent/internal/llm"
)

func main() {
	fmt.Println("Starting AI browser agent (LLM decision demo)...")

	mgr, err := browser.NewManager()
	if err != nil {
		log.Fatalf("failed to start browser manager: %v", err)
	}
	defer mgr.Close()

	reader := bufio.NewReader(os.Stdin)

	// 1. URL
	fmt.Print("Введите URL (пусто = https://example.com): ")
	rawURL, _ := reader.ReadString('\n')
	url := strings.TrimSpace(rawURL)
	if url == "" {
		url = "https://example.com"
	}

	_, err = mgr.Page.Goto(url)
	if err != nil {
		log.Fatalf("could not navigate to %s: %v", url, err)
	}

	fmt.Println("Страница загружена, получаю DOM-снапшот...")

	snapshot, err := mgr.Snapshot(50)
	if err != nil {
		log.Fatalf("could not snapshot page: %v", err)
	}

	fmt.Printf("URL:   %s\n", snapshot.URL)
	fmt.Printf("Title: %s\n", snapshot.Title)
	fmt.Printf("Найдено элементов: %d\n\n", len(snapshot.Elements))

	// 2. Задача пользователя
	fmt.Print("Опишите задачу для агента (например: 'Найди и нажми кнопку логина'):\n> ")
	rawTask, _ := reader.ReadString('\n')
	task := strings.TrimSpace(rawTask)
	if task == "" {
		log.Fatalf("пустая задача — нечего решать")
	}

	// 3. Инициализируем LLM-клиента
	cli, err := llm.NewOpenAIClient()
	if err != nil {
		log.Fatalf("failed to create OpenAI client: %v", err)
	}

	decision, err := cli.DecideAction(llm.DecisionInput{
		Task:     task,
		Snapshot: snapshot,
	})
	if err != nil {
		log.Fatalf("LLM decision error: %v", err)
	}

	fmt.Println("\nLLM decision:")
	fmt.Printf("Thought: %s\n", decision.Thought)
	fmt.Printf("Action:\n")
	fmt.Printf("  Type:      %s\n", decision.Action.Type)
	fmt.Printf("  TargetID:  %s\n", decision.Action.TargetID)
	fmt.Printf("  Text:      %s\n", decision.Action.Text)
	fmt.Printf("  URL:       %s\n", decision.Action.URL)

	fmt.Println("\n(Пока это только dry-run: агент ещё не нажимает/не вводит, просто принимает решение.)")
	fmt.Println("Нажмите Enter, чтобы закрыть браузер...")
	_, _ = reader.ReadString('\n')
}
