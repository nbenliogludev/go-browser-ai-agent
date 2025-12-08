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

// GENERIC PROMPT: Обновлен для работы с модалками и визуальными стилями
const visionSystemPrompt = `
You are a web-browsing agent.

INPUT:
1. User Task.
2. Current URL.
3. DOM Summary. 
   - Note elements with attributes: 'in_modal="true"', 'style="filled"', 'pos="sticky"'.
   - Modal sections are wrapped as:
       --- MODAL START ---
       ...
       --- MODAL END ---
4. Screenshot.

YOUR GOAL:
Select the SINGLE NEXT BEST ACTION to move towards the goal.

GENERIC STRATEGIES:

1. MODAL PRIORITY (CRITICAL)
   - If you see a section marked with '--- MODAL START ---' or elements with 'in_modal="true"', you MUST FOCUS ONLY ON THE MODAL CONTENT.
   - Ignore background elements while the modal is open.
   - The primary action (e.g., "Add", "Confirm", "Sepete Ekle") is usually at the bottom of the modal and often has 'style="filled"' and/or 'pos="sticky"'.

2. VISUAL CLUES
   - 'style="filled"'  → primary, colored action button (buy, add, checkout).
   - 'pos="sticky"'   → fixed header/footer, often contains important buttons like add-to-cart or checkout.

3. CONTEXT
   - Use labels and class hints like 'basket', 'footer', 'modal', etc. to understand purpose.
   - When ordering or adding something to the cart, choose buttons whose visible text matches the goal (e.g., "Sepete Ekle", "Sepete git", "Satın al").

4. SCROLL
   - If you cannot see the required button (e.g., it is below the visible area), output type "scroll_down".

RESPONSE FORMAT (STRICT JSON):
{
  "thought": "Short reasoning about what to do next",
  "action": {
    "type": "click" | "type" | "scroll_down" | "finish",
    "target_id": 123,
    "text": "..." 
  }
}

RULES FOR ACTION FIELDS:

- For "click":
  - "target_id" MUST come from the DOM summary like [320].
  - "text" MUST be the main visible label/innerText of the element (e.g. "Sepete Ekle").
- For "type":
  - "target_id" MUST be the input element ID from the DOM summary.
  - "text" MUST be the exact string to type.
- For "scroll_down":
  - "target_id" MUST be 0 or omitted, and "text" must be empty.
- For "finish":
  - Use only when the user task is clearly completed (e.g. pizza is already in the cart and the cart page is open).
`

func (c *OpenAIClient) DecideAction(input DecisionInput) (*DecisionOutput, error) {
	ctx := context.Background()

	var sb strings.Builder
	sb.WriteString("TASK:\n" + input.Task + "\n\n")
	sb.WriteString("URL: " + input.CurrentURL + "\n\n")
	if input.History != "" {
		sb.WriteString("HISTORY:\n" + input.History + "\n\n")
	}
	sb.WriteString("DOM:\n" + input.DOMTree)

	parts := []openai.ChatMessagePart{{Type: openai.ChatMessagePartTypeText, Text: sb.String()}}

	// Добавляем скриншот, если он есть
	if input.ScreenshotBase64 != "" {
		parts = append(parts, openai.ChatMessagePart{
			Type: openai.ChatMessagePartTypeImageURL,
			ImageURL: &openai.ChatMessageImageURL{
				URL: "data:image/jpeg;base64," + input.ScreenshotBase64,
			},
		})
	}

	// RETRY LOOP (3 попытки для обработки Rate Limits 429)
	var resp openai.ChatCompletionResponse
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		resp, err = c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model: "gpt-4o",
			Messages: []openai.ChatCompletionMessage{
				{Role: openai.ChatMessageRoleSystem, Content: visionSystemPrompt},
				{Role: openai.ChatMessageRoleUser, MultiContent: parts},
			},
			ResponseFormat: &openai.ChatCompletionResponseFormat{
				Type: openai.ChatCompletionResponseFormatTypeJSONObject,
			},
			Temperature: 0.1, // Низкая температура для предсказуемости JSON
		})

		if err == nil {
			break
		}

		// Если ошибка лимитов, ждем и пробуем снова
		if strings.Contains(err.Error(), "429") {
			fmt.Printf("⚠️ Rate Limit. Waiting 5s... (%d/3)\n", attempt+1)
			time.Sleep(5 * time.Second)
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
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		return nil, fmt.Errorf("JSON error: %w (content=%s)", err, content)
	}

	// Fallback: Если модель забыла указать действие, скроллим
	switch out.Action.Type {
	case ActionClick, ActionTypeInput:
		if out.Action.TargetID == 0 {
			out.Action.Type = ActionScroll
		}
	case ActionScroll, ActionFinish:
	default:
		out.Action.Type = ActionScroll
	}

	return &out, nil
}
