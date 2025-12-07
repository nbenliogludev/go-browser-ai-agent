package e2e

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nbenliogludev/go-browser-ai-agent/internal/agent"
	"github.com/nbenliogludev/go-browser-ai-agent/internal/browser"
	"github.com/nbenliogludev/go-browser-ai-agent/internal/llm"
	"github.com/nbenliogludev/go-browser-ai-agent/internal/planner"
)

// Этот тест реальный: он открывает Chromium, идёт на GetirYemek и
// просит оркестратор добавить маргариту среднего/любого размера в корзину,
// затем перейти в корзину.
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

	// 3. Planner-клиент
	plannerClient, err := planner.NewOpenAIPlanner()
	if err != nil {
		t.Fatalf("failed to init planner client: %v", err)
	}

	// 4. Открываем стартовый URL (как в main.go)
	if _, err := mgr.Page.Goto("https://getir.com/yemek/"); err != nil {
		t.Fatalf("failed to open start url: %v", err)
	}
	// Небольшая пауза, чтобы страница стабилизировалась
	time.Sleep(5 * time.Second)

	// 5. Создаём оркестратор (как в CLI orchestrator mode)
	orch := agent.NewOrchestrator(mgr, plannerClient, llmClient)

	// Можно уточнить, что задача на текущем сайте:
	task := "добавь одну пиццу маргариту среднего размера в корзину затем перейди в корзину"

	// 6. Запускаем сценарий
	if err := orch.Run(task, 35); err != nil {
		t.Fatalf("orchestrator finished with error: %v", err)
	}

	// 7. Проверяем, что корзина, скорее всего, не пустая.
	// Ищем в дереве слова вида "Sepet", "Sepeti" и т.п.
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
