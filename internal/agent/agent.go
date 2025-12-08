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
	mem := NewStepMemory(8, 3)

	for step := 1; step <= maxSteps; step++ {
		fmt.Printf("\n--- STEP %d ---\n", step)

		// 1. Очистка старых маркеров перед снимком
		a.clearHighlights()

		state := playwright.LoadState(browser.LoadStateNetworkidle)
		a.browser.Page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
			State:   &state,
			Timeout: playwright.Float(4000),
		})

		// 2. Снимок (передаем step для сохранения скриншота)
		snapshot, err := a.browser.Snapshot(step)
		if err != nil {
			return fmt.Errorf("snapshot failed: %w", err)
		}

		fmt.Printf("URL: %s\nTitle: %s\n", snapshot.URL, snapshot.Title)

		// Показываем превью дерева (первые 800 символов)
		preview := snapshot.Tree
		if len(preview) > 800 {
			preview = preview[:800] + "..."
		}
		fmt.Printf("Tree preview:\n%s\n", preview)

		// 3. Решение LLM
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

		// Формируем красивую строку действия
		actionStr := fmt.Sprintf("%s [%d]", decision.Action.Type, decision.Action.TargetID)
		if decision.Action.Text != "" {
			actionStr += fmt.Sprintf(" \"%s\"", decision.Action.Text)
		}
		fmt.Printf("⚡ ACTION: %s\n", actionStr)

		// --- SECURITY LAYER INTERCEPTOR (ROBUST FIX) ---
		if decision.Action.IsDestructive {
			fmt.Println("\n" + strings.Repeat("!", 50))
			fmt.Printf("🛡️  SECURITY ALERT: Sensitive action detected!\n")
			fmt.Printf("Reason: %s\n", decision.Action.DestructiveReason)
			fmt.Printf("Action: %s\n", actionStr)

			// Подсвечиваем элемент, чтобы пользователь видел, что подтверждает
			if decision.Action.TargetID > 0 {
				selector := fmt.Sprintf("[data-ai-id='%d']", decision.Action.TargetID)
				a.highlight(selector)
			}

			// ВАЖНО: Сброс буфера ввода.
			// Читаем всё, что накопилось в stdin, пока буфер не станет пустым.
			// Это предотвращает ложное срабатывание от старых нажатий Enter.
			for {
				if reader.Buffered() == 0 {
					break
				}
				_, _ = reader.ReadByte()
			}

			fmt.Print(">>> ALLOW this action? (type 'y' and Enter): ")

			// Читаем строку целиком (блокируемся, пока юзер не нажмет Enter)
			text, _ := reader.ReadString('\n')

			// Убираем пробелы и переносы строк с обоих концов
			answer := strings.TrimSpace(strings.ToLower(text))

			// Убираем подсветку
			a.clearHighlights()

			if answer != "y" && answer != "yes" {
				fmt.Println("❌ Action BLOCKED by user.")
				// Записываем отказ в память, чтобы LLM знала и не пыталась снова
				mem.AddSystemNote(fmt.Sprintf("USER BLOCKED: Action '%s' was denied by user.", actionStr))
				time.Sleep(1 * time.Second)
				continue // Пропускаем выполнение действия, идем на следующий шаг
			}
			fmt.Println("✅ Action APPROVED.")
		}
		// -----------------------------------------------

		// 4. Loop Guard (Защита от зацикливания)
		if blocked, reason := mem.ShouldBlock(snapshot.URL, decision.Action); blocked {
			fmt.Printf("⛔ LOOP GUARD: %s\n", reason)
			fmt.Println("🔄 Auto-fix: Scrolling down to break loop...")
			// Скроллим страницу, чтобы изменить контекст
			a.browser.Page.Evaluate(`window.scrollBy({top: 300, behavior: 'smooth'});`)
			mem.MarkLoopTriggered()
			time.Sleep(2 * time.Second)
			continue
		}

		// 5. Завершение задачи
		if decision.Action.Type == llm.ActionFinish {
			fmt.Println("✅ Task completed!")
			return nil
		}

		// 6. Выполнение действия
		if err := a.executeAction(reader, decision.Action); err != nil {
			log.Printf("Action failed: %v", err)
		} else {
			mem.Add(step, snapshot.URL, decision.Action)
		}

		// Пауза перед следующим шагом для прогрузки интерфейса
		time.Sleep(3 * time.Second)
	}

	return fmt.Errorf("max steps reached")
}

func (a *Agent) executeAction(reader *bufio.Reader, action llm.Action) error {
	// Скролл
	if action.Type == llm.ActionScroll {
		fmt.Println("📜 Scrolling down...")
		_, err := a.browser.Page.Evaluate(`window.scrollBy({top: 500, behavior: 'smooth'});`)
		time.Sleep(1 * time.Second)
		return err
	}

	selector := fmt.Sprintf("[data-ai-id='%d']", action.TargetID)

	// Визуальная подсветка перед кликом/вводом
	if action.Type == llm.ActionClick || action.Type == llm.ActionTypeInput {
		a.highlight(selector)
		time.Sleep(300 * time.Millisecond)
		a.clearHighlights() // Сразу убираем, чтобы не мешать клику
	}

	switch action.Type {
	case llm.ActionClick:
		fmt.Printf("Clicking %s...\n", selector)
		locator := a.browser.Page.Locator(selector).First()

		// 1. Попытка скролла к элементу
		_ = locator.ScrollIntoViewIfNeeded()

		// 2. Стандартный клик (Playwright)
		err := locator.Click(playwright.LocatorClickOptions{
			Force:   playwright.Bool(true),
			Timeout: playwright.Float(3000),
		})

		// 3. NUCLEAR OPTION: JS Click Fallback
		// Если стандартный клик не сработал (элемент перекрыт, не ловит фокус и т.д.),
		// вызываем нативный .click() через JS.
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
          // Используем box-shadow, чтобы не ломать верстку
          el.style.boxShadow = "inset 0 0 0 4px red";
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
