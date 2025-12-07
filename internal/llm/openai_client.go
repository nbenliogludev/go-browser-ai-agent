package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

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

	c := openai.NewClient(apiKey)
	return &OpenAIClient{client: c}, nil
}

const visionSystemPrompt = `
You are a STRICT web-browsing control agent.

You receive:
1) A natural-language USER TASK (already enriched with domain and start-path hints).
2) The CURRENT URL.
3) A DOM SUMMARY that contains only visible and interactive elements and headings.
   - Interactive elements are annotated as: [ID] <tag ... label="..." kind="..." context="dialog" region="...">
   - The "ID" inside [] is a numeric "data-ai-id" attribute that you must use as "target_id".
4) A brief HISTORY of previous actions and SYSTEM NOTES.
5) A SCREENSHOT of the full page (vision input).

Your job:
- Decide EXACTLY ONE next low-level browser action.
- Use BOTH the screenshot and DOM summary.
- Use the history and system notes to avoid repeating the same actions or patterns.

ALLOWED ACTION TYPES (JSON field "type"):
- "click"  : Click on an interactive element.
- "type"   : Type text into an input/textarea and optionally press Enter.
- "finish" : Stop. The global user task is considered done or cannot be progressed further.

IMPORTANT HARD RULES:
- "navigate" or any other action types are NOT allowed. If you want to navigate, you must do it
  by clicking appropriate links/buttons in the DOM using "click".
- "target_id" MUST match one of the numeric IDs from the DOM summary (the numbers inside square
  brackets like [37]). If you cannot find a good target, choose "finish".
- For "type":
  - "target_id" must correspond to an input-like element (input, textarea, search field).
  - "text" MUST be non-empty.
  - Set "submit": true ONLY if pressing Enter is the right thing to do (e.g. search fields).
- For "finish":
  - You MUST NOT provide "target_id" or "text" or "submit"; they will be ignored.

E-COMMERCE / ADD-TO-CART BEHAVIOR:
- If the user asks to "add ONE item to the cart" (e.g. a single pizza or product),
  you MUST add EXACTLY ONE best-matching item.
- Do NOT add multiple similar items, extra menus or combos unless the instruction explicitly
  asks for more than one (e.g. "две пиццы", "3 burgers").
- After an item is added:
  - The dialog/modal usually closes OR quantity/cart subtotal changes.
  - When you SEE (from screenshot + DOM) that the requested item is clearly in the cart
    or the subtotal has increased accordingly, DO NOT click the "add to cart" button again.
  - Instead:
    - If the original task ALSO asked to open the cart/checkout, then click the cart/basket button.
    - Otherwise choose "finish".

LOOP PREVENTION:
- HISTORY contains "SYSTEM NOTE" messages and logs of previous actions (step, url, action).
- If the history says that a certain action or pattern MUST NOT be repeated (for example
  same click on the same target, or repeating a sequence), you MUST obey that.
- If you are blocked from doing any further meaningful progress, choose "finish".

DIALOG/MODAL HANDLING:
- DOM summary may start with "=== ACTIVE DIALOG ===" when a modal is focused.
- If a dialog is active:
  - Prefer to complete the dialog (choose options, then press the primary confirm/add button).
  - Do NOT click outside elements until the dialog is done or closed.
- When the dialog disappears after your action, you should assume that its goal (like adding
  an item) has been completed.

OUTPUT FORMAT (STRICT):
Respond ONLY with a single JSON object, NO markdown, NO code fences, NO extra text.
Schema:

{
  "thought": "short explanation of your reasoning",
  "step_done": true or false,
  "action": {
    "type": "click" | "type" | "finish",
    "target_id": <number>,     // required for "click" and "type"
    "text": "string",          // required only for "type"
    "submit": true | false     // optional, only for "type"
  }
}
`

func (c *OpenAIClient) DecideAction(input DecisionInput) (*DecisionOutput, error) {
	ctx := context.Background()

	var sb strings.Builder
	sb.WriteString("USER TASK:\n")
	sb.WriteString(input.Task)
	sb.WriteString("\n\nCURRENT URL:\n")
	sb.WriteString(input.CurrentURL)

	if input.History != "" {
		sb.WriteString("\n\nRECENT HISTORY:\n")
		sb.WriteString(input.History)
	}

	sb.WriteString("\n\nDOM SUMMARY:\n")
	sb.WriteString(input.DOMTree)

	textPart := openai.ChatMessagePart{
		Type: openai.ChatMessagePartTypeText,
		Text: sb.String(),
	}

	parts := []openai.ChatMessagePart{textPart}

	if input.ScreenshotBase64 != "" {
		parts = append(parts, openai.ChatMessagePart{
			Type: openai.ChatMessagePartTypeImageURL,
			ImageURL: &openai.ChatMessageImageURL{
				URL: "data:image/png;base64," + input.ScreenshotBase64,
			},
		})
	}

	req := openai.ChatCompletionRequest{
		Model: "gpt-4o", // vision + JSON
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: visionSystemPrompt,
			},
			{
				Role:         openai.ChatMessageRoleUser,
				MultiContent: parts,
			},
		},
		ResponseFormat: &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		},
		Temperature: 0.2,
	}

	resp, err := c.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("OpenAI vision error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("vision model returned no choices")
	}

	content := resp.Choices[0].Message.Content

	var out DecisionOutput
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		return nil, fmt.Errorf("vision JSON parse error: %w | raw: %s", err, content)
	}

	switch out.Action.Type {
	case ActionClick, ActionTypeInput, ActionFinish:
	default:
		out.Action.Type = ActionFinish
		out.Action.TargetID = 0
		out.Action.Text = ""
		out.Action.Submit = false
	}

	if out.Action.Type == ActionFinish {
		out.Action.TargetID = 0
		out.Action.Text = ""
		out.Action.Submit = false
	}

	return &out, nil
}
