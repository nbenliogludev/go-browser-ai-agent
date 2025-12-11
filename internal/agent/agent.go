package agent

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"regexp"
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
	var report []string
	start := time.Now()

	var finalAction llm.Action
	var finalURL string

	cartItems := make(map[string]int)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)

	printReport := func(reason string) {
		if len(report) == 0 {
			return
		}
		fmt.Println("\n===== EXECUTION REPORT =====")
		fmt.Printf("Task: %s\n", task)
		fmt.Printf("Duration: %s\n", time.Since(start).Truncate(time.Millisecond))
		if reason != "" {
			fmt.Printf("Exit reason: %s\n", reason)
		}

		for _, line := range report {
			fmt.Println(line)
		}

		fmt.Println("\n----- SUMMARY -----")
		fmt.Printf("Task: %s\n", task)
		fmt.Printf("Total steps: %d\n", len(report))

		if finalURL != "" {
			fmt.Printf("Final URL: %s\n", finalURL)
		}

		if hr := humanizeReason(reason); hr != "" {
			fmt.Printf("Human-readable reason: %s\n", hr)
		}

		if len(cartItems) > 0 {
			fmt.Println("Cart actions:")
			for name, count := range cartItems {
				fmt.Printf("- Added to cart: %d × %s\n", count, name)
			}
		}

		lowerURL := strings.ToLower(finalURL)
		if strings.Contains(lowerURL, "/odeme") ||
			strings.Contains(lowerURL, "payment") ||
			strings.Contains(lowerURL, "checkout") {
			fmt.Println("Checkout status: payment page reached, order was not submitted automatically.")
		}

		if finalAction.Type != "" {
			fmt.Printf("Last action: %s [%d] %q\n",
				finalAction.Type,
				finalAction.TargetID,
				finalAction.Text,
			)
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
			printReport("snapshot error")
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
			printReport("llm error")
			return fmt.Errorf("llm error: %w", err)
		}

		finalAction = decision.Action
		finalURL = snapshot.URL

		extractCartItemsFromObservation(decision.Observation, snapshot.URL, cartItems)

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
			printReport("task finished")
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
		printReport("interrupted by user (Ctrl+C)")
		return fmt.Errorf("interrupted by user (Ctrl+C)")
	}

	printReport("max steps reached")
	return fmt.Errorf("max steps reached")
}

func (a *Agent) executeAction(action llm.Action, snap *browser.PageSnapshot) error {
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

	if action.IsDestructive {
		if !confirmDestructiveAction(action) {
			return nil
		}
	}

	backendNodeID, found := snap.Elements[action.TargetID]
	if !found {
		return fmt.Errorf("TargetID %d not found in elements map", action.TargetID)
	}

	fmt.Printf("🎯 Targeting BackendNodeID: %d\n", backendNodeID)

	return chromedp.Run(a.browser.Ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		switch action.Type {
		case llm.ActionClick:
			obj, err := dom.ResolveNode().
				WithBackendNodeID(backendNodeID).
				Do(ctx)
			if err != nil {
				return fmt.Errorf("resolve node failed: %w", err)
			}
			if obj == nil || obj.ObjectID == "" {
				return fmt.Errorf("object id is empty (node might be detached)")
			}

			script := `function() {
				try {
					if (this.scrollIntoViewIfNeeded) {
						this.scrollIntoViewIfNeeded();
					} else if (this.scrollIntoView) {
						this.scrollIntoView({ block: "center", inline: "center" });
					}

					const isClickable = (el) => {
						if (!el) return false;
						const tag = (el.tagName || "").toLowerCase();
						const role = (el.getAttribute && (el.getAttribute("role") || "").toLowerCase()) || "";

						if (tag === "button" || tag === "a") return true;
						if (tag === "input") {
							const type = (el.type || "").toLowerCase();
							if (type === "button" || type === "submit" || type === "radio" || type === "checkbox") return true;
						}
						if (tag === "label") return true;
						if (role === "button" || role === "link" || role === "radio" || role === "checkbox") return true;
						return false;
					};

					const clickRadioFromLabel = (label) => {
						if (!label) return false;
						const input = label.querySelector("input[type='radio'],input[type='checkbox']");
						if (input) {
							input.click();
							return true;
						}
						return false;
					};

					let el = this;

					if (el.closest) {
						const directLabel = el.closest("label");
						if (clickRadioFromLabel(directLabel)) {
							return;
						}
					}

					for (let i = 0; i < 5 && el; i++) {
						if (isClickable(el)) {
							if (el.tagName && el.tagName.toLowerCase() === "label") {
								if (clickRadioFromLabel(el)) return;
							}
							el.click();
							return;
						}
						if (el.closest) {
							const parentLabel = el.closest("label");
							if (clickRadioFromLabel(parentLabel)) {
								return;
							}
						}
						el = el.parentElement;
					}

					this.click();
				} catch (e) {
					console.log("click helper error", e);
				}
			}`

			_, _, err = runtime.CallFunctionOn(script).
				WithObjectID(obj.ObjectID).
				Do(ctx)
			return err

		case llm.ActionTypeInput:
			obj, err := dom.ResolveNode().
				WithBackendNodeID(backendNodeID).
				Do(ctx)
			if err != nil {
				return fmt.Errorf("resolve node failed: %w", err)
			}
			if obj == nil || obj.ObjectID == "" {
				return fmt.Errorf("object id is empty (node might be detached)")
			}

			script := fmt.Sprintf(`function() { 
				if (this.scrollIntoViewIfNeeded) {
					this.scrollIntoViewIfNeeded();
				} else if (this.scrollIntoView) {
					this.scrollIntoView({ block: "center", inline: "center" });
				}
				this.value = "";
				this.value = "%s";
				this.dispatchEvent(new Event('input', { bubbles: true }));
				this.dispatchEvent(new Event('change', { bubbles: true }));
			}`, action.Text)

			_, _, err = runtime.CallFunctionOn(script).
				WithObjectID(obj.ObjectID).
				Do(ctx)
			if err != nil {
				return err
			}

			if action.Submit {
				_ = dom.Focus().WithBackendNodeID(backendNodeID).Do(ctx)
				_ = chromedp.SendKeys("", "\r").Do(ctx)
			}
			return nil

		default:
			return nil
		}
	}))
}

func confirmDestructiveAction(action llm.Action) bool {
	fmt.Printf("⚠️ SECURITY LAYER: model suggests a DESTRUCTIVE action (payment, deletion, etc.).\n")
	fmt.Printf("   Planned action: %s [%d] %q\n", action.Type, action.TargetID, action.Text)
	fmt.Print("   Allow this action? (y/n): ")

	tty, err := os.Open("/dev/tty")
	if err != nil {
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

func humanizeReason(reason string) string {
	switch reason {
	case "task finished":
		return "model explicitly finished the task"
	case "max steps reached":
		return "step limit reached"
	case "interrupted by user (Ctrl+C)":
		return "execution was interrupted by user (Ctrl+C)"
	case "llm error":
		return "LLM client error"
	case "snapshot error":
		return "page snapshot error"
	default:
		return reason
	}
}

var cartItemRegexp = regexp.MustCompile(`(\d+)\s*[×x]\s*([^'"]+)`)

func extractCartItemsFromObservation(obs, url string, acc map[string]int) {
	if acc == nil {
		return
	}
	lowObs := strings.ToLower(obs)
	lowURL := strings.ToLower(url)

	if !(strings.Contains(lowObs, "cart") ||
		strings.Contains(lowObs, "sepet") ||
		strings.Contains(lowURL, "/sepet")) {
		return
	}

	matches := cartItemRegexp.FindAllStringSubmatch(obs, -1)
	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		countStr := strings.TrimSpace(m[1])
		name := strings.TrimSpace(m[2])

		var count int
		_, err := fmt.Sscanf(countStr, "%d", &count)
		if err != nil || count <= 0 {
			continue
		}

		if name == "" {
			continue
		}

		acc[name] += count
	}
}
