package llm

type ActionType string

const (
	ActionClick     ActionType = "click"
	ActionNavigate  ActionType = "navigate"
	ActionTypeInput ActionType = "type"
	ActionFinish    ActionType = "finish"
)

type Action struct {
	Type     ActionType `json:"type"`
	TargetID int        `json:"target_id,omitempty"`
	Text     string     `json:"text,omitempty"`
	URL      string     `json:"url,omitempty"`
	Submit   bool       `json:"submit,omitempty"`
}

type DecisionInput struct {
	Task       string
	DOMTree    string
	CurrentURL string
	History    string // short description of previous steps
}

type DecisionOutput struct {
	Thought  string `json:"thought"`
	StepDone bool   `json:"step_done"`
	Action   Action `json:"action"`
}

type Client interface {
	DecideAction(input DecisionInput) (*DecisionOutput, error)
}
