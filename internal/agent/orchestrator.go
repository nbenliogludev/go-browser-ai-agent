package agent

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/nbenliogludev/go-browser-ai-agent/internal/browser"
	"github.com/nbenliogludev/go-browser-ai-agent/internal/llm"
	"github.com/nbenliogludev/go-browser-ai-agent/internal/planner"
	"github.com/playwright-community/playwright-go"
)

type Orchestrator struct {
	browser     *browser.Manager
	planner     planner.Client
	navigator   SubAgent
	interaction SubAgent
}

func NewOrchestrator(b *browser.Manager, p planner.Client, llmClient llm.Client) *Orchestrator {
	return &Orchestrator{
		browser:     b,
		planner:     p,
		navigator:   &NavigatorAgent{LLM: llmClient},
		interaction: &InteractionAgent{LLM: llmClient},
	}
}

// Run — основной цикл оркестратора.
// Всегда печатает финальный репорт по действиям (успех или ошибка).
func (o *Orchestrator) Run(task string, maxSteps int) (err error) {
	reader := bufio.NewReader(os.Stdin)
	ctx := context.Background()

	// Shared step memory (loop protection) for both sub-agents.
	mem := NewStepMemory(10, 3)

	// Финальный репорт по итогам работы
	defer func() {
		fmt.Println("\n===== EXECUTION REPORT =====")
		lines := mem.FullHistory()
		if len(lines) == 0 {
			fmt.Println("(no actions recorded)")
		} else {
			for _, l := range lines {
				fmt.Println(l)
			}
		}

		if err != nil {
			fmt.Printf("\nFINAL STATUS: ERROR: %v\n", err)
		} else {
			fmt.Println("\nFINAL STATUS: SUCCESS")
		}
	}()

	// 0. Build high-level plan
	plan, err := o.planner.BuildPlan(ctx, task)
	if err != nil {
		return fmt.Errorf("build plan failed: %w", err)
	}

	fmt.Println("📋 PLAN:")
	for _, s := range plan.Steps {
		fmt.Printf("  %d. [%s] %s\n", s.Index, s.Mode, s.Goal)
	}

	currentPlanStep := 0
	// Сколько раз луп-гард блокировал действия в рамках конкретного шага плана
	loopBlocksPerStep := make(map[int]int)

	for step := 1; step <= maxSteps; step++ {
		fmt.Printf("\n--- STEP %d (plan %d/%d) ---\n",
			step, currentPlanStep+1, len(plan.Steps))

		state := playwright.LoadState(browser.LoadStateNetworkidle)
		o.browser.Page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
			State: &state,
		})

		snapshot, snapErr := o.browser.Snapshot()
		if snapErr != nil {
			err = fmt.Errorf("snapshot failed: %w", snapErr)
			return err
		}

		fmt.Printf("URL: %s\nTitle: %s\n", snapshot.URL, snapshot.Title)
		preview := snapshot.Tree
		if len(preview) > 500 {
			preview = preview[:500] + "..."
		}
		fmt.Printf("Tree preview:\n%s\n", preview)

		if currentPlanStep >= len(plan.Steps) {
			fmt.Println("✅ All plan steps processed — finishing.")
			return nil
		}

		stepGoal := plan.Steps[currentPlanStep]

		// Если видна модалка - принудительно interaction-режим.
		hasDialog := strings.Contains(snapshot.Tree, `context="dialog"`) ||
			strings.Contains(snapshot.Tree, "=== ACTIVE DIALOG ===")

		mode := stepGoal.Mode
		if hasDialog && mode == planner.ModeNavigation {
			mode = planner.ModeInteraction
		}

		env := EnvState{
			Task:        task,
			Plan:        plan,
			CurrentStep: currentPlanStep,
			URL:         snapshot.URL,
			DOMTree:     snapshot.Tree,
			History:     mem.HistoryLines(),
		}

		var subAgent SubAgent
		if mode == planner.ModeInteraction {
			subAgent = o.interaction
		} else {
			subAgent = o.navigator
		}

		fmt.Printf("🎯 CURRENT GOAL (%s): %s\n", mode, stepGoal.Goal)
		fmt.Printf("👤 SUB-AGENT: %s\n", subAgent.Name())

		action, thought, stepDone, stepErr := subAgent.Step(ctx, env)
		if stepErr != nil {
			err = fmt.Errorf("%s step error: %w", subAgent.Name(), stepErr)
			return err
		}

		fmt.Printf("\n🤖 THOUGHT: %s\n", thought)
		fmt.Printf("⚡ ACTION: %s [%d] %q (step_done=%v)\n",
			action.Type, action.TargetID, action.Text, stepDone)

		// Loop guard (including pattern detection)
		if blocked, reason := mem.ShouldBlock(snapshot.URL, *action); blocked {
			fmt.Printf("⛔ LOOP GUARD: suppressing action %s on target %d\n",
				action.Type, action.TargetID)
			if reason != "" {
				fmt.Println(reason)
				mem.AddSystemNote(reason)
			}
			mem.MarkLoopTriggered()

			loopBlocksPerStep[currentPlanStep]++

			// Если для текущего шага плана луп-гард сработал несколько раз подряд —
			// считаем, что этот шаг либо уже выполнен, либо дальше автоматом его не продвинуть.
			if loopBlocksPerStep[currentPlanStep] >= 2 {
				fmt.Printf("🔁 LOOP GUARD: too many blocked actions in plan step %d, forcing move to the next plan step.\n",
					plan.Steps[currentPlanStep].Index)
				mem.AddSystemNote(fmt.Sprintf(
					"SYSTEM NOTE: Several actions for plan step %d were blocked as loops. "+
						"Treat this plan step as completed or not actionable and move on.",
					plan.Steps[currentPlanStep].Index,
				))
				currentPlanStep++
				if currentPlanStep >= len(plan.Steps) {
					fmt.Println("✅ All plan steps processed — finishing.")
					return nil
				}
			}

			time.Sleep(1 * time.Second)
			continue
		}

		if action.Type == llm.ActionFinish {
			fmt.Println("✅ Sub-agent requested global finish.")
			return nil
		}

		if execErr := o.executeAction(reader, *action); execErr != nil {
			log.Printf("Action failed: %v", execErr)
		} else {
			// в память попадают только успешно выполненные действия
			mem.Add(step, snapshot.URL, *action)

			// Если LLM явно сказал, что план-шаг выполнен — двигаемся дальше.
			if stepDone {
				currentPlanStep++
			}
		}

		time.Sleep(2 * time.Second)
	}

	err = fmt.Errorf("max steps reached")
	return err
}

func (o *Orchestrator) executeAction(reader *bufio.Reader, action llm.Action) error {
	selector := fmt.Sprintf("[data-ai-id='%d']", action.TargetID)

	if action.Type == llm.ActionClick || action.Type == llm.ActionTypeInput {
		o.highlight(selector)
		time.Sleep(500 * time.Millisecond)
	}

	switch action.Type {
	case llm.ActionClick:
		fmt.Printf("Clicking %s...\n", selector)
		if err := o.browser.Page.Locator(selector).First().ScrollIntoViewIfNeeded(); err != nil {
			return fmt.Errorf("scroll failed: %w", err)
		}
		return o.browser.Page.Click(selector)

	case llm.ActionTypeInput:
		fmt.Printf("Typing '%s' into %s (Submit=%v)...\n", action.Text, selector, action.Submit)
		if err := o.browser.Page.Fill(selector, action.Text); err != nil {
			return err
		}
		if action.Submit {
			fmt.Println("👉 Pressing ENTER...")
			return o.browser.Page.Press(selector, "Enter")
		}
		return nil

	case llm.ActionNavigate:
		targetURL := normalizeURL(o.browser.Page.URL(), action.URL)
		fmt.Printf("Navigating to %s...\n", targetURL)
		_, err := o.browser.Page.Goto(targetURL)
		return err

	case llm.ActionFinish:
		return nil

	default:
		return fmt.Errorf("unknown action type: %s", action.Type)
	}
}

func (o *Orchestrator) highlight(selector string) {
	script := fmt.Sprintf(`
		const el = document.querySelector("%s");
		if (el) {
			el.style.outline = "5px solid red";
			el.style.zIndex = "999999";
			el.scrollIntoView({behavior: "smooth", block: "center", inline: "center"});
		}
	`, selector)
	_, _ = o.browser.Page.Evaluate(script)
}

func normalizeURL(currentURL, target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return currentURL
	}

	u, err := url.Parse(target)
	if err == nil && u.IsAbs() {
		return target
	}

	base, err := url.Parse(currentURL)
	if err != nil {
		return target
	}

	return base.ResolveReference(u).String()
}
