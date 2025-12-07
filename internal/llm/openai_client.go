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

// visionSystemPrompt обновлен для обработки Cookies и понимания Viewport
const visionSystemPrompt = `
You are a STRICT web-browsing control agent.

You receive:
1) A natural-language USER TASK.
2) The CURRENT URL.
3) A DOM SUMMARY of the VISIBLE VIEWPORT only.
   - Elements outside the viewport are NOT included.
   - Interactive elements are annotated as: [ID] <tag ... label="..." ...>
   - The "ID" inside [] is a numeric "data-ai-id" attribute.
4) A SCREENSHOT of the VISIBLE VIEWPORT (not full page).

Your job:
- Decide EXACTLY ONE next low-level browser action.
- Use BOTH the screenshot and DOM summary.

POPUP & COOKIE HANDLING (CRITICAL):
- Before proceeding with the task, check if there are "Accept Cookies", "Subscribe", "Location", or "Login" overlays blocking the view.
- If you see them (in screenshot or DOM), your HIGHEST PRIORITY is to close them or click "Accept"/"Allow" to clear the screen.
- You cannot click elements underneath a dark backdrop/overlay. Deal with the overlay first.

ALLOWED ACTION TYPES (JSON field "type"):
- "click"  : Click on an interactive element (target_id required).
- "type"   : Type text into an input (target_id and text required).
- "finish" : Stop. The task is done or impossible.

OUTPUT FORMAT (STRICT JSON):
{
  "thought": "short explanation of your reasoning",
  "step_done": true or false,
  "action": {
    "type": "click" | "type" | "finish",
    "target_id": <number>,     
    "text": "string",          
    "submit": true | false     
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

	sb.WriteString("\n\nDOM SUMMARY (Viewport Only):\n")
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
				URL: "data:image/jpeg;base64," + input.ScreenshotBase64,
			},
		})
	}

	req := openai.ChatCompletionRequest{
		Model: "gpt-4o", // Vision capable
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
		// MaxTokens можно ограничить, чтобы ответ не был длинным, но для JSON объекта обычно хватает дефолта
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

	// Защита от пустых значений
	switch out.Action.Type {
	case ActionClick, ActionTypeInput, ActionFinish:
	default:
		out.Action.Type = ActionFinish
	}

	return &out, nil
}
