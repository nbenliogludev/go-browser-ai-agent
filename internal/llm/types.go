package llm

import (
	"github.com/nbenliogludev/go-browser-ai-agent/internal/browser"
)

type ActionType string

const (
	ActionClick       ActionType = "click"
	ActionNavigate    ActionType = "navigate"
	ActionTypeText    ActionType = "type"
	ActionFinish      ActionType = "finish"
	ActionReadContent ActionType = "read_content" // новый тип для чтения текста страницы
)

type Action struct {
	Type     ActionType `json:"type"`
	TargetID string     `json:"target_id,omitempty"`
	Text     string     `json:"text,omitempty"`
	URL      string     `json:"url,omitempty"`

	// Используется только для read_content:
	// ограничивает количество символов возвращаемого текста,
	// чтобы не раздувать контекст модели.
	MaxChars int `json:"max_chars,omitempty"`
}

type DecisionInput struct {
	// Высокоуровневая задача пользователя.
	Task string

	// Снимок страницы с интерактивными элементами.
	Snapshot *browser.PageSnapshot
}

type DecisionOutput struct {
	Thought string `json:"thought"`
	Action  Action `json:"action"`
}

// Клиент LLM, который принимает DecisionInput и возвращает DecisionOutput.
type Client interface {
	DecideAction(input DecisionInput) (*DecisionOutput, error)
}
