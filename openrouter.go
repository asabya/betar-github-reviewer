package main

import (
	"context"
	"os"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

type OpenRouterClaudeRunner struct {
	client *openai.Client
	timeout    time.Duration
}

func NewOpenRouterClaudeRunner() (*OpenRouterClaudeRunner, error) {
	// In this example, we assume the OpenRouter Claude CLI is available as "or-claude" in PATH.
	// Adjust as needed for your actual CLI tool.
	token := os.Getenv("OPENROUTER_API_KEY")
	client := openai.NewClient(token)
	return &OpenRouterClaudeRunner{
		client: client,
		timeout:    5 * time.Minute,
	}, nil
}

func (c *OpenRouterClaudeRunner) Run(ctx context.Context, prompt string) (string, error) {
	// Implement the logic to call your OpenRouter Claude CLI with the given prompt and return the output.
	// This will likely involve similar code to the ClaudeRunner, but adjusted for your specific CLI.
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	assistantID := "claude-4-reviewer" // This should match the assistant ID registered on OpenRouter
	resp, err := c.client.CreateThreadAndRun(ctx, openai.CreateThreadAndRunRequest{
		RunRequest: openai.RunRequest{
			AssistantID: assistantID,
		},
		Thread: openai.ThreadRequest{
			Messages: []openai.ThreadMessage{
				{
					Role: openai.ThreadMessageRoleUser,
					Content: prompt,
				},
			},
		},
	})

	if err != nil {
		return "", err
	}
	
	return resp.Instructions, nil
}