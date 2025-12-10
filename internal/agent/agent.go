package agent

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/nbenliogludev/go-browser-ai-agent/internal/browser"
	"github.com/nbenliogludev/go-browser-ai-agent/internal/llm"
)

type Agent struct {
	browser *browser.Manager
	llm     llm.Client
}

func NewAgent(b *browser.Manager, c llm.Client) *Agent {
	return &Agent{browser: b, llm: c}
}

// ---------- Структуры и хелперы для финального отчёта ----------

type stepReport struct {
	Step          int
	URL           string
	Phase         string
	Observation   string
	Thought       string
	ActionSummary string
}

func formatActionSummary(a llm.Action) string {
	return fmt.Sprintf(
		"%s target=%d text=%q destructive=%v",
		string(a.Type),
		a.TargetID,
		a.Text,
		a.IsDestructive,
	)
}

func printReport(task string, steps []stepReport) {
	if len(steps) == 0 {
		return
	}

	fmt.Println("\n===== AGENT REPORT =====")
	fmt.Printf("Task: %s\n", task)
	fmt.Printf("Total steps: %d\n\n", len(steps))

	for _, s := range steps {
		fmt.Printf("Step %d:\n", s.Step)
		fmt.Printf("  URL:    %s\n", s.URL)
		if s.Phase != "" {
			fmt.Printf("  Phase:  %s\n", s.Phase)
		}
		if s.Observation != "" {
			fmt.Printf("  Obs:    %s\n", s.Observation)
		}
		if s.Thought != "" {
			fmt.Printf("  Thought:%s\n", s.Thought)
		}
		fmt.Printf("  Action: %s\n\n", s.ActionSummary)
	}
	fmt.Println("===== END OF REPORT =====")
}

// ------------------------------ Run ------------------------------

func (a *Agent) Run(task string, maxSteps int) error {
	mem := NewStepMemory(10, 3)
	var prevSnapshot *browser.PageSnapshot

	// Копим шаги для финального отчёта
	var reportSteps []stepReport

	for step := 1; step <= maxSteps; step++ {
		fmt.Printf("\n--- STEP %d ---\n", step)

		// 1. Снимок страницы
		snapshot, err := a.browser.Snapshot(step)
		if err != nil {
			printReport(task, reportSteps)
			return fmt.Errorf("snapshot failed: %w", err)
		}

		// No-op detection
		if prevSnapshot != nil && snapshot.Tree == prevSnapshot.Tree {
			mem.AddSystemNote("SYSTEM ALERT: Last action had NO VISIBLE EFFECT.")
		}

		fmt.Printf("URL: %s\nTitle: %s\n", snapshot.URL, snapshot.Title)

		preview := snapshot.Tree
		if len(preview) > 800 {
			preview = preview[:800] + "..."
		}
		fmt.Printf("Tree preview:\n%s\n", preview)

		// 2. Решение модели
		decision, err := a.llm.DecideAction(llm.DecisionInput{
			Task:             task,
			DOMTree:          snapshot.Tree,
			CurrentURL:       snapshot.URL,
			History:          mem.HistoryString(),
			ScreenshotBase64: snapshot.ScreenshotBase64,
		})
		if err != nil {
			printReport(task, reportSteps)
			return fmt.Errorf("llm error: %w", err)
		}

		// Логирование решения
		decor := ""
		if decision.Action.IsDestructive {
			decor = " [DESTRUCTIVE]"
		}

		fmt.Println("\n" + strings.Repeat("-", 40))
		fmt.Printf("🧠 PHASE:       %s\n", strings.ToUpper(decision.CurrentPhase))
		fmt.Printf("👀 OBSERVATION: %s\n", decision.Observation)
		fmt.Printf("🤖 THOUGHT:     %s\n", decision.Thought)
		fmt.Printf("⚡ ACTION:      %s [%d] %q%s\n",
			decision.Action.Type,
			decision.Action.TargetID,
			decision.Action.Text,
			decor,
		)
		fmt.Println(strings.Repeat("-", 40))

		// Базовое описание действия для отчёта
		reportActionSummary := formatActionSummary(decision.Action)

		// Loop Guard
		if blocked, reason := mem.ShouldBlock(snapshot.URL, decision.Action); blocked {
			fmt.Printf("⛔ LOOP GUARD: %s\n", reason)

			reportSteps = append(reportSteps, stepReport{
				Step:          step,
				URL:           snapshot.URL,
				Phase:         decision.CurrentPhase,
				Observation:   decision.Observation,
				Thought:       decision.Thought,
				ActionSummary: reportActionSummary + " [BLOCKED BY LOOP GUARD]",
			})

			_ = chromedp.Run(a.browser.Ctx,
				chromedp.Evaluate(`window.scrollBy({top: 300, behavior: 'smooth'});`, nil),
			)
			mem.MarkLoopTriggered()
			time.Sleep(2 * time.Second)
			continue
		}

		// FINISH
		if decision.Action.Type == llm.ActionFinish {
			reportSteps = append(reportSteps, stepReport{
				Step:          step,
				URL:           snapshot.URL,
				Phase:         decision.CurrentPhase,
				Observation:   decision.Observation,
				Thought:       decision.Thought,
				ActionSummary: reportActionSummary + " [FINISH]",
			})

			printReport(task, reportSteps)
			fmt.Println("✅ Task completed!")
			return nil
		}

		// 3. Выполнение действия (с учётом security-layer)
		if err := a.executeAction(decision.Action, snapshot); err != nil {
			log.Printf("Action failed: %v", err)
			mem.AddSystemNote(fmt.Sprintf("SYSTEM ERROR: %v", err))
			reportActionSummary = reportActionSummary + " [ERROR: " + err.Error() + "]"
		} else {
			mem.Add(step, snapshot.URL, decision.Action)
			mem.AddSystemNote(fmt.Sprintf("STATE UPDATE: %s | %s", decision.CurrentPhase, decision.Observation))
			prevSnapshot = snapshot
		}

		// Добавляем шаг в отчёт
		reportSteps = append(reportSteps, stepReport{
			Step:          step,
			URL:           snapshot.URL,
			Phase:         decision.CurrentPhase,
			Observation:   decision.Observation,
			Thought:       decision.Thought,
			ActionSummary: reportActionSummary,
		})

		time.Sleep(3 * time.Second)
	}

	printReport(task, reportSteps)
	return fmt.Errorf("max steps reached")
}

// --------------------------- Actions ----------------------------

func (a *Agent) executeAction(action llm.Action, snap *browser.PageSnapshot) error {
	// Скролл – отдельный путь
	if action.Type == llm.ActionScroll {
		fmt.Println("📜 Scrolling down...")
		return chromedp.Run(
			a.browser.Ctx,
			chromedp.Evaluate(`window.scrollBy({top: 500, behavior: 'smooth'});`, nil),
		)
	}

	// Защита: без targetID для клика / ввода – ничего не делаем
	if action.TargetID == 0 {
		return nil
	}

	// SECURITY LAYER для деструктивных действий
	if action.IsDestructive {
		if !confirmDestructiveAction(action) {
			// Пользователь запретил – считаем, что шага не было (ошибкой не считаем)
			return nil
		}
	}

	// 1. BackendNodeID по нашему внутреннему ID
	backendNodeID, found := snap.Elements[action.TargetID]
	if !found {
		return fmt.Errorf("TargetID %d not found in elements map", action.TargetID)
	}

	fmt.Printf("🎯 Targeting BackendNodeID: %d\n", backendNodeID)

	// 2. Выполнение через CDP
	return chromedp.Run(a.browser.Ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		obj, err := dom.ResolveNode().
			WithBackendNodeID(backendNodeID).
			Do(ctx)
		if err != nil {
			return fmt.Errorf("resolve node failed: %w", err)
		}

		if obj == nil || obj.ObjectID == "" {
			return fmt.Errorf("object id is empty (node might be detached)")
		}

		remoteObjectID := obj.ObjectID

		switch action.Type {
		case llm.ActionClick:
			_, _, err = runtime.CallFunctionOn(`function() { 
				this.scrollIntoViewIfNeeded();
				this.click(); 
			}`).
				WithObjectID(remoteObjectID).
				Do(ctx)

		case llm.ActionTypeInput:
			script := fmt.Sprintf(`function() { 
				this.scrollIntoViewIfNeeded();
				this.value = "";
				this.value = "%s";
				this.dispatchEvent(new Event('input', { bubbles: true }));
				this.dispatchEvent(new Event('change', { bubbles: true }));
			}`, action.Text)

			_, _, err = runtime.CallFunctionOn(script).
				WithObjectID(remoteObjectID).
				Do(ctx)

			if action.Submit && err == nil {
				_ = dom.Focus().
					WithBackendNodeID(backendNodeID).
					Do(ctx)
				_ = chromedp.SendKeys("", "\r").Do(ctx)
			}

		default:
			// Если тип действия незнаком – просто ничего не делаем
			return nil
		}

		return err
	}))
}

// ---------------------- Security layer -------------------------

// confirmDestructiveAction – security-слой для опасных действий (оплата, удаление и т.п.)
func confirmDestructiveAction(action llm.Action) bool {
	fmt.Printf("⚠️ SECURITY LAYER: модель предлагает ДЕСТРУКТИВНОЕ действие (оплата, удаление и т.п.).\n")
	fmt.Printf("   Planned action: %s [%d] %q\n", action.Type, action.TargetID, action.Text)
	fmt.Print("   Разрешить это действие? (y/n): ")

	// Пытаемся читать прямо из терминала, а не из stdin теста
	tty, err := os.Open("/dev/tty")
	if err != nil {
		// Нет TTY (например, CI) – безопасно отменяем
		fmt.Println(" (no TTY, auto-cancel)")
		fmt.Println("🚫 Destructive action cancelled (no interactive TTY).")
		return false
	}
	defer tty.Close()

	reader := bufio.NewReader(tty)

	for {
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("\n🚫 Destructive action cancelled (read error).")
			return false
		}

		answer := strings.ToLower(strings.TrimSpace(input))

		if answer == "y" || answer == "yes" || answer == "д" {
			fmt.Println("✅ Destructive action approved by user.")
			return true
		}

		if answer == "n" || answer == "no" || answer == "н" || answer == "" {
			fmt.Println("🚫 Destructive action cancelled by user.")
			return false
		}

		fmt.Print("   Please answer 'y' or 'n': ")
	}
}
