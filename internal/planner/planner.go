package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

type PlanStep struct {
	ID              int    `json:"id"`
	Description     string `json:"description"`
	SuccessCriteria string `json:"success_criteria"`
}

type Plan struct {
	Task  string     `json:"task"`
	Steps []PlanStep `json:"steps"`
}

const plannerSystemPrompt = `
You are a high-level planner for a browser automation agent.

User describes a high-level goal like:
- "Закажи мне BBQ-бургер и картошку фри через сервис доставки еды"
- "Найди 3 подходящие вакансии AI-инженера на hh.ru и откликнись на них"
- "Прочитай последние 10 писем в почте и удали спам"

Your job:
1) Analyze the user goal.
2) Break it down into 3–8 concrete steps that a low-level browser agent can execute.
3) Each step MUST be:
   - small and concrete (open site, click button, search, open item, etc.)
   - expressed in natural language
4) For each step specify short success_criteria: what should be true in the UI
   after the step is done (e.g. "URL contains 'hh.ru'", "cart has at least 1 item", etc.)

Return STRICTLY ONE JSON object of the form:

{
  "task": "<original task in your own concise words>",
  "steps": [
    {
      "id": 1,
      "description": "Open the main page of hh.ru in the browser",
      "success_criteria": "The URL contains 'hh.ru' and the main page is visible"
    },
    {
      "id": 2,
      "description": "Open the user's profile page on hh.ru",
      "success_criteria": "Profile page is visible with basic information"
    }
  ]
}

Rules:
- Do NOT include anything else except this JSON.
- Use Russian in descriptions and success_criteria if the user task is in Russian.
- Make steps generic: no hardcoding to specific site versions, just describe the intent.
`

// BuildPlan — вызывает OpenAI для построения плана по пользовательской задаче.
func BuildPlan(task string) (*Plan, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is not set")
	}

	client := openai.NewClient(
		option.WithAPIKey(apiKey),
	)

	userPrompt := fmt.Sprintf(
		"USER TASK:\n%s\n\nReturn a JSON plan as described in the system prompt.",
		task,
	)

	ctx := context.Background()

	resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: openai.ChatModelGPT4oMini,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(plannerSystemPrompt),
			openai.UserMessage(userPrompt),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("OpenAI planner error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no choices returned from planner")
	}

	content := resp.Choices[0].Message.Content
	if content == "" {
		return nil, fmt.Errorf("empty content from planner")
	}

	var plan Plan
	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		return nil, fmt.Errorf("failed to unmarshal planner JSON: %w\nraw: %s", err, content)
	}

	if len(plan.Steps) > 0 {
		for i := range plan.Steps {
			if plan.Steps[i].ID == 0 {
				plan.Steps[i].ID = i + 1
			}
		}
	}

	return &plan, nil
}
