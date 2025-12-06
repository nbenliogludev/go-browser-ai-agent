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

func (o *Orchestrator) Run(task string, maxSteps int) error {
	reader := bufio.NewReader(os.Stdin)
	ctx := context.Background()

	// Общая память шагов для обоих под-агентов:
	// история + защита от циклов и паттернов.
	mem := NewStepMemory(10, 3)

	// 0. План
	plan, err := o.planner.BuildPlan(ctx, task)
	if err != nil {
		return fmt.Errorf("build plan failed: %w", err)
	}

	fmt.Println("📋 PLAN:")
	for _, s := range plan.Steps {
		fmt.Printf("  %d. [%s] %s\n", s.Index, s.Mode, s.Goal)
	}

	currentPlanStep := 0

	for step := 1; step <= maxSteps; step++ {
		fmt.Printf("\n--- STEP %d (plan %d/%d) ---\n",
			step, currentPlanStep+1, len(plan.Steps))

		state := playwright.LoadState(browser.LoadStateNetworkidle)
		o.browser.Page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
			State: &state,
		})

		snapshot, err := o.browser.Snapshot()
		if err != nil {
			return fmt.Errorf("snapshot failed: %w", err)
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

		// если есть модалка — форсируем interaction
		hasDialog := strings.Contains(snapshot.Tree, `context="dialog"`)
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

		action, thought, err := subAgent.Step(ctx, env)
		if err != nil {
			return fmt.Errorf("%s step error: %w", subAgent.Name(), err)
		}

		fmt.Printf("\n🤖 THOUGHT: %s\n", thought)
		fmt.Printf("⚡ ACTION: %s [%d] %q\n",
			action.Type, action.TargetID, action.Text)

		// Жёсткая защита от цикла (в том числе по паттернам действий)
		if blocked, reason := mem.ShouldBlock(snapshot.URL, *action); blocked {
			fmt.Printf("⛔ LOOP GUARD: suppressing action %s on target %d\n",
				action.Type, action.TargetID)
			if reason != "" {
				fmt.Println(reason)
				mem.AddSystemNote(reason)
			}
			time.Sleep(1 * time.Second)
			continue
		}

		if action.Type == llm.ActionFinish {
			fmt.Println("✅ Sub-agent requested finish.")
			return nil
		}

		if err := o.executeAction(reader, *action); err != nil {
			log.Printf("Action failed: %v", err)
		} else {
			mem.Add(step, snapshot.URL, *action)

			// Простая эвристика завершения шага
			if mode == planner.ModeNavigation {
				if snapshot.URL != o.browser.Page.URL() {
					currentPlanStep++
				}
			} else {
				currentPlanStep++
			}
		}

		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("max steps reached")
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
