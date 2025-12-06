package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/nbenliogludev/go-browser-ai-agent/internal/llm"
	"github.com/nbenliogludev/go-browser-ai-agent/internal/planner"
)

// EnvState — то, что получают под-агенты от оркестратора.
type EnvState struct {
	Task        string
	Plan        *planner.Plan
	CurrentStep int
	URL         string
	DOMTree     string
	History     []string
}

// SubAgent — общий интерфейс под-агентов.
type SubAgent interface {
	Name() string
	Step(ctx context.Context, env EnvState) (*llm.Action, string, error)
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

func (a *NavigatorAgent) Step(ctx context.Context, env EnvState) (*llm.Action, string, error) {
	historyStr := strings.Join(env.History, "\n")

	var stepGoal string
	if env.Plan != nil && env.CurrentStep < len(env.Plan.Steps) {
		stepGoal = env.Plan.Steps[env.CurrentStep].Goal
	}

	taskForLLM := fmt.Sprintf(
		"USER TASK: %s\n\nCURRENT PLAN STEP (navigation): %s\n"+
			"Focus ONLY on navigation: choose links, buttons, categories or lists that move you closer to the goal. "+
			"Do NOT fill forms or confirm dialogs here.",
		env.Task, stepGoal,
	)

	out, err := a.LLM.DecideAction(llm.DecisionInput{
		Task:       taskForLLM,
		DOMTree:    env.DOMTree,
		CurrentURL: env.URL,
		History:    historyStr,
	})
	if err != nil {
		return nil, "", err
	}

	return &out.Action, out.Thought, nil
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

func (a *InteractionAgent) Step(ctx context.Context, env EnvState) (*llm.Action, string, error) {
	historyStr := strings.Join(env.History, "\n")

	var stepGoal string
	if env.Plan != nil && env.CurrentStep < len(env.Plan.Steps) {
		stepGoal = env.Plan.Steps[env.CurrentStep].Goal
	}

	taskForLLM := fmt.Sprintf(
		"USER TASK: %s\n\nCURRENT PLAN STEP (interaction): %s\n"+
			"You are ALREADY on the relevant page or modal. "+
			"Focus on choosing required options (selects, checkboxes, quantity) and pressing the primary confirm/add/apply button. "+
			"Do NOT navigate to other pages in this mode.",
		env.Task, stepGoal,
	)

	out, err := a.LLM.DecideAction(llm.DecisionInput{
		Task:       taskForLLM,
		DOMTree:    env.DOMTree,
		CurrentURL: env.URL,
		History:    historyStr,
	})
	if err != nil {
		return nil, "", err
	}

	return &out.Action, out.Thought, nil
}
