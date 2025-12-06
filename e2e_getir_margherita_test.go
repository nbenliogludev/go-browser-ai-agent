package e2e

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nbenliogludev/go-browser-ai-agent/internal/agent"
	"github.com/nbenliogludev/go-browser-ai-agent/internal/browser"
	"github.com/nbenliogludev/go-browser-ai-agent/internal/llm"
)

// Этот тест реальный: он открывает Chromium, идёт на getir и
// просит агента добавить первую попавшуюся маргариту среднего размера.
func TestGetir_MargheritaMedium(t *testing.T) {
	// Чтобы не запускалось случайно в CI или при go test ./...
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set — пропускаю e2e-тест")
	}

	// 1. Поднимаем браузер
	mgr, err := browser.NewManager()
	if err != nil {
		t.Fatalf("failed to init browser: %v", err)
	}
	defer mgr.Close()

	// 2. LLM-клиент (использует OPENAI_API_KEY из env)
	llmClient, err := llm.NewOpenAIClient()
	if err != nil {
		t.Fatalf("failed to init llm client: %v", err)
	}

	// 3. Агент (тот же, что использует agent-cli)
	a := agent.NewAgent(mgr, llmClient)

	// 4. Открываем стартовый URL
	if _, err := mgr.Page.Goto("https://getir.com/yemek/"); err != nil {
		t.Fatalf("failed to open start url: %v", err)
	}
	// Небольшая пауза, чтобы страница стабилизировалась
	time.Sleep(5 * time.Second)

	// 5. Запускаем сценарий
	task := "добавь одну пиццу маргариту в корзину затем перейди в корзину"
	if err := a.Run(task, 25); err != nil {
		t.Fatalf("agent finished with error: %v", err)
	}

	// 6. Проверяем, что корзина, скорее всего, не пустая.
	// Это простой, но практичный чек: в дереве должен появиться текст
	// вида "Sepette", "Sepeti Gör" или просто "Sepet".
	snap, err := mgr.Snapshot()
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}

	tree := snap.Tree
	lower := strings.ToLower(tree)

	if !strings.Contains(lower, "sepet") && !strings.Contains(lower, "sepeti") {
		// Если не нашли — падаем и показываем кусок дерева, чтобы дебажить.
		preview := tree
		if len(preview) > 2000 {
			preview = preview[:2000] + "...(truncated)"
		}
		t.Fatalf("expected cart to contain something, but cart keywords not found.\nURL: %s\nTree preview:\n%s",
			snap.URL, preview)
	}
}
