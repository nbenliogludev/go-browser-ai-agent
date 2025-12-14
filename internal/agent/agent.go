package agent

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"time"

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
	var report []string
	start := time.Now()

	var finalAction llm.Action
	var finalURL string

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)

	printFinalReport := func(exitReason string) {
		if len(report) == 0 {
			return
		}

		duration := time.Since(start).Truncate(time.Millisecond).String()

		fmt.Println("\n===== EXECUTION REPORT =====")
		fmt.Printf("Task: %s\n", task)
		fmt.Printf("Duration: %s\n", duration)
		if exitReason != "" {
			fmt.Printf("Exit reason: %s\n", exitReason)
		}

		fmt.Println("\n--- RAW STEP TRACE ---")
		for _, line := range report {
			fmt.Println(line)
		}

		fmt.Println("\n--- LLM SUMMARY ---")
		summary, err := a.llm.SummarizeRun(llm.SummaryInput{
			Task:        task,
			ExitReason:  humanizeReason(exitReason),
			FinalURL:    finalURL,
			FinalAction: finalAction,
			Duration:    duration,
			Steps:       mem.FullHistory(),
		})
		if err != nil {
			fmt.Printf("(failed to generate LLM summary: %v)\n", err)
		} else {
			fmt.Println(summary)
		}

		fmt.Println("===== END OF REPORT =====")
	}

	interrupted := false

loop:
	for step := 1; step <= maxSteps; step++ {
		select {
		case <-sigCh:
			fmt.Println("\n⏹ Received Ctrl+C (SIGINT). Stopping agent loop gracefully...")
			interrupted = true
			break loop
		default:
		}

		fmt.Printf("\n--- STEP %d ---\n", step)

		snapshot, err := a.browser.Snapshot(step)
		if err != nil {
			printFinalReport("snapshot error")
			return fmt.Errorf("snapshot failed: %w", err)
		}

		if prevSnapshot != nil && snapshot.Tree == prevSnapshot.Tree {
			mem.AddSystemNote("SYSTEM ALERT: Last action had NO VISIBLE EFFECT.")
		}

		fmt.Printf("URL: %s\nTitle: %s\n", snapshot.URL, snapshot.Title)

		decision, err := a.llm.DecideAction(llm.DecisionInput{
			Task:             task,
			DOMTree:          snapshot.Tree,
			CurrentURL:       snapshot.URL,
			History:          mem.HistoryString(),
			ScreenshotBase64: snapshot.ScreenshotBase64,
		})
		if err != nil {
			printFinalReport("llm error")
			return fmt.Errorf("llm error: %w", err)
		}

		finalAction = decision.Action
		finalURL = snapshot.URL

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

		reportLine := fmt.Sprintf(
			"STEP %d | URL=%s | PHASE=%s | ACTION=%s[%d] %q%s | OBS=%s",
			step,
			snapshot.URL,
			strings.ToUpper(decision.CurrentPhase),
			decision.Action.Type,
			decision.Action.TargetID,
			decision.Action.Text,
			decor,
			decision.Observation,
		)
		report = append(report, reportLine)

		if blocked, reason := mem.ShouldBlock(snapshot.URL, decision.Action); blocked {
			fmt.Printf("⛔ LOOP GUARD: %s\n", reason)
			_ = chromedp.Run(a.browser.Ctx,
				chromedp.Evaluate(`window.scrollBy({top: 300, behavior: 'smooth'});`, nil),
			)
			mem.MarkLoopTriggered()
			time.Sleep(2 * time.Second)
			continue
		}

		if decision.Action.Type == llm.ActionFinish {
			fmt.Println("✅ Task completed!")
			printFinalReport("task finished")
			return nil
		}

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

	if interrupted {
		mem.AddSystemNote("SYSTEM: execution interrupted by user (Ctrl+C).")
		report = append(report, "INTERRUPTED BY USER (Ctrl+C)")
		printFinalReport("interrupted by user (Ctrl+C)")
		return fmt.Errorf("interrupted by user (Ctrl+C)")
	}

	printFinalReport("max steps reached")
	return fmt.Errorf("max steps reached")
}
