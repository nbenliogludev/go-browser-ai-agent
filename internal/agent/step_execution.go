package agent

import (
	"fmt"
	"strings"

	"github.com/chromedp/chromedp"
	"github.com/nbenliogludev/go-browser-ai-agent/internal/llm"
)

type PageSnapshotWrapper struct {
	Tree string
	URL  string
}

func (r *Runner) executeStep(step int) (bool, error) {
	fmt.Printf("\n--- STEP %d ---\n", step)

	snap, err := r.agent.browser.Snapshot(step)
	if err != nil {
		return false, ErrSnapshotFail
	}

	if r.prevSnap != nil && snap.Tree == r.prevSnap.Tree {
		r.mem.AddSystemNote("SYSTEM ALERT: Last action had NO VISIBLE EFFECT.")
	}

	fmt.Printf("URL: %s\nTitle: %s\n", snap.URL, snap.Title)

	decision, err := r.agent.llm.DecideAction(llm.DecisionInput{
		Task:             r.task,
		DOMTree:          snap.Tree,
		CurrentURL:       snap.URL,
		History:          r.mem.HistoryString(),
		ScreenshotBase64: snap.ScreenshotBase64,
	})
	if err != nil {
		return false, ErrLLMFail
	}

	r.reporter.LogDecision(step, snap.URL, decision)

	if blocked, reason := r.mem.ShouldBlock(snap.URL, decision.Action); blocked {
		fmt.Printf("⛔ LOOP GUARD: %s\n", reason)
		_ = chromedp.Run(
			r.agent.browser.Ctx,
			chromedp.Evaluate(`window.scrollBy({top: 300, behavior: 'smooth'});`, nil),
		)
		r.mem.MarkLoopTriggered()
		return false, nil
	}

	if decision.Action.Type == llm.ActionFinish {
		return true, nil
	}

	if err := r.agent.executeAction(decision.Action, snap); err != nil {
		r.mem.AddSystemNote(fmt.Sprintf("SYSTEM ERROR: %v", err))
	} else {
		r.mem.Add(step, snap.URL, decision.Action)
		r.mem.AddSystemNote(fmt.Sprintf(
			"STATE UPDATE: %s | %s",
			strings.ToUpper(decision.CurrentPhase),
			decision.Observation,
		))
	}

	r.prevSnap = &PageSnapshotWrapper{
		Tree: snap.Tree,
		URL:  snap.URL,
	}

	return false, nil
}
