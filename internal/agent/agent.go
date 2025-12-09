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
	mem := NewStepMemory(10, 3) // Увеличили память, чтобы держать контекст фаз

	var prevSnapshot *browser.PageSnapshot
	var prevAction *llm.Action

	for step := 1; step <= maxSteps; step++ {
		fmt.Printf("\n--- STEP %d ---\n", step)

		// 1. Очистка визуальных маркеров
		a.clearHighlights()

		// 2. Ждем стабилизации сети (но не падаем, если таймаут)
		if err := a.browser.Page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
			State:   playwright.LoadStateNetworkidle,
			Timeout: playwright.Float(4000),
		}); err != nil {
			// Лог, но продолжаем — современные сайты (SPA) редко бывают полностью idle
			// log.Printf("Network idle wait timeout (proceeding anyway)")
		}

		// 3. Снимок страницы (использует новую логику Snapshot с Modal Focus)
		snapshot, err := a.browser.Snapshot(step)
		if err != nil {
			return fmt.Errorf("snapshot failed: %w", err)
		}

		// Проверка на "залипание" (No-Op): если URL и DOM не меняются
		if prevSnapshot != nil && prevAction != nil {
			if isNoOpTransition(prevSnapshot, snapshot) {
				note := fmt.Sprintf(
					"SYSTEM ALERT: Last action '%s' had NO VISIBLE EFFECT. The page looks identical. Mark this approach as FAILED and try a different element or strategy.",
					formatAction(*prevAction),
				)
				mem.AddSystemNote(note)
			}
		}

		fmt.Printf("URL: %s\nTitle: %s\n", snapshot.URL, snapshot.Title)

		// Показываем превью дерева в консоль (для отладки)
		preview := snapshot.Tree
		if len(preview) > 800 {
			preview = preview[:800] + "..."
		}
		fmt.Printf("Tree preview:\n%s\n", preview)

		// 4. Запрос к LLM (с передачей истории фаз и наблюдений)
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

		// 5. Логирование "Мыслей" и "Состояния" (Chain of Thought)
		fmt.Println("\n" + strings.Repeat("-", 40))
		fmt.Printf("🧠 PHASE:       %s\n", strings.ToUpper(decision.CurrentPhase))
		fmt.Printf("👀 OBSERVATION: %s\n", decision.Observation)
		fmt.Printf("🤖 THOUGHT:     %s\n", decision.Thought)

		actionStr := formatAction(decision.Action)
		fmt.Printf("⚡ ACTION:      %s\n", actionStr)
		fmt.Println(strings.Repeat("-", 40))

		// --- SECURITY LAYER ---
		if decision.Action.IsDestructive {
			fmt.Println("\n" + strings.Repeat("!", 50))
			fmt.Printf("🛡️  SECURITY ALERT: Sensitive action detected!\n")
			fmt.Printf("Reason: %s\n", decision.Action.DestructiveReason)

			// Подсвечиваем опасный элемент
			if decision.Action.TargetID > 0 {
				selector := fmt.Sprintf("[data-ai-id='%d']", decision.Action.TargetID)
				a.highlight(selector)
			}

			// Чистим буфер ввода
			for {
				if reader.Buffered() == 0 {
					break
				}
				_, _ = reader.ReadByte()
			}

			fmt.Print(">>> ALLOW this action? (type 'y' and Enter): ")
			text, _ := reader.ReadString('\n')
			answer := strings.TrimSpace(strings.ToLower(text))
			a.clearHighlights()

			if answer != "y" && answer != "yes" {
				fmt.Println("❌ Action BLOCKED by user.")
				mem.AddSystemNote(fmt.Sprintf("USER BLOCKED: Action '%s' was denied.", actionStr))
				time.Sleep(1 * time.Second)
				continue
			}
			fmt.Println("✅ Action APPROVED.")
		}

		// 6. Loop Guard (Защита от циклов)
		if blocked, reason := mem.ShouldBlock(snapshot.URL, decision.Action); blocked {
			fmt.Printf("⛔ LOOP GUARD: %s\n", reason)
			fmt.Println("🔄 Auto-fix: Scrolling down to change context...")
			a.browser.Page.Evaluate(`window.scrollBy({top: 300, behavior: 'smooth'});`)
			mem.MarkLoopTriggered()
			time.Sleep(2 * time.Second)
			continue
		}

		// 7. Завершение
		if decision.Action.Type == llm.ActionFinish {
			fmt.Println("✅ Task completed!")
			return nil
		}

		// 8. Выполнение действия
		if err := a.executeAction(reader, decision.Action); err != nil {
			log.Printf("Action failed: %v", err)
			// Добавляем ошибку в память, чтобы LLM знала
			mem.AddSystemNote(fmt.Sprintf("SYSTEM ERROR: Action failed execution: %v", err))
		} else {
			// ВАЖНО: Сохраняем не только действие, но и КОНТЕКСТ (Фазу и Наблюдение)
			// Это позволяет LLM помнить "Я уже добавил товар" на следующем шаге.
			mem.Add(step, snapshot.URL, decision.Action)

			// Добавляем мета-информацию в историю
			contextNote := fmt.Sprintf("STATE UPDATE: Phase=%s | Observation=%s", decision.CurrentPhase, decision.Observation)
			mem.AddSystemNote(contextNote)

			prevSnapshot = snapshot
			actionCopy := decision.Action
			prevAction = &actionCopy
		}

		// Пауза для отработки JS и анимаций сайта
		time.Sleep(4 * time.Second)
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

	// Подсветка перед действием
	if action.Type == llm.ActionClick || action.Type == llm.ActionTypeInput {
		a.highlight(selector)
		time.Sleep(300 * time.Millisecond) // Короткая пауза для визуализации
		a.clearHighlights()
	}

	switch action.Type {
	case llm.ActionClick:
		fmt.Printf("Clicking %s...\n", selector)
		locator := a.browser.Page.Locator(selector).First()

		// Пытаемся проскроллить к элементу
		_ = locator.ScrollIntoViewIfNeeded()

		// Playwright Click
		err := locator.Click(playwright.LocatorClickOptions{
			Force:   playwright.Bool(true),
			Timeout: playwright.Float(3000), // Быстрый тайм-аут для попытки
		})

		// Fallback: JS Click (если элемент перекрыт или Playwright не может кликнуть)
		if err != nil {
			fmt.Printf("⚠️ Click failed (%v). Trying JS Click fallback...\n", err)
			_, jsErr := a.browser.Page.Evaluate(fmt.Sprintf(`
				const el = document.querySelector("%s");
				if (el) { 
					el.click(); 
				} else { 
					throw new Error('Element not found in DOM'); 
				}
			`, selector))
			return jsErr
		}
		return nil

	case llm.ActionTypeInput:
		fmt.Printf("Typing '%s' into %s...\n", action.Text, selector)
		locator := a.browser.Page.Locator(selector).First()
		_ = locator.ScrollIntoViewIfNeeded()

		// 1. Очистка поля (важно для React-форм)
		if err := locator.Fill(""); err != nil {
			return fmt.Errorf("failed to clear input: %w", err)
		}

		// 2. Ввод текста
		if err := locator.Fill(action.Text); err != nil {
			return fmt.Errorf("failed to fill input: %w", err)
		}

		// 3. Обработка Submit / Autocomplete
		if action.Submit {
			return a.browser.Page.Press(selector, "Enter")
		} else {
			// Если Submit не нужен, значит это автокомплит (поиск).
			// Ждем чуть-чуть, чтобы JS отработал и показал выпадающий список.
			fmt.Println("⏳ Waiting 1.5s for autocomplete/dropdown...")
			time.Sleep(1500 * time.Millisecond)
		}
		return nil

	case llm.ActionFinish:
		return nil

	default:
		return fmt.Errorf("unknown action: %s", action.Type)
	}
}

func (a *Agent) highlight(selector string) {
	// Безопасный JS без backticks внутри строки
	script := fmt.Sprintf(`
        const el = document.querySelector("%s");
        if (el) {
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

func formatAction(a llm.Action) string {
	s := fmt.Sprintf("%s [%d]", a.Type, a.TargetID)
	if a.Text != "" {
		s += fmt.Sprintf(" \"%s\"", a.Text)
	}
	return s
}

func isNoOpTransition(prev, cur *browser.PageSnapshot) bool {
	if prev == nil || cur == nil {
		return false
	}
	if prev.URL != cur.URL {
		return false
	}
	// Эвристика: если длина DOM почти не изменилась и начало дерева совпадает
	if abs(len(prev.Tree)-len(cur.Tree)) < 50 && len(prev.Tree) > 500 && len(cur.Tree) > 500 {
		// Сравниваем первые 500 символов
		if prev.Tree[:500] == cur.Tree[:500] {
			return true
		}
	}
	return false
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
