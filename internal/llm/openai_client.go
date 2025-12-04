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
3) Optionally, a piece of text extracted from the page on a previous step.

You MUST respond ONLY with a single JSON object of the following form:

{
  "thought": "short explanation of what you want to do next",
  "action": {
    "type": "click" | "navigate" | "type" | "finish" | "extract_text",
    "target_id": "element id from snapshot or empty if not needed",
    "text": "text to type if type",
    "url": "url to open if navigate"
  }
}

Action types:
- "click": click on an element from the snapshot (use its target_id).
- "navigate": open a new URL in the current tab.
- "type": type text into an input/textarea-like element (textbox/combobox).
- "extract_text": read a larger text from a chosen element to better understand the page.
- "finish": stop when the task is completed or cannot be continued.

Important interaction rules:
- You usually need to TYPE into a given textbox/combobox only once per task to set a query or value.
- After a textbox already contains a useful query or phrase, prefer other actions
  (click buttons, links, submit forms, navigate, or extract_text) instead of typing the same text again.
- When you see a textbox/combobox and also a BUTTON element (tag=button or input with role="button" or type="submit")
  that looks like it submits a search or form (for example "Search", "Ara", "Поиск", "Go", etc.),
  the common next step is:
    1) type the query once into the textbox,
    2) then CLICK that button to execute the search.
- Avoid repeatedly typing nearly identical queries into the same textbox if it does not move the task forward.

General rules:
- Output strictly valid JSON, no comments, no extra text, no markdown.
- If the page is not suitable for the task or you need human input, return:
  { "thought": "why you cannot continue", "action": { "type": "finish" } }
`

func (c *OpenAIClient) DecideAction(input DecisionInput) (*DecisionOutput, error) {
	if input.Snapshot == nil {
		return nil, fmt.Errorf("snapshot is nil")
	}

	snapshotJSON, err := json.Marshal(input.Snapshot)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	var userPrompt string
	if input.LastExtractedText != "" {
		userPrompt = fmt.Sprintf(
			"USER TASK:\n%s\n\nLAST EXTRACTED TEXT (from previous step):\n%s\n\nPAGE SNAPSHOT (JSON):\n%s\n\nRemember: respond ONLY with a single JSON object.",
			input.Task,
			input.LastExtractedText,
			string(snapshotJSON),
		)
	} else {
		userPrompt = fmt.Sprintf(
			"USER TASK:\n%s\n\nPAGE SNAPSHOT (JSON):\n%s\n\nRemember: respond ONLY with a single JSON object.",
			input.Task,
			string(snapshotJSON),
		)
	}

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
