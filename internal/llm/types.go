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

	// НОВОЕ ПОЛЕ: Если true, агент нажмет Enter после ввода
	Submit bool `json:"submit,omitempty"`
}

type DecisionInput struct {
	Task       string
	DOMTree    string
	CurrentURL string
}

type DecisionOutput struct {
	Thought string `json:"thought"`
	Action  Action `json:"action"`
}

type Client interface {
	DecideAction(input DecisionInput) (*DecisionOutput, error)
}
