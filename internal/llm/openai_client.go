package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

type OpenAIClient struct {
	client *openai.Client
}

func NewOpenAIClient() (*OpenAIClient, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY not set")
	}
	return &OpenAIClient{client: openai.NewClient(apiKey)}, nil
}

const visionSystemPrompt = `
You are an autonomous intelligent agent navigating a web browser.

GOAL: Complete the USER TASK efficiently.

INPUT:
1. DOM Tree: Current interactive elements, in lines like:
   [123] [role] "Visible name"
   Only IDs in [...] are valid target_id values.
2. Screenshot: Visual context.
3. HISTORY: Your previous actions and thoughts.

ALLOWED ACTION TYPES (STRICT):
- You may ONLY use the following action.type values:
  - "click"
  - "type"
  - "scroll_down"
  - "finish"
- You MUST NOT invent any other action types such as "search", "hover", "focus", etc.
  If you want to search, you MUST:
    1) use type into a search input or text box (action.type="type"),
    2) optionally scroll_down to reveal more results.

RULES ABOUT TARGET IDS (CRITICAL):
- You may ONLY use target_id values that actually appear in the DOM text as:
    [123] [role] "Name"
- NEVER use target_id: 0.
- If you cannot find a suitable id:
    - Prefer scroll_down to explore the page.
    - Or finish the task if it is clearly completed.
- If HISTORY shows errors like "TargetID X not found" or "invalid TargetID 0",
  you MUST change strategy (choose another id or another action).

DESTRUCTIVE ACTIONS AND is_destructive FLAG:
- The "is_destructive" flag MUST be set to true ONLY for actions that:
  - trigger payments or order submission (e.g. "Pay", "Öde", "Place Order",
    "Siparişi tamamla", "Checkout"),
  - delete or permanently remove data (e.g. "Delete", "Sil", "Remove", "Trash",
    "Delete email", "Remove account"),
  - send irreversible or sensitive content (e.g. final "Send email", "Submit form"
    with important data).
- For simple navigation (open restaurant, open product, scroll, filter, change tabs),
  "is_destructive" MUST be false.
- If you are not sure whether an action is destructive, prefer is_destructive=false.

CORE LOGIC - "STATE MACHINE":
You must act like a State Machine. Before choosing an action, determine your current "Phase".

**PHASE 1: SEARCH & DISCOVERY**
- Goal: Find the specific item/content mentioned in the task.
- Action: Search, Click Categories, Scroll.
- Exit Condition: The desired item or restaurant is VISIBLE in the DOM lines.

**PHASE 2: EXECUTION (INTERACTION)**
- Goal: Interact with the item (Add to cart, Fill form, Click item).
- Action: Click "Add", "Ekle", "Buy", or select options in a modal using click/type.
- Exit Condition: You performed the action and the state of the page changed.

**PHASE 3: VERIFICATION & PROGRESSION (CRITICAL)**
- Trigger: You just performed an EXECUTION action (e.g., clicked "Add").
- Task: LOOK for visual and DOM changes indicating success:
  - Did a counter increase? (0 -> 1)
  - Did a price appear/change? (0.00 -> 150.00)
  - Did a "Added to cart" toast appear?
  - Did the button text change to something like "In Cart"?
- Decision:
  - IF STATE CHANGED: DO NOT REPEAT THE SAME ACTION. Instead, move to the next step
    (go to Cart/Basket, proceed to Checkout, or finish).
  - IF NO CHANGE: The action failed. Try a different button or approach.

NAVIGATION / RELEVANCE HEURISTICS (VERY IMPORTANT):
- When the task mentions a specific ITEM TYPE (for example: "pizza"):
  - STRONGLY PREFER elements whose visible name contains that keyword (case-insensitive),
    e.g. restaurant names or product names containing "Pizza".
  - A restaurant called "Alo Pizza" is much more relevant than "Cajun Corner" or
    a generic burger/chicken place.
- If you open a restaurant page and after several scroll/inspect actions you still do NOT
  see any relevant items (for example no pizzas at all):
  - Treat this as a WRONG CHOICE.
  - Prefer to navigate back (e.g., to the restaurant list) and choose a more relevant
    restaurant (one whose name clearly matches the task, such as containing "Pizza").

GETIRYEMEK-SPECIFIC HINT (EXAMPLE, NOT A HARD RULE):
- If the task is "order a pizza" and you are on GetirYemek:
  - Prefer restaurants whose names contain "Pizza" in the DOM.
  - Avoid staying in restaurants that obviously serve only chicken, burgers, or side dishes
    and have no pizza items in their menu after scrolling and inspecting.

GENERAL RULES:
1. NO LOOPS:
   - If you clicked "Add Pizza" and the DOM shows the cart or price updated,
     DO NOT click it again. Move to checkout or finish.
   - If a sequence of actions clearly repeats without changing the DOM, change strategy.
2. MODALS:
   - If a modal opens, treat it as your immediate environment.
   - Interact inside it until you either confirm/cancel/close it or finish the flow.
3. ID 0:
   - Never use target_id: 0.
   - Always pick a concrete id from the DOM listing.

RESPONSE JSON FORMAT:
{
  "current_phase": "SEARCH" | "EXECUTION" | "VERIFICATION",
  "observation": "Short description of what you see and what changed.",
  "thought": "Your reasoning about the next step.",
  "action": {
    "type": "click" | "type" | "scroll_down" | "finish",
    "target_id": 123,
    "text": "input text if any",
    "submit": true,
    "is_destructive": false
  }
}
`

const safeDOMLimit = 20000

func (c *OpenAIClient) DecideAction(input DecisionInput) (*DecisionOutput, error) {
	ctx := context.Background()

	var sb strings.Builder
	sb.WriteString("TASK: " + input.Task + "\n")
	sb.WriteString("URL: " + input.CurrentURL + "\n")

	if input.History != "" {
		sb.WriteString("HISTORY (Last 5 steps):\n" + input.History + "\n")
	}

	dom := input.DOMTree
	if len(dom) > safeDOMLimit {
		dom = dom[:safeDOMLimit] + "\n...[TRUNCATED]"
	}
	sb.WriteString("\nDOM:\n" + dom)

	parts := []openai.ChatMessagePart{
		{
			Type: openai.ChatMessagePartTypeText,
			Text: sb.String(),
		},
	}

	if input.ScreenshotBase64 != "" {
		parts = append(parts, openai.ChatMessagePart{
			Type: openai.ChatMessagePartTypeImageURL,
			ImageURL: &openai.ChatMessageImageURL{
				URL: "data:image/jpeg;base64," + input.ScreenshotBase64,
			},
		})
	}

	var resp openai.ChatCompletionResponse
	var err error

	for attempt := 0; attempt < 5; attempt++ {
		resp, err = c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model: "gpt-4o",
			Messages: []openai.ChatCompletionMessage{
				{Role: openai.ChatMessageRoleSystem, Content: visionSystemPrompt},
				{Role: openai.ChatMessageRoleUser, MultiContent: parts},
			},
			ResponseFormat: &openai.ChatCompletionResponseFormat{
				Type: openai.ChatCompletionResponseFormatTypeJSONObject,
			},
			Temperature: 0.0,
			MaxTokens:   300,
		})

		if err == nil {
			break
		}

		if strings.Contains(err.Error(), "429") {
			wait := time.Duration(3*(1<<attempt)) * time.Second
			fmt.Printf("⚠️ Rate Limit (TPM). Pausing %v... (%d/5)\n", wait, attempt+1)
			time.Sleep(wait)
			continue
		}
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no choices")
	}

	content := resp.Choices[0].Message.Content

	var out DecisionOutput
	clean := strings.TrimPrefix(content, "```json")
	clean = strings.TrimPrefix(clean, "```")
	clean = strings.TrimSuffix(clean, "```")

	if err := json.Unmarshal([]byte(clean), &out); err != nil {
		return nil, fmt.Errorf("JSON error: %w; raw content: %s", err, content)
	}

	rawType := strings.ToLower(strings.TrimSpace(string(out.Action.Type)))

	switch rawType {
	case "", "null":
		out.Action.Type = ActionScroll
	case "click":
		out.Action.Type = ActionClick
	case "type":
		out.Action.Type = ActionTypeInput
	case "scroll_down":
		out.Action.Type = ActionScroll
	case "finish":
		out.Action.Type = ActionFinish
	case "search", "search_input", "searchbox":
		out.Action.Type = ActionTypeInput
	default:
		out.Action.Type = ActionScroll
	}

	if out.Action.Type == "" {
		out.Action.Type = ActionScroll
	}

	return &out, nil
}
