package agent

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/nbenliogludev/go-browser-ai-agent/internal/browser"
	"github.com/nbenliogludev/go-browser-ai-agent/internal/llm"
	"github.com/playwright-community/playwright-go"
)

type Agent struct {
	browser *browser.Manager
	llm     llm.Client
}

func NewAgent(b *browser.Manager, c llm.Client) *Agent {
	return &Agent{browser: b, llm: c}
}

func (a *Agent) Run(task string, maxSteps int) error {
	reader := bufio.NewReader(os.Stdin)

	for step := 1; step <= maxSteps; step++ {
		fmt.Printf("\n--- STEP %d ---\n", step)

		// ИСПРАВЛЕНИЕ:
		// Мы явно создаем переменную типа playwright.LoadState из строки.
		// Это устраняет путаницу с типами констант и указателей.
		networkIdle := playwright.LoadState("networkidle")

		a.browser.Page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
			State: &networkIdle, // Теперь это гарантированно *LoadState
		})

		// Снимаем снимок
		snapshot, err := a.browser.Snapshot()
		if err != nil {
			return fmt.Errorf("snapshot failed: %w", err)
		}

		fmt.Printf("URL: %s\nTitle: %s\n", snapshot.URL, snapshot.Title)

		// Для отладки
		preview := snapshot.Tree
		if len(preview) > 500 {
			preview = preview[:500] + "..."
		}
		fmt.Printf("Tree preview:\n%s\n", preview)

		// 2. Спрашиваем LLM
		decision, err := a.llm.DecideAction(llm.DecisionInput{
			Task:       task,
			DOMTree:    snapshot.Tree,
			CurrentURL: snapshot.URL,
		})
		if err != nil {
			return fmt.Errorf("llm error: %w", err)
		}

		fmt.Printf("\n🤖 THOUGHT: %s\n", decision.Thought)
		fmt.Printf("⚡ ACTION: %s [%d] %q\n", decision.Action.Type, decision.Action.TargetID, decision.Action.Text)

		if decision.Action.Type == llm.ActionFinish {
			fmt.Println("✅ Task completed!")
			return nil
		}

		// 3. Выполняем действие
		if err := a.executeAction(reader, decision.Action); err != nil {
			log.Printf("Action failed: %v. Retrying...", err)
			// Не выходим, даем LLM шанс исправиться
		}

		// Пауза
		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("max steps reached")
}

func (a *Agent) executeAction(reader *bufio.Reader, action llm.Action) error {
	// Получаем селектор по ID
	selector := fmt.Sprintf("[data-ai-id='%d']", action.TargetID)

	switch action.Type {
	case llm.ActionClick:
		fmt.Printf("Clicking %s...\n", selector)
		if err := a.browser.Page.Locator(selector).First().ScrollIntoViewIfNeeded(); err != nil {
			return fmt.Errorf("scroll failed: %w", err)
		}
		return a.browser.Page.Click(selector)

	case llm.ActionTypeInput:
		fmt.Printf("Typing '%s' into %s (Submit=%v)...\n", action.Text, selector, action.Submit)

		// 1. Очищаем и вводим текст
		if err := a.browser.Page.Fill(selector, action.Text); err != nil {
			return err
		}

		// 2. Если флаг Submit=true, жмем Enter
		if action.Submit {
			fmt.Println("👉 Pressing ENTER...")
			return a.browser.Page.Press(selector, "Enter")
		}
		return nil

	case llm.ActionNavigate:
		// ... (без изменений)
		fmt.Printf("Navigating to %s...\n", action.URL)
		_, err := a.browser.Page.Goto(action.URL)
		return err

	case llm.ActionFinish:
		return nil

	default:
		return fmt.Errorf("unknown action type: %s", action.Type)
	}
}

func askConfirmation(reader *bufio.Reader, msg string) bool {
	fmt.Print(msg + " [y/N]: ")
	res, _ := reader.ReadString('\n')
	return strings.TrimSpace(strings.ToLower(res)) == "y"
}
