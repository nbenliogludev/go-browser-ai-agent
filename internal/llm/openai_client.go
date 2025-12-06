package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	openai "github.com/sashabaranov/go-openai"
	"github.com/sashabaranov/go-openai/jsonschema"
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

// Tool description for choosing the next browser-agent action.
var browserActionTool = openai.Tool{
	Type: openai.ToolTypeFunction,
	Function: &openai.FunctionDefinition{
		Name:        "decide_browser_action",
		Description: "Chooses the NEXT action of the browser agent based on the user task, page DOM tree, and step history.",
		Parameters: jsonschema.Definition{
			Type: jsonschema.Object,
			Properties: map[string]jsonschema.Definition{
				"thought": {
					Type:        jsonschema.String,
					Description: "A short explanation of why this action is chosen for the current step.",
				},
				"action": {
					Type:        jsonschema.Object,
					Description: "Description of the next agent action.",
					Properties: map[string]jsonschema.Definition{
						"type": {
							Type:        jsonschema.String,
							Enum:        []string{"click", "type", "finish"},
							Description: "Action type: click, type, or finish.",
						},
						"target_id": {
							Type:        jsonschema.Integer,
							Description: "Target element id from the DOM tree for click/type.",
						},
						"text": {
							Type:        jsonschema.String,
							Description: "Text to type (only for type).",
						},
						"submit": {
							Type:        jsonschema.Boolean,
							Description: "If true, press Enter after typing.",
						},
					},
					Required: []string{"type"},
				},
			},
			Required: []string{"thought", "action"},
		},
	},
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

You MUST NOT output plain natural language.
You MUST ALWAYS respond by calling the "decide_browser_action" tool with:
- "thought": your brief reasoning about current state and next step;
- "action": a SINGLE next action that follows the rules above.

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
   - Do NOT repeatedly execute the same short pattern of actions (for example, opening
     the same card and pressing the same primary button again and again).
   - If the history contains "SYSTEM NOTE" lines about loops or forbidden actions/patterns,
     you MUST obey them: do NOT repeat such actions or patterns.
   - If after several reasonable attempts the flow clearly requires human actions
     (e.g. complex payment), use "finish" and explain the situation in the thought.

HISTORY MAY CONTAIN SYSTEM NOTES:
- Some history lines may start with "SYSTEM NOTE:".
- These are constraints from the environment (for example, detected loops).
- You MUST treat them as hard constraints: avoid repeating forbidden actions or patterns
  and prefer choosing a different path or finish if the user goal already looks achieved
  (for example, the cart clearly shows the desired item and quantity).

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

	// Guard against too long prompt
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
		Tools: []openai.Tool{browserActionTool},
		ToolChoice: &openai.ToolChoice{
			// Force the model to always call our function
			Type: openai.ToolTypeFunction,
			Function: openai.ToolFunction{
				Name: browserActionTool.Function.Name, // "decide_browser_action"
			},
		},
		Temperature: 0.2,
	}

	resp, err := c.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("OpenAI error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no response choices")
	}

	msg := resp.Choices[0].Message
	if len(msg.ToolCalls) == 0 {
		return nil, fmt.Errorf("LLM did not call decide_browser_action tool")
	}

	toolCall := msg.ToolCalls[0]
	rawArgs := toolCall.Function.Arguments // JSON string: {"thought": "...", "action": {...}}

	var output DecisionOutput
	if err := json.Unmarshal([]byte(rawArgs), &output); err != nil {
		return nil, fmt.Errorf("json parse error: %w | raw tool args: %s", err, rawArgs)
	}

	// Safety post-processing: if the model still returns "navigate" — disallow it
	if output.Action.Type == ActionNavigate {
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
