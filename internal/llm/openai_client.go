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

// GENERIC STATE-AWARE PROMPT
const visionSystemPrompt = `
You are an autonomous intelligent agent navigating a web browser.

GOAL: Complete the USER TASK efficiently.

INPUT:
1. DOM Tree: Current interactive elements.
2. Screenshot: Visual context.
3. HISTORY: Your previous actions and thoughts.

CORE LOGIC - "STATE MACHINE":
You must act like a State Machine. Before choosing an action, determine your current "Phase".

**PHASE 1: SEARCH & DISCOVERY**
- Goal: Find the specific item/content mentioned in the task.
- Action: Search, Click Categories, Scroll.
- Exit Condition: The item is visible on the screen.

**PHASE 2: EXECUTION (INTERACTION)**
- Goal: Interact with the item (Add to cart, Fill form, Click item).
- Action: Click "Add", "Ekle", "Buy", or Select Options in Modal.
- Exit Condition: You performed the action.

**PHASE 3: VERIFICATION & PROGRESSION (CRITICAL)**
- **Trigger:** You just performed an EXECUTION action (e.g., clicked "Add").
- **Task:** LOOK for visual changes indicating success:
  - Did a counter increase? (0 -> 1)
  - Did a price appear/change? (0.00 -> 150.00)
  - Did a "Added to cart" toast appear?
  - Did the button text change to "In Cart"?
- **Decision:**
  - IF STATE CHANGED: **DO NOT REPEAT THE ACTION.** The item is added. Move to the NEXT logical step (Go to Cart, Checkout, Finish).
  - IF NO CHANGE: The action failed. Try a different button or approach.

**RULES:**
1. **NO LOOPS:** If you just clicked "Add Pizza" and the DOM shows the price/cart updated, DO NOT click "Add Pizza" again. Proceed to Checkout.
2. **MODALS:** If a modal opens, your entire universe is that modal. Finish the interaction inside it to close it or proceed.
3. **ID 0:** Never use target_id: 0.

RESPONSE JSON FORMAT:
{
  "current_phase": "SEARCH" | "EXECUTION" | "VERIFICATION",
  "observation": "I clicked 'Add' last step. I see the basket total is now 250 TL. The item is successfully added.",
  "thought": "Since the item is added, I must now locate the Basket/Cart button to proceed to checkout.",
  "action": {
    "type": "click" | "type" | "scroll_down" | "finish",
    "target_id": 123,
    "text": "input",
    "submit": true,
    "is_destructive": false
  }
}
`

const safeDOMLimit = 10000

func (c *OpenAIClient) DecideAction(input DecisionInput) (*DecisionOutput, error) {
	ctx := context.Background()

	var sb strings.Builder
	sb.WriteString("TASK: " + input.Task + "\n")
	sb.WriteString("URL: " + input.CurrentURL + "\n")

	// Передаем больше истории, чтобы модель видела свой прогресс
	// Но только "Thought" и "Action", чтобы не забивать контекст
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
			Temperature: 0.0, // Строгий ноль для детерминизма
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
	clean = strings.TrimSuffix(clean, "```")

	if err := json.Unmarshal([]byte(clean), &out); err != nil {
		return nil, fmt.Errorf("JSON error: %w", err)
	}

	if out.Action.Type == "" {
		out.Action.Type = ActionScroll
	}

	return &out, nil
}
