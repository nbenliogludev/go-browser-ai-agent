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

Environment:
- You control a real browser page.
- On each step you receive:
  1) A high-level user task.
  2) A JSON "snapshot" of the current page with a limited list of interactive elements:
     { "url", "title", "elements": [{ "id", "tag", "role", "text", "selector" }] }

You MUST respond ONLY with a single JSON object of the form:

{
  "thought": "short explanation of what you want to do next (1–2 sentences)",
  "action": {
    "type": "click" | "navigate" | "type" | "read_content" | "finish",
    "target_id": "element id from snapshot or empty if not needed",
    "text": "text to type when type",
    "url": "url to open when navigate",
    "max_chars": 0
  }
}

Action semantics:
- "click": click on the element with the given target_id.
- "type": type the provided "text" into the element with the given target_id.
  Use this ONLY for real text inputs (search boxes, form fields, input/textarea).
- "navigate": open the given absolute URL in the current tab.
- "read_content": read human-visible text around the element with the given target_id.
  The environment will return this text and include it in the next steps of the task
  under a "PREVIOUS OBSERVATIONS" section. Use this when you need to understand
  the content of:
    - user profiles / resumes,
    - job descriptions,
    - emails,
    - product pages,
    - order checkouts, etc.
  Set "max_chars" to a reasonable limit (e.g. 1000–2000) to keep context small.
- "finish": use this when the user's task is fully solved or cannot be reasonably continued.

General rules:
- Output strictly valid JSON. No comments, no markdown, no extra keys.
- Always choose a target_id that exists in the current snapshot for "click", "type" and "read_content".
- Prefer elements whose "text" best matches the user's goal
  (for example: "Войти", "Откликнуться", "Удалить", "Корзина", "В корзину", "Оформить заказ").
- Avoid getting stuck: after typing into a search box you usually need to submit the form
  (e.g. clicking a nearby search button) instead of typing the same query again.
- For tasks that require understanding page content (reading emails, resumes, job descriptions,
  product details, delivery addresses, etc.), first navigate and click to the right page,
  then use "read_content" on the relevant elements before acting.
- If the page is not suitable for the task or you need human input, return:
  { "thought": "why you cannot continue", "action": { "type": "finish" } }.
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
