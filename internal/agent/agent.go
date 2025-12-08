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

		// 2. Снимок
		snapshot, err := a.browser.Snapshot(step)
		if err != nil {
			return fmt.Errorf("snapshot failed: %w", err)
		}

		fmt.Printf("URL: %s\nTitle: %s\n", snapshot.URL, snapshot.Title)

		// Превью дерева
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

		// --- Sanity-check ID против текущего DOM ---
		if (decision.Action.Type == llm.ActionClick || decision.Action.Type == llm.ActionTypeInput) &&
			decision.Action.TargetID != 0 {

			needle := fmt.Sprintf("[%d]", decision.Action.TargetID)
			if !strings.Contains(snapshot.Tree, needle) {
				log.Printf("⚠️ target_id=%d not found in DOM summary, converting to scroll\n", decision.Action.TargetID)
				decision.Action.Type = llm.ActionScroll
				decision.Action.TargetID = 0
				decision.Action.Text = ""
			}
		}
		// -------------------------------------------

		actionStr := fmt.Sprintf("%s [%d]", decision.Action.Type, decision.Action.TargetID)
		if decision.Action.Text != "" {
			actionStr += fmt.Sprintf(" \"%s\"", decision.Action.Text)
		}
		fmt.Printf("⚡ ACTION: %s\n", actionStr)

		// --- SECURITY LAYER INTERCEPTOR ---
		if decision.Action.IsDestructive {
			fmt.Println("\n" + strings.Repeat("!", 50))
			fmt.Printf("🛡️  SECURITY ALERT: Sensitive action detected!\n")
			fmt.Printf("Reason: %s\n", decision.Action.DestructiveReason)
			fmt.Printf("Action: %s\n", actionStr)

			if decision.Action.TargetID > 0 {
				selector := fmt.Sprintf("[data-ai-id='%d']", decision.Action.TargetID)
				a.highlight(selector)
			}

			// Очистка буфера ввода
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
				mem.AddSystemNote(fmt.Sprintf("USER BLOCKED: Action '%s' was denied by user.", actionStr))
				time.Sleep(1 * time.Second)
				continue
			}
			fmt.Println("✅ Action APPROVED.")
		}
		// ----------------------------------

		// 4. Loop Guard
		if blocked, reason := mem.ShouldBlock(snapshot.URL, decision.Action); blocked {
			fmt.Printf("⛔ LOOP GUARD: %s\n", reason)
			fmt.Println("🔄 Auto-fix: Scrolling down to break loop...")

			// Скроллим и окно, и модалку
			a.browser.Page.Evaluate(`() => {
				window.scrollBy({top: 300, behavior: 'smooth'});
				const modal = document.querySelector('[role="dialog"], .modal, .popup, [data-testid="modal"]');
				if (modal) modal.scrollBy({top: 300, behavior: 'smooth'});
			}`)

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
		_, err := a.browser.Page.Evaluate(`() => {
            window.scrollBy({top: 500, behavior: 'smooth'});
            const modal = document.querySelector('[role="dialog"], .modal, .popup, [data-testid="modal"]');
            if (modal) {
                modal.scrollBy({top: 500, behavior: 'smooth'});
            }
        }`)
		time.Sleep(1 * time.Second)
		return err
	}

	selector := fmt.Sprintf("[data-ai-id='%d']", action.TargetID)

	if action.Type == llm.ActionClick || action.Type == llm.ActionTypeInput {
		a.highlight(selector)
		time.Sleep(300 * time.Millisecond)
		a.clearHighlights()
	}

	switch action.Type {
	case llm.ActionClick:
		fmt.Printf("Clicking %s...\n", selector)
		locator := a.browser.Page.Locator(selector).First()

		_ = locator.ScrollIntoViewIfNeeded()

		// 1. Стандартный клик (попытка)
		err := locator.Click(playwright.LocatorClickOptions{
			Force:   playwright.Bool(true),
			Timeout: playwright.Float(2000),
		})

		// 2. FALLBACK STRATEGIES
		if err != nil {
			fmt.Printf("⚠️ Standard click failed (%v). Trying Fallbacks...\n", err)

			// Fallback A: JS Handle Click (если элемент найден по ID, но не кликается)
			handle, hErr := locator.ElementHandle()
			if hErr == nil {
				fmt.Println("🔧 Executing JS Event Dispatch on Element Handle...")
				_, jsErr := handle.Evaluate(`el => {
                    el.click();
                    const opts = {bubbles: true, cancelable: true, view: window};
                    el.dispatchEvent(new MouseEvent('mousedown', opts));
                    el.dispatchEvent(new MouseEvent('mouseup', opts));
                }`, nil)
				return jsErr
			}

			// Fallback B: Text Search Click (если элемент НЕ найден по ID - он исчез/обновился)
			fmt.Printf("⚠️ Element not found by ID. Searching by TEXT '%s'...\n", action.Text)
			if action.Text != "" {
				// Экранируем кавычки для JS
				safeText := strings.ReplaceAll(action.Text, "'", "\\'")

				// Ищем кнопку по тексту (contains) и кликаем
				jsScript := fmt.Sprintf(`() => {
                    const targets = Array.from(document.querySelectorAll('button, a, [role="button"], div[style*="cursor: pointer"]'));
                    const found = targets.find(el => el.innerText.includes('%s') || el.textContent.includes('%s'));
                    
                    if (found) {
                        found.click();
                        const opts = {bubbles: true, cancelable: true, view: window};
                        found.dispatchEvent(new MouseEvent('mousedown', opts));
                        found.dispatchEvent(new MouseEvent('mouseup', opts));
                        return true;
                    }
                    return false;
                }`, safeText, safeText)

				res, jsErr2 := a.browser.Page.Evaluate(jsScript)
				if jsErr2 == nil && res == true {
					fmt.Println("✅ Fallback click by TEXT successful!")
					return nil
				}
			}

			return fmt.Errorf("failed to click element by ID and by Text")
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
