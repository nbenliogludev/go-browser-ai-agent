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

	taskForLLM := fmt.Sprintf(
		"GLOBAL USER TASK: %s\n\nCURRENT PLAN STEP (navigation): %s\n\n"+
			"Use the CURRENT PLAN STEP as a high-level description of WHAT should be achieved, "+
			"not as a strict sequence of UI labels or exact paths.\n\n"+
			"In this mode you focus PRIMARILY on navigation:\n"+
			"- Prefer links, buttons, categories and lists in region=\"main\" that move you closer to this step's goal.\n"+
			"- You MAY type into local search fields or filters (kind=\"search\" or relevant inputs) "+
			"to refine the visible list of items, as long as you stay in the same site section.\n"+
			"- Avoid using global header navigation (region=\"header\") unless the user explicitly asked "+
			"for a global section or there is clearly no relevant path in region=\"main\".\n"+
			"- Do NOT perform complex confirmations or multi-step forms in this mode; "+
			"leave them for the interaction step.\n\n"+
			"Set \"step_done\" to true as soon as the CURRENT PAGE/SECTION clearly matches this step's goal "+
			"(for example, a restaurant menu with pizzas is already open). "+
			"\"step_done\" refers ONLY to this plan step, NOT to the whole task.",
		env.Task, stepGoal,
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

	taskForLLM := fmt.Sprintf(
		"GLOBAL USER TASK: %s\n\nCURRENT PLAN STEP (interaction): %s\n\n"+
			"You are ALREADY on the relevant page or modal for this step.\n"+
			"- Focus on choosing required options (selects, checkboxes, quantity) and pressing the primary confirm/add/apply button.\n"+
			"- Do NOT navigate to other pages in this mode.\n"+
			"- As soon as the requested interaction for THIS STEP is clearly completed (for example: dialog is closed, item is in cart, form is submitted), "+
			"set \"step_done\" to true in your JSON response. \"step_done\" refers ONLY to this plan step, NOT to the whole task.",
		env.Task, stepGoal,
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
