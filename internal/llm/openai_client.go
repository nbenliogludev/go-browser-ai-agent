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
You are a web-browsing agent.

INPUT:
1. User Task.
2. Current URL.
3. DOM Summary (Viewport). [ID] <tag label="...">
4. Screenshot.

YOUR GOAL:
Select the NEXT BEST ACTION to move towards the goal.

STRATEGIES:
1. **Search:** Use search bars for specific items ("Pizza", "Sushi").
2. **Scroll:** If you don't see the target, scroll down ("scroll_down").
3. **Modals:** If a modal/popup is open, interact with it (close or select options inside).
4. **IDs:** Always use the numeric [ID] from the DOM Summary.

RESPONSE FORMAT (STRICT JSON):
{
  "thought": "Reasoning here...",
  "action": {
    "type": "click" | "type" | "scroll_down" | "finish",
    "target_id": 123,      // Required for click/type (integer)
    "text": "search term"  // Required for type
  }
}
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

	if input.ScreenshotBase64 != "" {
		parts = append(parts, openai.ChatMessagePart{
			Type:     openai.ChatMessagePartTypeImageURL,
			ImageURL: &openai.ChatMessageImageURL{URL: "data:image/jpeg;base64," + input.ScreenshotBase64},
		})
	}

	// RETRY LOOP (3 попытки для 429 ошибки)
	var resp openai.ChatCompletionResponse
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		resp, err = c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model: "gpt-4o",
			Messages: []openai.ChatCompletionMessage{
				{Role: openai.ChatMessageRoleSystem, Content: visionSystemPrompt},
				{Role: openai.ChatMessageRoleUser, MultiContent: parts},
			},
			ResponseFormat: &openai.ChatCompletionResponseFormat{Type: openai.ChatCompletionResponseFormatTypeJSONObject},
			Temperature:    0.1,
		})
		if err == nil {
			break
		}
		if strings.Contains(err.Error(), "429") {
			fmt.Printf("⚠️ Rate Limit. Waiting 10s... (%d/3)\n", attempt+1)
			time.Sleep(10 * time.Second) // Ждем дольше
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
	// fmt.Println("DEBUG RAW:", content) // Раскомментируй, если JSON снова сломается

	var out DecisionOutput
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		return nil, fmt.Errorf("JSON error: %w", err)
	}

	// Fallback для неизвестных действий
	switch out.Action.Type {
	case ActionClick, ActionTypeInput:
		if out.Action.TargetID == 0 {
			out.Action.Type = ActionScroll // Если ID нет, лучше проскроллить
		}
	case ActionScroll, ActionFinish:
	default:
		out.Action.Type = ActionScroll
	}

	return &out, nil
}
