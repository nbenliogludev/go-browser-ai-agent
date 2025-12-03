package llm

import (
	"github.com/nbenliogludev/go-browser-ai-agent/internal/browser"
)

type ActionType string

const (
	ActionClick    ActionType = "click"
	ActionNavigate ActionType = "navigate"
	ActionTypeText ActionType = "type"
	ActionFinish   ActionType = "finish"
)

type Action struct {
	Type     ActionType `json:"type"`
	TargetID string     `json:"target_id,omitempty"`
	Text     string     `json:"text,omitempty"`
	URL      string     `json:"url,omitempty"`
}

type DecisionInput struct {
	Task     string
	Snapshot *browser.PageSnapshot
}

type DecisionOutput struct {
	Thought string `json:"thought"`
	Action  Action `json:"action"`
}

type Client interface {
	DecideAction(input DecisionInput) (*DecisionOutput, error)
}
