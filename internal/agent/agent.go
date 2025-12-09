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

	var prevSnapshot *browser.PageSnapshot
	var prevAction *llm.Action

	for step := 1; step <= maxSteps; step++ {
		fmt.Printf("\n--- STEP %d ---\n", step)

		a.clearHighlights()

		if err := a.browser.Page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
			State:   playwright.LoadStateNetworkidle,
			Timeout: playwright.Float(4000),
		}); err != nil {
			// Игнорируем таймаут ожидания, идем дальше
		}

		snapshot, err := a.browser.Snapshot(step)
		if err != nil {
			return fmt.Errorf("snapshot failed: %w", err)
		}

		if prevSnapshot != nil && prevAction != nil {
			if isNoOpTransition(prevSnapshot, snapshot) {
				note := fmt.Sprintf(
					"SYSTEM: Last action '%s' had NO VISIBLE EFFECT. Mark it as FAILED.",
					formatAction(*prevAction),
				)
				mem.AddSystemNote(note)
			}
		}

		fmt.Printf("URL: %s\nTitle: %s\n", snapshot.URL, snapshot.Title)

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
		actionStr := formatAction(decision.Action)
		fmt.Printf("⚡ ACTION: %s\n", actionStr)

		if decision.Action.IsDestructive {
			fmt.Println("\n" + strings.Repeat("!", 50))
			fmt.Printf("🛡️  SECURITY ALERT: Sensitive action detected!\n")
			fmt.Printf("Reason: %s\n", decision.Action.DestructiveReason)

			if decision.Action.TargetID > 0 {
				selector := fmt.Sprintf("[data-ai-id='%d']", decision.Action.TargetID)
				a.highlight(selector)
			}

			// Clear buffer
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
				mem.AddSystemNote(fmt.Sprintf("USER BLOCKED: Action '%s' denied.", actionStr))
				time.Sleep(1 * time.Second)
				continue
			}
			fmt.Println("✅ Action APPROVED.")
		}

		if blocked, reason := mem.ShouldBlock(snapshot.URL, decision.Action); blocked {
			fmt.Printf("⛔ LOOP GUARD: %s\n", reason)
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
			prevSnapshot = snapshot
			actionCopy := decision.Action
			prevAction = &actionCopy
		}

		// Пауза чуть больше, чтобы успели открыться поп-апы
		time.Sleep(4 * time.Second)
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

		err := locator.Click(playwright.LocatorClickOptions{
			Force:   playwright.Bool(true),
			Timeout: playwright.Float(3000),
		})

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

		if err := locator.Fill(""); err != nil {
			return fmt.Errorf("failed to clear input: %w", err)
		}

		if err := locator.Fill(action.Text); err != nil {
			return fmt.Errorf("failed to type text: %w", err)
		}

		if action.Submit {
			return a.browser.Page.Press(selector, "Enter")
		} else {
			fmt.Println("⏳ Waiting 1s for autocomplete/dropdown...")
			time.Sleep(1 * time.Second)
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
	if abs(len(prev.Tree)-len(cur.Tree)) < 50 && prev.Tree[:min(500, len(prev.Tree))] == cur.Tree[:min(500, len(cur.Tree))] {
		return true
	}
	return false
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
