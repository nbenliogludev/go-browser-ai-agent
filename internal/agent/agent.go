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

func (a *Agent) Run(task string, maxSteps int) error {
	mem := NewStepMemory(10, 3)
	var prevSnapshot *browser.PageSnapshot

	// Для security layer — читаем ответы пользователя из stdin
	reader := bufio.NewReader(os.Stdin)

	for step := 1; step <= maxSteps; step++ {
		fmt.Printf("\n--- STEP %d ---\n", step)

		// 1. Делаем снимок
		snapshot, err := a.browser.Snapshot(step)
		if err != nil {
			return fmt.Errorf("snapshot failed: %w", err)
		}

		// Проверка на No-Op
		if prevSnapshot != nil && snapshot.Tree == prevSnapshot.Tree {
			mem.AddSystemNote("SYSTEM ALERT: Last action had NO VISIBLE EFFECT.")
		}

		fmt.Printf("URL: %s\nTitle: %s\n", snapshot.URL, snapshot.Title)

		preview := snapshot.Tree
		if len(preview) > 800 {
			preview = preview[:800] + "..."
		}
		fmt.Printf("Tree preview:\n%s\n", preview)

		// 2. Спрашиваем LLM
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

		// Логирование
		fmt.Println("\n" + strings.Repeat("-", 40))
		fmt.Printf("🧠 PHASE:       %s\n", strings.ToUpper(decision.CurrentPhase))
		fmt.Printf("👀 OBSERVATION: %s\n", decision.Observation)
		fmt.Printf("🤖 THOUGHT:     %s\n", decision.Thought)

		destructiveMark := ""
		if decision.Action.IsDestructive {
			destructiveMark = " [DESTRUCTIVE]"
		}
		fmt.Printf("⚡ ACTION:      %s [%d] %q%s\n",
			decision.Action.Type,
			decision.Action.TargetID,
			decision.Action.Text,
			destructiveMark,
		)
		fmt.Println(strings.Repeat("-", 40))

		// Loop Guard
		if blocked, reason := mem.ShouldBlock(snapshot.URL, decision.Action); blocked {
			fmt.Printf("⛔ LOOP GUARD: %s\n", reason)
			_ = chromedp.Run(a.browser.Ctx, chromedp.Evaluate(`window.scrollBy({top: 300, behavior: 'smooth'});`, nil))
			mem.MarkLoopTriggered()
			time.Sleep(2 * time.Second)
			continue
		}

		if decision.Action.Type == llm.ActionFinish {
			fmt.Println("✅ Task completed!")
			return nil
		}

		// 2.5. SECURITY LAYER: подтверждение деструктивных действий
		if decision.Action.IsDestructive {
			fmt.Println("⚠️ SECURITY LAYER: модель предлагает ДЕСТРУКТИВНОЕ действие (оплата, удаление и т.п.).")
			fmt.Printf("   Planned action: %s [%d] %q\n",
				decision.Action.Type,
				decision.Action.TargetID,
				decision.Action.Text,
			)
			fmt.Print("   Разрешить это действие? (y/n): ")

			answer, _ := reader.ReadString('\n')
			answer = strings.TrimSpace(strings.ToLower(answer))

			// Принимаем несколько вариантов «да», чтобы не промахнуться по раскладке:
			// y / yes / да / д / e / evet → разрешить
			allow := answer == "y" ||
				answer == "yes" ||
				answer == "да" ||
				answer == "д" ||
				answer == "e" ||
				answer == "evet"

			if !allow {
				fmt.Println("🚫 Destructive action cancelled by user.")
				mem.AddSystemNote("USER CANCELLED DESTRUCTIVE ACTION. Agent must choose a safer or alternative action.")
				// Не выполняем действие, переходим к следующему шагу цикла.
				time.Sleep(1 * time.Second)
				continue
			}

			fmt.Println("✅ User approved destructive action, executing...")
		}

		// 3. Выполнение действия
		if err := a.executeAction(decision.Action, snapshot); err != nil {
			log.Printf("Action failed: %v", err)
			mem.AddSystemNote(fmt.Sprintf("SYSTEM ERROR: %v", err))
		} else {
			mem.Add(step, snapshot.URL, decision.Action)
			mem.AddSystemNote(fmt.Sprintf("STATE UPDATE: %s | %s", decision.CurrentPhase, decision.Observation))
			prevSnapshot = snapshot
		}

		time.Sleep(3 * time.Second)
	}

	return fmt.Errorf("max steps reached")
}

func (a *Agent) executeAction(action llm.Action, snap *browser.PageSnapshot) error {
	// Скролл — без target_id
	if action.Type == llm.ActionScroll {
		fmt.Println("📜 Scrolling down...")
		return chromedp.Run(
			a.browser.Ctx,
			chromedp.Evaluate(`window.scrollBy({top: 500, behavior: 'smooth'});`, nil),
		)
	}

	if action.TargetID == 0 {
		return nil
	}

	// 1. Находим BackendNodeID по нашему внутреннему ID
	backendNodeID, found := snap.Elements[action.TargetID]
	if !found {
		return fmt.Errorf("TargetID %d not found in elements map", action.TargetID)
	}

	fmt.Printf("🎯 Targeting BackendNodeID: %d\n", backendNodeID)

	// 2. Выполнение через CDP
	return chromedp.Run(a.browser.Ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		// dom.ResolveNode().Do возвращает *runtime.RemoteObject
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
			// На всякий случай — ничего не делаем
			return nil
		}

		return err
	}))
}
