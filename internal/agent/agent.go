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

		// 1. Ждем стабилизации сети
		// FIX: Явно создаем переменную типа LoadState из строки "networkidle"
		state := playwright.LoadState("networkidle")
		a.browser.Page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
			State: &state, // Теперь передаем корректный указатель *LoadState
		})

		// 2. Делаем снимок
		snapshot, err := a.browser.Snapshot()
		if err != nil {
			return fmt.Errorf("snapshot failed: %w", err)
		}

		fmt.Printf("URL: %s\nTitle: %s\n", snapshot.URL, snapshot.Title)

		// Показываем превью дерева
		preview := snapshot.Tree
		if len(preview) > 500 {
			preview = preview[:500] + "..."
		}
		fmt.Printf("Tree preview:\n%s\n", preview)

		// 3. Спрашиваем LLM
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

		// 4. Выполняем действие
		if err := a.executeAction(reader, decision.Action); err != nil {
			log.Printf("Action failed: %v. Retrying...", err)
		}

		// Пауза между шагами
		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("max steps reached")
}

func (a *Agent) executeAction(reader *bufio.Reader, action llm.Action) error {
	selector := fmt.Sprintf("[data-ai-id='%d']", action.TargetID)

	if action.Type == llm.ActionClick || action.Type == llm.ActionTypeInput {
		a.highlight(selector)
		// Небольшая пауза, чтобы вы успели увидеть подсветку
		time.Sleep(500 * time.Millisecond)
	}

	switch action.Type {
	case llm.ActionClick:
		fmt.Printf("Clicking %s...\n", selector)
		if err := a.browser.Page.Locator(selector).First().ScrollIntoViewIfNeeded(); err != nil {
			return fmt.Errorf("scroll failed: %w", err)
		}
		return a.browser.Page.Click(selector)

	case llm.ActionTypeInput:
		fmt.Printf("Typing '%s' into %s (Submit=%v)...\n", action.Text, selector, action.Submit)
		if err := a.browser.Page.Fill(selector, action.Text); err != nil {
			return err
		}
		if action.Submit {
			fmt.Println("👉 Pressing ENTER...")
			return a.browser.Page.Press(selector, "Enter")
		}
		return nil

	case llm.ActionNavigate:
		fmt.Printf("Navigating to %s...\n", action.URL)
		_, err := a.browser.Page.Goto(action.URL)
		return err

	case llm.ActionFinish:
		return nil

	default:
		return fmt.Errorf("unknown action type: %s", action.Type)
	}
}

func (a *Agent) highlight(selector string) {
	script := fmt.Sprintf(`
		const el = document.querySelector("%s");
		if (el) {
			el.style.outline = "5px solid red";
			el.style.zIndex = "999999";
			el.scrollIntoView({behavior: "smooth", block: "center", inline: "center"});
		}
	`, selector)
	_, _ = a.browser.Page.Evaluate(script)
}

func askConfirmation(reader *bufio.Reader, msg string) bool {
	fmt.Print(msg + " [y/N]: ")
	res, _ := reader.ReadString('\n')
	return strings.TrimSpace(strings.ToLower(res)) == "y"
}
