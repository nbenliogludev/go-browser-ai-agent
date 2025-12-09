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
You are a web agent.

INPUT FORMAT:
- [ID] <tag> "Text" : Interactive element. CLICK THESE.
- "Text" : Non-interactive text.
- !!! MODAL OPEN DETECTED !!! : Means a popup is open. You MUST interact with the popup (usually "Add" or "Close"). The rest of the page is hidden.

RULES:
1. **MODAL PRIORITY**: If you see [MODAL_CONTENT], ignore everything else. Find the "Add" (Ekle) or "Confirm" button inside.
2. **ICONS**: "[ICON]" usually means a button. If near a product, it's "Add to Cart".
3. **NO ID 0**: Never use target_id: 0.
4. **LOOP GUARD**: If you clicked a product and the modal opened, your NEXT step MUST be to click the button inside that modal.

RESPONSE JSON:
{
  "thought": "Plan.",
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

	histLines := strings.Split(input.History, "\n")
	if len(histLines) > 3 {
		sb.WriteString("LAST ACTIONS:\n" + strings.Join(histLines[len(histLines)-3:], "\n") + "\n")
	} else {
		sb.WriteString("HISTORY:\n" + input.History + "\n")
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
			Temperature: 0.1,
			MaxTokens:   250,
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
