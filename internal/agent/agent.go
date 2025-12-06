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

	// Память шагов: история + защита от циклов и повторяющихся паттернов.
	// maxLines=8, loopThreshold=3 → одно и то же действие больше 3 раз подряд запрещаем,
	// плюс детект паттернов из двух действий.
	mem := NewStepMemory(8, 3)

	for step := 1; step <= maxSteps; step++ {
		fmt.Printf("\n--- STEP %d ---\n", step)

		state := playwright.LoadState(browser.LoadStateNetworkidle)
		a.browser.Page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
			State: &state,
		})

		snapshot, err := a.browser.Snapshot()
		if err != nil {
			return fmt.Errorf("snapshot failed: %w", err)
		}

		fmt.Printf("URL: %s\nTitle: %s\n", snapshot.URL, snapshot.Title)

		preview := snapshot.Tree
		if len(preview) > 500 {
			preview = preview[:500] + "..."
		}
		fmt.Printf("Tree preview:\n%s\n", preview)

		histStr := mem.HistoryString()

		// --------- Динамическое уточнение задачи (phase control) ---------
		// Никаких правок system prompt — только user-task.
		effectiveTask := task

		taskLower := strings.ToLower(task)
		wantsCart :=
			strings.Contains(taskLower, "корзин") || // рус. "корзина"
				strings.Contains(taskLower, "cart") ||
				strings.Contains(taskLower, "basket") ||
				strings.Contains(taskLower, "checkout") ||
				strings.Contains(taskLower, "sepet") // тур. "sepet", "sepete"

		hasDialog := strings.Contains(snapshot.Tree, `context="dialog"`)

		// Если уже была попытка повторяющегося паттерна (loop guard сработал),
		// пользователь явно просил "перейти в корзину",
		// и при этом мы не в модалке — пора перестать думать про "добавление"
		// и сфокусироваться на переходе в корзину/checkout.
		if wantsCart && mem.LoopTriggered() && !hasDialog {
			effectiveTask = task + `
			
NOTE: It looks like the requested item has already been added or the "add to cart"
action was repeated in a loop. From now on, DO NOT try to add the same item again.
Focus ONLY on opening the cart/basket/checkout page and proceeding there.`
		}
		// -----------------------------------------------------------------

		decision, err := a.llm.DecideAction(llm.DecisionInput{
			Task:       effectiveTask,
			DOMTree:    snapshot.Tree,
			CurrentURL: snapshot.URL,
			History:    histStr,
		})
		if err != nil {
			return fmt.Errorf("llm error: %w", err)
		}

		fmt.Printf("\n🤖 THOUGHT: %s\n", decision.Thought)
		fmt.Printf("⚡ ACTION: %s [%d] %q\n",
			decision.Action.Type, decision.Action.TargetID, decision.Action.Text)

		// Жёсткая защита от зацикливания:
		// если модель снова хочет воспроизвести тот же action или тот же паттерн,
		// мы его не выполняем и добавляем SYSTEM NOTE в историю.
		if blocked, reason := mem.ShouldBlock(snapshot.URL, decision.Action); blocked {
			fmt.Printf("⛔ LOOP GUARD: suppressing action %s on target %d\n",
				decision.Action.Type, decision.Action.TargetID)
			if reason != "" {
				fmt.Println(reason)
				mem.AddSystemNote(reason)
			}
			// отмечаем, что защита от цикла уже срабатывала —
			// это потом используем для смены "фазы" (добавление -> переход в корзину)
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
			// в память попадают только успешно выполненные действия
			mem.Add(step, snapshot.URL, decision.Action)
		}

		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("max steps reached")
}

func (a *Agent) executeAction(reader *bufio.Reader, action llm.Action) error {
	// навигацию по URL не делаем вообще
	if action.Type == llm.ActionNavigate {
		fmt.Println("⚠ navigate action is disabled, ignoring (navigation must be via clicks).")
		return nil
	}

	selector := fmt.Sprintf("[data-ai-id='%d']", action.TargetID)

	// немного дебага — по какому элементу идём кликать/печатать
	if action.Type == llm.ActionClick || action.Type == llm.ActionTypeInput {
		htmlAny, _ := a.browser.Page.Evaluate(
			`(sel) => {
				const el = document.querySelector(sel);
				if (!el) return null;
				let s = el.outerHTML || "";
				if (s.length > 400) s = s.slice(0, 400) + "...";
				return s;
			}`,
			selector,
		)
		if htmlAny != nil {
			if htmlStr, ok := htmlAny.(string); ok && htmlStr != "" {
				fmt.Printf("🔍 DEBUG element for %s:\n%s\n", selector, htmlStr)
			} else {
				fmt.Printf("🔍 DEBUG element for %s: <nil or non-string>\n", selector)
			}
		} else {
			fmt.Printf("🔍 DEBUG element for %s: not found\n", selector)
		}
	}

	if action.Type == llm.ActionClick || action.Type == llm.ActionTypeInput {
		a.highlight(selector)
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
