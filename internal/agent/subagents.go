package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/nbenliogludev/go-browser-ai-agent/internal/llm"
	"github.com/nbenliogludev/go-browser-ai-agent/internal/planner"
)

// EnvState — environment snapshot for sub-agents.
type EnvState struct {
	Task        string
	Plan        *planner.Plan
	CurrentStep int
	URL         string
	DOMTree     string
	History     []string

	// Показываем агенту, что сейчас есть активный диалог
	HasDialog bool
}

// SubAgent — common interface for sub-agents.
type SubAgent interface {
	Name() string
	// Step returns:
	// - next low-level browser action
	// - LLM "thought" explanation
	// - stepDone: whether the CURRENT plan step should be considered completed
	Step(ctx context.Context, env EnvState) (*llm.Action, string, bool, error)
}

//
// ---------- NavigatorAgent ----------
//

type NavigatorAgent struct {
	LLM llm.Client
}

func (a *NavigatorAgent) Name() string {
	return "NavigatorAgent"
}

func (a *NavigatorAgent) Step(ctx context.Context, env EnvState) (*llm.Action, string, bool, error) {
	historyStr := strings.Join(env.History, "\n")

	var stepGoal string
	if env.Plan != nil && env.CurrentStep < len(env.Plan.Steps) {
		stepGoal = env.Plan.Steps[env.CurrentStep].Goal
	}

	dialogState := "No active dialog is visible."
	if env.HasDialog {
		dialogState = "An ACTIVE DIALOG/MODAL is currently visible. However, in navigation mode you normally should not stay inside dialogs for long; finish them quickly or close them."
	}

	taskForLLM := fmt.Sprintf(
		"GLOBAL USER TASK: %s\n\nCURRENT PLAN STEP (navigation): %s\n\n"+
			"DIALOG STATE: %s\n\n"+
			"Right now you are working ONLY on this CURRENT PLAN STEP.\n"+
			"- Focus purely on navigation: choose links, buttons, categories or lists that move you closer to this goal.\n"+
			"- Do NOT fill forms or confirm dialogs in this mode.\n"+
			"- As soon as the CURRENT PAGE/SECTION clearly matches this step's goal "+
			"(for example, the desired restaurant/product/category page or search results are already open), "+
			"set \"step_done\" to true. \"step_done\" refers ONLY to this plan step, NOT to the whole task.",
		env.Task, stepGoal, dialogState,
	)

	out, err := a.LLM.DecideAction(llm.DecisionInput{
		Task:       taskForLLM,
		DOMTree:    env.DOMTree,
		CurrentURL: env.URL,
		History:    historyStr,
	})
	if err != nil {
		return nil, "", false, err
	}

	return &out.Action, out.Thought, out.StepDone, nil
}

//
// ---------- InteractionAgent ----------
//

type InteractionAgent struct {
	LLM llm.Client
}

func (a *InteractionAgent) Name() string {
	return "InteractionAgent"
}

func (a *InteractionAgent) Step(ctx context.Context, env EnvState) (*llm.Action, string, bool, error) {
	historyStr := strings.Join(env.History, "\n")

	var stepGoal string
	if env.Plan != nil && env.CurrentStep < len(env.Plan.Steps) {
		stepGoal = env.Plan.Steps[env.CurrentStep].Goal
	}

	dialogState := "No active dialog is visible."
	if env.HasDialog {
		dialogState = "An ACTIVE DIALOG/MODAL is visible. You MUST finish the flow inside this dialog (for example, choose options and click the primary confirm/add button) BEFORE touching anything outside of it."
	}

	taskForLLM := fmt.Sprintf(
		"GLOBAL USER TASK: %s\n\nCURRENT PLAN STEP (interaction): %s\n\n"+
			"DIALOG STATE: %s\n\n"+
			"You are ALREADY on the relevant page or modal for this step.\n"+
			"- Focus on choosing required options (selects, checkboxes, quantity) and pressing the primary confirm/add/apply button.\n"+
			"- Do NOT navigate to other pages in this mode (no switching categories or opening unrelated products).\n"+
			"- When the user asks to add ONE item (for example: 'добавь в корзину пиццу', 'add a pizza to the cart', 'add the product to the basket'), "+
			"you MUST add exactly ONE best matching item. Do NOT add multiple similar items or extra menus unless the instruction explicitly asks for more than one (e.g. 'две пиццы', '3 burgers').\n"+
			"- If the page/cart already clearly shows that the requested item is present (or subtotal has changed accordingly), "+
			"treat the interaction for THIS STEP as completed, avoid adding more of the same item and set \"step_done\" to true.\n"+
			"- As soon as the requested interaction for THIS STEP is clearly completed (for example: dialog is closed, item is in cart, form is submitted), "+
			"set \"step_done\" to true in your JSON response. \"step_done\" refers ONLY to this plan step, NOT to the whole task.",
		env.Task, stepGoal, dialogState,
	)

	out, err := a.LLM.DecideAction(llm.DecisionInput{
		Task:       taskForLLM,
		DOMTree:    env.DOMTree,
		CurrentURL: env.URL,
		History:    historyStr,
	})
	if err != nil {
		return nil, "", false, err
	}

	return &out.Action, out.Thought, out.StepDone, nil
}
