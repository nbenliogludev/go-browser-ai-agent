package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	openai "github.com/sashabaranov/go-openai"
)

type OpenAIClient struct {
	client *openai.Client
}

func NewOpenAIClient() (*OpenAIClient, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is not set")
	}
	client := openai.NewClient(apiKey)
	return &OpenAIClient{client: client}, nil
}

// ОБНОВЛЕННЫЙ ПРОМПТ
const systemPrompt = `
You are an autonomous browser agent navigating the web.
You will see a textual representation of the current webpage (DOM Tree).
Interactive elements are marked with numeric IDs in brackets, e.g., [12] <button>.

Your goal is to complete the user's task.

RESPONSE FORMAT:
You must strictly respond with a SINGLE JSON object:
{
  "thought": "Brief reasoning about the state and what to do next",
  "action": {
    "type": "click" | "type" | "navigate" | "finish",
    "target_id": 12,        // INTEGER ID from the tree
    "text": "some text",    // ONLY for "type" action
    "submit": true,         // OPTIONAL: set to true to press ENTER after typing (CRITICAL for search inputs!)
    "url": "https://..."    // ONLY for "navigate" action
  }
}

GUIDELINES:
1. SEARCHING: Always set "submit": true when typing into search bars. This is much more reliable than clicking a button.
2. POPUPS: If a cookie banner blocks the view, try to click "Accept" or "Close" first.
3. ERRORS: If you repeat the same action twice, stop and try a different approach.
`

func (c *OpenAIClient) DecideAction(input DecisionInput) (*DecisionOutput, error) {
	userMessage := fmt.Sprintf(`
USER TASK: %s

CURRENT URL: %s

PAGE ACCESSIBILITY TREE:
%s
`, input.Task, input.CurrentURL, input.DOMTree)

	if len(userMessage) > 60000 {
		userMessage = userMessage[:60000] + "\n... (truncated)"
	}

	ctx := context.Background()

	req := openai.ChatCompletionRequest{
		Model: openai.GPT4oMini,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: systemPrompt,
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: userMessage,
			},
		},
		ResponseFormat: &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		},
	}

	resp, err := c.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("OpenAI error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no response choices")
	}

	content := resp.Choices[0].Message.Content

	var output DecisionOutput
	if err := json.Unmarshal([]byte(content), &output); err != nil {
		return nil, fmt.Errorf("json parse error: %w | content: %s", err, content)
	}

	return &output, nil
}
