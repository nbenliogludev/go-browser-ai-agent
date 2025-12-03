package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

type OpenAIClient struct {
	client openai.Client
}

func NewOpenAIClient() (*OpenAIClient, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is not set")
	}

	client := openai.NewClient(
		option.WithAPIKey(apiKey),
	)

	return &OpenAIClient{client: client}, nil
}

const systemPrompt = `
You are an autonomous web browsing assistant.
You receive:
1) A user task (what the user wants to achieve).
2) A JSON snapshot of the current web page with limited elements.

You MUST respond ONLY with a single JSON object of the following form:

{
  "thought": "short explanation of what you want to do next",
  "action": {
    "type": "click" | "navigate" | "type" | "finish",
    "target_id": "element id from snapshot or empty if not needed",
    "text": "text to type if type",
    "url": "url to open if navigate"
  }
}

Rules:
- Output strictly valid JSON, no comments, no extra text, no markdown.
- If the page is not suitable for the task or you need human input, return:
  { "thought": "why you cannot continue", "action": { "type": "finish" } }
- Prefer using elements that have meaningful text matching the task.
`

func (c *OpenAIClient) DecideAction(input DecisionInput) (*DecisionOutput, error) {
	if input.Snapshot == nil {
		return nil, fmt.Errorf("snapshot is nil")
	}

	snapshotJSON, err := json.Marshal(input.Snapshot)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	userPrompt := fmt.Sprintf(
		"USER TASK:\n%s\n\nPAGE SNAPSHOT (JSON):\n%s\n\nRemember: respond ONLY with a single JSON object.",
		input.Task,
		string(snapshotJSON),
	)

	ctx := context.Background()

	resp, err := c.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: openai.ChatModelGPT4oMini,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(userPrompt),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("OpenAI ChatCompletion error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no choices returned from OpenAI")
	}

	content := resp.Choices[0].Message.Content
	if content == "" {
		return nil, fmt.Errorf("empty content from OpenAI")
	}

	var decision DecisionOutput
	if err := json.Unmarshal([]byte(content), &decision); err != nil {
		return nil, fmt.Errorf("failed to unmarshal model JSON: %w\nraw: %s", err, content)
	}

	return &decision, nil
}
