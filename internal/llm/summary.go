package llm

import (
	"context"
	"fmt"
	"strings"

	openai "github.com/sashabaranov/go-openai"
)

func (c *OpenAIClient) SummarizeRun(input SummaryInput) (string, error) {
	ctx := context.Background()

	var sb strings.Builder
	sb.WriteString("TASK:\n" + input.Task + "\n\n")
	sb.WriteString("EXIT_REASON:\n" + input.ExitReason + "\n\n")
	sb.WriteString("DURATION:\n" + input.Duration + "\n\n")

	if input.FinalURL != "" {
		sb.WriteString("FINAL_URL:\n" + input.FinalURL + "\n\n")
	}

	if len(input.Steps) > 0 {
		sb.WriteString("STEPS:\n")
		for _, s := range input.Steps {
			sb.WriteString(s + "\n")
		}
	}

	resp, err := c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: summarySystemPrompt},
			{Role: openai.ChatMessageRoleUser, Content: sb.String()},
		},
		Temperature: 0.2,
		MaxTokens:   600,
	})
	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no summary choices")
	}

	return resp.Choices[0].Message.Content, nil
}
