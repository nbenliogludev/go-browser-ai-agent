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

	// Память шагов: история + защита от циклов.
	mem := NewStepMemory(8, 3)

	for step := 1; step <= maxSteps; step++ {
		fmt.Printf("\n--- STEP %d ---\n", step)

		// Ждем NetworkIdle, но не слишком жестко, чтобы не висеть на стриминге
		// Можно изменить на LoadStateDomcontentloaded для скорости
		state := playwright.LoadState(browser.LoadStateNetworkidle)
		tryWait := func() {
			a.browser.Page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
				State:   &state,
				Timeout: playwright.Float(4000), // Не ждем вечно
			})
		}
		tryWait()

		snapshot, err := a.browser.Snapshot()
		if err != nil {
			return fmt.Errorf("snapshot failed: %w", err)
		}

		fmt.Printf("URL: %s\nTitle: %s\n", snapshot.URL, snapshot.Title)

		preview := snapshot.Tree
		if len(preview) > 500 {
			preview = preview[:500] + "..."
		}
		fmt.Printf("Tree preview (Viewport only):\n%s\n", preview)

		histStr := mem.HistoryString()

		// Динамическое уточнение задачи
		effectiveTask := task
		taskLower := strings.ToLower(task)
		wantsCart :=
			strings.Contains(taskLower, "корзин") ||
				strings.Contains(taskLower, "cart") ||
				strings.Contains(taskLower, "basket") ||
				strings.Contains(taskLower, "checkout") ||
				strings.Contains(taskLower, "sepet")

		// Если агент тупит и хочет корзину, но продолжает добавлять — даем подсказку
		if wantsCart && mem.LoopTriggered() {
			effectiveTask = task + `

NOTE: If the item is already added, STOP adding it. Focus ONLY on finding the cart/basket icon in the header or footer.`
		}

		decision, err := a.llm.DecideAction(llm.DecisionInput{
			Task:             effectiveTask,
			DOMTree:          snapshot.Tree,
			CurrentURL:       snapshot.URL,
			History:          histStr,
			ScreenshotBase64: snapshot.ScreenshotBase64,
		})
		if err != nil {
			return fmt.Errorf("llm error: %w", err)
		}

		fmt.Printf("\n🤖 THOUGHT: %s\n", decision.Thought)
		fmt.Printf("⚡ ACTION: %s [%d] %q\n",
			decision.Action.Type, decision.Action.TargetID, decision.Action.Text)

		// Жёсткая защита от зацикливания
		if blocked, reason := mem.ShouldBlock(snapshot.URL, decision.Action); blocked {
			fmt.Printf("⛔ LOOP GUARD: suppressing action %s on target %d\n",
				decision.Action.Type, decision.Action.TargetID)
			if reason != "" {
				mem.AddSystemNote(reason)
			}
			mem.MarkLoopTriggered()

			time.Sleep(1 * time.Second)
			continue
		}

		if decision.Action.Type == llm.ActionFinish {
			fmt.Println("✅ Task completed!")
			return nil
		}

		if err := a.executeAction(reader, decision.Action); err != nil {
			log.Printf("Action failed: %v. Retrying...", err)
		} else {
			mem.Add(step, snapshot.URL, decision.Action)
		}

		// Пауза, чтобы сайт успел отреагировать
		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("max steps reached")
}

func (a *Agent) executeAction(reader *bufio.Reader, action llm.Action) error {
	if action.Type == llm.ActionNavigate {
		fmt.Println("⚠ navigate action is disabled, ignoring.")
		return nil
	}

	selector := fmt.Sprintf("[data-ai-id='%d']", action.TargetID)

	// Подсветка и дебаг
	if action.Type == llm.ActionClick || action.Type == llm.ActionTypeInput {
		a.highlight(selector)
		time.Sleep(300 * time.Millisecond)
	}

	switch action.Type {
	case llm.ActionClick:
		fmt.Printf("Clicking %s...\n", selector)
		locator := a.browser.Page.Locator(selector).First()

		// Пытаемся проскроллить
		_ = locator.ScrollIntoViewIfNeeded()

		// Force: true позволяет нажимать, даже если элемент перекрыт (например, прозрачным оверлеем)
		return locator.Click(playwright.LocatorClickOptions{
			Force:   playwright.Bool(true),
			Timeout: playwright.Float(5000),
		})

	case llm.ActionTypeInput:
		fmt.Printf("Typing '%s' into %s (Submit=%v)...\n", action.Text, selector, action.Submit)
		// Для инпутов тоже полезно проскроллить
		locator := a.browser.Page.Locator(selector).First()
		_ = locator.ScrollIntoViewIfNeeded()

		if err := locator.Fill(action.Text); err != nil {
			return err
		}
		if action.Submit {
			fmt.Println("👉 Pressing ENTER...")
			return a.browser.Page.Press(selector, "Enter")
		}
		return nil

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
          el.style.outline = "4px solid red";
          el.style.zIndex = "2147483647"; // Max z-index
       }
    `, selector)
	_, _ = a.browser.Page.Evaluate(script)
}
