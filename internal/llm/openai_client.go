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

const systemPrompt = `
You are an autonomous browser agent.

You receive a textual representation of the current page (DOM tree).
Interactive elements are shown like:
  [12] <button label="Sepete ekle" kind="button" context="dialog">
  [25] <a label="Pizza" kind="link" href="/yemek/restoranlar/?cuisines=...">

Non-interactive lines are just plain text / headings.

ATTRIBUTES
- label="..."      — text visible to user: button caption, link text, placeholder etc.
- kind="..."       — button, link, input, textarea, select, combobox, menuitem, option, ...
- context="dialog" — element is inside an active dialog / modal. 
                     When a dialog is open, the DOM tree usually contains ONLY this dialog.

IMPORTANT: YOU CANNOT NAVIGATE BY URL.
You must never invent or use URLs directly. All navigation must be done by
clicking interactive elements (links, buttons, menu items, etc.) or typing
into fields and pressing Enter when needed.

A dialog (modal):
- If the tree starts with a line like "=== ACTIVE DIALOG ===" or elements have context="dialog",
  that means a modal is open.
- While a dialog is open, you must finish the flow inside it (choose required options,
  click the primary button like "Sepete ekle", "Add to cart", "Confirm", etc.)
  before interacting with anything else.

ALLOWED ACTIONS:
You ONLY have these action types:
- "click"  — click on an interactive element by its numeric id.
- "type"   — type text into an input/textarea by id (optionally press Enter).
- "finish" — stop when the task is reasonably completed or cannot be completed automatically.

RESPONSE FORMAT (STRICT):
Return a SINGLE JSON object:
{
  "thought": "Brief reasoning about current state and the next step",
  "action": {
    "type": "click" | "type" | "finish",
    "target_id": 12,        // integer id from the tree (for click/type)
    "text": "some text",    // only for type
    "submit": true          // if true, press Enter after typing
  }
}

GUIDELINES:

1) Navigation and search
   - To change page/section, click links/buttons in the DOM tree.
   - For search, type into the search input (kind="input" or "search") and set submit=true
     so that Enter is pressed.

2) Dialogs / modals
   - When a dialog is shown (context="dialog" or "=== ACTIVE DIALOG ==="),
     focus only on elements inside it.
   - Typical steps:
       * if there are required choices (selects, options), click them first;
       * then click the primary confirming button (often at the bottom, with a price or
         label like "Sepete ekle", "Add", "Confirm").
   - Do not click background elements visible behind the dialog.

3) Selects / dropdowns / options
   - For selects or comboboxes:
       * First click the select field (kind="select" or "combobox") to open options.
       * On the next step click an option (kind="menuitem" or "option") whose label matches
         the desired choice.
       * Repeat until all important fields are filled.

4) Avoid loops
   - Do not repeat the exact same click / type on the same page if it didn't change anything.
   - If after several reasonable attempts the flow clearly requires human actions
     (e.g. complex payment), use "finish" and explain the situation in the thought.

Remember: no direct URL navigation. Everything is done via clicks and typing based on the DOM tree only.
`

func (c *OpenAIClient) DecideAction(input DecisionInput) (*DecisionOutput, error) {
	userMessage := fmt.Sprintf(`
USER TASK:
%s

CURRENT URL:
%s

PREVIOUS STEPS (summary, optional):
%s

PAGE TREE:
%s
`, input.Task, input.CurrentURL, input.History, input.DOMTree)

	// защита от слишком длинного промпта
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

	// Защитный пост-процессинг: если модель всё-таки вернула "navigate" — запрещаем
	if output.Action.Type == ActionNavigate {
		// если есть target_id, превращаем навигацию в клик по этому элементу
		if output.Action.TargetID != 0 {
			output.Thought += " | SYSTEM: 'navigate' is not allowed; converted to 'click' on the same target."
			output.Action.Type = ActionClick
			output.Action.URL = ""
		} else {
			output.Thought += " | SYSTEM: 'navigate' is not allowed and has no target; finishing."
			output.Action.Type = ActionFinish
			output.Action.URL = ""
		}
	}

	return &output, nil
}
