package llm

type ActionType string

const (
	ActionClick     ActionType = "click"
	ActionTypeInput ActionType = "type"
	ActionScroll    ActionType = "scroll_down"
	ActionFinish    ActionType = "finish"
)

type Action struct {
	Type     ActionType `json:"type"`
	TargetID int        `json:"target_id,omitempty"`
	Text     string     `json:"text,omitempty"`
	Submit   bool       `json:"submit,omitempty"`

	// --- SECURITY FIELDS ---
	IsDestructive     bool   `json:"is_destructive,omitempty"`     // Is this action dangerous?
	DestructiveReason string `json:"destructive_reason,omitempty"` // Why? (e.g., "Deleting file")
}

type DecisionInput struct {
	Task             string
	DOMTree          string
	CurrentURL       string
	History          string
	ScreenshotBase64 string
}

type DecisionOutput struct {
	Thought  string `json:"thought"`
	StepDone bool   `json:"step_done"`
	Action   Action `json:"action"`
}

type Client interface {
	DecideAction(input DecisionInput) (*DecisionOutput, error)
}
