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

// GENERIC PROMPT: Приоритет на атрибуты!
const visionSystemPrompt = `
You are a web-browsing agent.

INPUT:
1. User Task.
2. Current URL.
3. DOM Summary. 
   - Note elements with attributes: 'priority="high"', 'style="filled"', 'pos="sticky"'.
4. Screenshot.

YOUR GOAL:
Select the NEXT BEST ACTION to move towards the goal.

GENERIC STRATEGIES:
1. **Target Priority:** If you see an element with 'priority="high"' (filled color, short text), IT IS LIKELY THE PRIMARY ACTION. Click it!
2. **Modals:** If inside a modal (--- MODAL START ---), ignore header texts/descriptions. Look for the 'priority="high"' button at the bottom.
3. **Search:** Use search inputs for specific items.
4. **Scroll:** If you don't see the target, scroll down.

RESPONSE FORMAT (STRICT JSON):
{
  "thought": "Reasoning...",
  "action": {
    "type": "click" | "type" | "scroll_down" | "finish",
    "target_id": 123,
    "text": "..."
  }
}
`

func (c *OpenAIClient) DecideAction(input DecisionInput) (*DecisionOutput, error) {
	// ... (код функции остается тем же) ...
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
		return nil, fmt.Errorf("JSON error: %w", err)
	}

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
