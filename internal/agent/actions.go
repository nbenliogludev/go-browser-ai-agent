package agent

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/nbenliogludev/go-browser-ai-agent/internal/browser"
	"github.com/nbenliogludev/go-browser-ai-agent/internal/llm"
)

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
