package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

func (c *OpenAIClient) DecideAction(input DecisionInput) (*DecisionOutput, error) {
	ctx := context.Background()

	var sb strings.Builder
	sb.WriteString("TASK: " + input.Task + "\n")
	sb.WriteString("URL: " + input.CurrentURL + "\n")

	if input.History != "" {
		sb.WriteString("HISTORY:\n" + input.History + "\n")
	}

	dom := input.DOMTree
	if len(dom) > safeDOMLimit {
		dom = dom[:safeDOMLimit] + "\n...[TRUNCATED]"
	}
	sb.WriteString("\nDOM:\n" + dom)

	parts := []openai.ChatMessagePart{
		{Type: openai.ChatMessagePartTypeText, Text: sb.String()},
	}

	if input.ScreenshotBase64 != "" {
		parts = append(parts, openai.ChatMessagePart{
			Type: openai.ChatMessagePartTypeImageURL,
			ImageURL: &openai.ChatMessageImageURL{
				URL: "data:image/jpeg;base64," + input.ScreenshotBase64,
			},
		})
	}

	var resp openai.ChatCompletionResponse
	var err error

	for attempt := 0; attempt < 5; attempt++ {
		resp, err = c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model: "gpt-4o",
			Messages: []openai.ChatCompletionMessage{
				{Role: openai.ChatMessageRoleSystem, Content: visionSystemPrompt},
				{Role: openai.ChatMessageRoleUser, MultiContent: parts},
			},
			ResponseFormat: &openai.ChatCompletionResponseFormat{
				Type: openai.ChatCompletionResponseFormatTypeJSONObject,
			},
			Temperature: 0,
			MaxTokens:   300,
		})

		if err == nil {
			break
		}

		if strings.Contains(err.Error(), "429") {
			time.Sleep(time.Duration(3*(1<<attempt)) * time.Second)
			continue
		}
		return nil, err
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no choices")
	}

	var out DecisionOutput
	content := strings.Trim(resp.Choices[0].Message.Content, "`")

	if err := json.Unmarshal([]byte(content), &out); err != nil {
		return nil, err
	}

	normalizeActionType(&out.Action)
	return &out, nil
}

func normalizeActionType(a *Action) {
	switch strings.ToLower(string(a.Type)) {
	case "click":
		a.Type = ActionClick
	case "type":
		a.Type = ActionTypeInput
	case "scroll_down":
		a.Type = ActionScroll
	case "finish":
		a.Type = ActionFinish
	default:
		a.Type = ActionScroll
	}
}
