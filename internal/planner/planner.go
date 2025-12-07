package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	openai "github.com/sashabaranov/go-openai"
)

const (
	ModeNavigation  = "navigation"
	ModeInteraction = "interaction"
)

type PlanStep struct {
	Index int    `json:"index"`
	Goal  string `json:"goal"`
	Mode  string `json:"mode"` // "navigation" или "interaction"
}

type Plan struct {
	Steps []PlanStep `json:"steps"`
}

type Client interface {
	BuildPlan(ctx context.Context, task string) (*Plan, error)
}

type OpenAIPlanner struct {
	client *openai.Client
}

func NewOpenAIPlanner() (*OpenAIPlanner, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is not set")
	}
	client := openai.NewClient(apiKey)
	return &OpenAIPlanner{client: client}, nil
}

const plannerSystemPrompt = `
You are a high-level task planner for a web-browsing agent.

Your job is to decompose a single natural-language user request into
a small sequence of high-level steps.

Each step must have:
- "index": integer starting from 1
- "goal": what should be achieved in this step
- "mode": either "navigation" or "interaction"

"navigation":
  - moving between pages or sections
  - opening a list/search page where the relevant items can be found

"interaction":
  - working inside a specific page or modal
  - filling forms, selecting options, pressing confirm / add-to-cart / apply buttons

IMPORTANT:
- Steps describe WHAT should be achieved, not the exact UI labels or paths.
- You do NOT know the real site structure. Avoid over-specific phrases like
  "click the 'Pizza' category in the global menu".
- Prefer neutral goals that can be satisfied in multiple ways, e.g.:
    * "open a page with restaurant list that contains pizzas"
    * "select a medium-sized Margherita pizza and prepare it for adding to the cart"

Return a JSON object of the form:
{
  "steps": [
    { "index": 1, "goal": "...", "mode": "navigation" },
    { "index": 2, "goal": "...", "mode": "interaction" }
  ]
}

Do not include any other fields.
Keep steps concise but informative.
`

func (p *OpenAIPlanner) BuildPlan(ctx context.Context, task string) (*Plan, error) {
	userMsg := fmt.Sprintf("User task:\n%s\n\nProduce 3-7 high-level steps.", task)

	req := openai.ChatCompletionRequest{
		Model: openai.GPT4oMini,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: plannerSystemPrompt,
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: userMsg,
			},
		},
		ResponseFormat: &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		},
	}

	resp, err := p.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("OpenAI planner error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("planner returned no choices")
	}

	content := resp.Choices[0].Message.Content

	var plan Plan
	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		return nil, fmt.Errorf("planner JSON parse error: %w | content: %s", err, content)
	}

	// Нормализуем индексы и режимы на всякий случай
	for i := range plan.Steps {
		if plan.Steps[i].Index == 0 {
			plan.Steps[i].Index = i + 1
		}
		mode := strings.ToLower(strings.TrimSpace(plan.Steps[i].Mode))
		if mode != ModeNavigation && mode != ModeInteraction {
			// простая эвристика: всё, что похоже на "search / go to / open" – navigation
			goalLower := strings.ToLower(plan.Steps[i].Goal)
			if strings.Contains(goalLower, "search") ||
				strings.Contains(goalLower, "go to") ||
				strings.Contains(goalLower, "open") {
				mode = ModeNavigation
			} else {
				mode = ModeInteraction
			}
		}
		plan.Steps[i].Mode = mode
	}

	return &plan, nil
}
