package agent

import (
	"bufio"
	"fmt"
	"log"
	"os"
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
	mem := NewStepMemory(8, 3)

	for step := 1; step <= maxSteps; step++ {
		fmt.Printf("\n--- STEP %d ---\n", step)

		// Очистка маркеров перед снимком
		a.clearHighlights()

		state := playwright.LoadState(browser.LoadStateNetworkidle)
		a.browser.Page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
			State:   &state,
			Timeout: playwright.Float(4000),
		})

		// FIX: Передаем step в Snapshot
		snapshot, err := a.browser.Snapshot(step)
		if err != nil {
			return fmt.Errorf("snapshot failed: %w", err)
		}

		fmt.Printf("URL: %s\nTitle: %s\n", snapshot.URL, snapshot.Title)

		// Показываем кусок дерева для контроля
		preview := snapshot.Tree
		if len(preview) > 800 {
			preview = preview[:800] + "..."
		}
		fmt.Printf("Tree preview:\n%s\n", preview)

		decision, err := a.llm.DecideAction(llm.DecisionInput{
			Task:             task,
			DOMTree:          snapshot.Tree,
			CurrentURL:       snapshot.URL,
			History:          mem.HistoryString(),
			ScreenshotBase64: snapshot.ScreenshotBase64,
		})
		if err != nil {
			return fmt.Errorf("llm error: %w", err)
		}

		fmt.Printf("\n🤖 THOUGHT: %s\n", decision.Thought)
		fmt.Printf("⚡ ACTION: %s [%d] %q\n",
			decision.Action.Type, decision.Action.TargetID, decision.Action.Text)

		if blocked, reason := mem.ShouldBlock(snapshot.URL, decision.Action); blocked {
			fmt.Printf("⛔ LOOP GUARD: %s\n", reason)
			// Если застряли — скроллим страницу, часто это помогает найти новые элементы
			fmt.Println("🔄 Auto-fix: Scrolling down to break loop...")
			a.browser.Page.Evaluate(`window.scrollBy({top: 300, behavior: 'smooth'});`)
			mem.MarkLoopTriggered()
			time.Sleep(2 * time.Second)
			continue
		}

		if decision.Action.Type == llm.ActionFinish {
			fmt.Println("✅ Task completed!")
			return nil
		}

		if err := a.executeAction(reader, decision.Action); err != nil {
			log.Printf("Action failed: %v", err)
		} else {
			mem.Add(step, snapshot.URL, decision.Action)
		}

		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("max steps reached")
}

func (a *Agent) executeAction(reader *bufio.Reader, action llm.Action) error {
	if action.Type == llm.ActionScroll {
		fmt.Println("📜 Scrolling down...")
		_, err := a.browser.Page.Evaluate(`window.scrollBy({top: 500, behavior: 'smooth'});`)
		time.Sleep(1 * time.Second)
		return err
	}

	selector := fmt.Sprintf("[data-ai-id='%d']", action.TargetID)

	// Подсветка (визуально для тебя)
	if action.Type == llm.ActionClick || action.Type == llm.ActionTypeInput {
		a.highlight(selector)
		time.Sleep(300 * time.Millisecond)
		a.clearHighlights() // Убираем сразу, чтобы не мешать клику
	}

	switch action.Type {
	case llm.ActionClick:
		fmt.Printf("Clicking %s...\n", selector)
		locator := a.browser.Page.Locator(selector).First()

		// 1. Попытка скролла к элементу
		_ = locator.ScrollIntoViewIfNeeded()

		// 2. Стандартный клик с Force (игнорирует некоторые проверки)
		err := locator.Click(playwright.LocatorClickOptions{
			Force:   playwright.Bool(true),
			Timeout: playwright.Float(3000),
		})

		// 3. NUCLEAR OPTION: JS Click.
		// Если Playwright не смог (например, элемент перекрыт попапом, или это div без role),
		// мы вызываем .click() через JS. Это работает в 99% случаев в сложных SPA.
		if err != nil {
			fmt.Printf("⚠️ Click failed (%v). Trying JS Click fallback...\n", err)
			_, jsErr := a.browser.Page.Evaluate(fmt.Sprintf(`
             const el = document.querySelector("%s");
             if (el) { el.click(); } else { throw new Error('Element not found'); }
          `, selector))
			return jsErr
		}
		return nil

	case llm.ActionTypeInput:
		fmt.Printf("Typing '%s' into %s...\n", action.Text, selector)
		locator := a.browser.Page.Locator(selector).First()
		_ = locator.ScrollIntoViewIfNeeded()

		if err := locator.Fill(action.Text); err != nil {
			return err
		}
		if action.Submit {
			return a.browser.Page.Press(selector, "Enter")
		}
		return nil

	case llm.ActionFinish:
		return nil

	default:
		return fmt.Errorf("unknown action: %s", action.Type)
	}
}

func (a *Agent) highlight(selector string) {
	script := fmt.Sprintf(`
       const el = document.querySelector("%s");
       if (el) {
          el.style.boxShadow = "inset 0 0 0 4px red"; // box-shadow не ломает верстку как border
          el.setAttribute('data-ai-highlight', 'true');
       }
    `, selector)
	_, _ = a.browser.Page.Evaluate(script)
}

func (a *Agent) clearHighlights() {
	_, _ = a.browser.Page.Evaluate(`() => {
       document.querySelectorAll('[data-ai-highlight]').forEach(el => {
          el.style.boxShadow = '';
          el.removeAttribute('data-ai-highlight');
       });
    }`)
}
