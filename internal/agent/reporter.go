package agent

import (
	"fmt"
	"strings"
	"time"

	"github.com/nbenliogludev/go-browser-ai-agent/internal/llm"
)

type Reporter struct {
	llm   llm.Client
	task  string
	trace []string

	finalAction llm.Action
	finalURL    string
}

func NewReporter(llmClient llm.Client, task string) *Reporter {
	return &Reporter{
		llm:  llmClient,
		task: task,
	}
}

func (r *Reporter) LogDecision(step int, url string, d *llm.DecisionOutput) {
	decor := ""
	if d.Action.IsDestructive {
		decor = " [DESTRUCTIVE]"
	}

	fmt.Println(strings.Repeat("-", 40))
	fmt.Printf("🧠 PHASE:       %s\n", strings.ToUpper(d.CurrentPhase))
	fmt.Printf("👀 OBSERVATION: %s\n", d.Observation)
	fmt.Printf("🤖 THOUGHT:     %s\n", d.Thought)
	fmt.Printf("⚡ ACTION:      %s [%d] %q%s\n",
		d.Action.Type,
		d.Action.TargetID,
		d.Action.Text,
		decor,
	)
	fmt.Println(strings.Repeat("-", 40))

	r.finalAction = d.Action
	r.finalURL = url

	r.trace = append(r.trace, fmt.Sprintf(
		"STEP %d | URL=%s | PHASE=%s | ACTION=%s[%d] %q%s | OBS=%s",
		step,
		url,
		strings.ToUpper(d.CurrentPhase),
		d.Action.Type,
		d.Action.TargetID,
		d.Action.Text,
		decor,
		d.Observation,
	))
}

func (r *Reporter) StepError(err error) {
	fmt.Printf("⚠️ Step error: %v\n", err)
}

func (r *Reporter) Finished(start time.Time, mem *StepMemory) {
	r.printReport(start, "task finished", mem)
}

func (r *Reporter) Interrupted(start time.Time, mem *StepMemory) {
	r.printReport(start, "interrupted by user (Ctrl+C)", mem)
}

func (r *Reporter) MaxStepsReached(start time.Time, mem *StepMemory) {
	r.printReport(start, "max steps reached", mem)
}

func (r *Reporter) printReport(start time.Time, reason string, mem *StepMemory) {
	duration := time.Since(start).Truncate(time.Millisecond)

	fmt.Println("\n===== EXECUTION REPORT =====")
	fmt.Printf("Task: %s\n", r.task)
	fmt.Printf("Duration: %s\n", duration)
	fmt.Printf("Exit reason: %s\n\n", reason)

	fmt.Println("--- RAW STEP TRACE ---")
	for _, line := range r.trace {
		fmt.Println(line)
	}

	fmt.Println("\n--- LLM SUMMARY ---")
	summary, err := r.llm.SummarizeRun(llm.SummaryInput{
		Task:        r.task,
		ExitReason:  humanizeReason(reason),
		FinalURL:    r.finalURL,
		FinalAction: r.finalAction,
		Duration:    duration.String(),
		Steps:       mem.FullHistory(),
	})
	if err != nil {
		fmt.Println("(failed to generate summary)")
	} else {
		fmt.Println(summary)
	}

	fmt.Println("===== END OF REPORT =====")
}
