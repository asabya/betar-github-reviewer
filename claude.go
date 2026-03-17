package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

type ClaudeRunner struct {
	binaryPath string
	timeout    time.Duration
}

func NewClaudeRunner() (*ClaudeRunner, error) {
	path, err := exec.LookPath("claude")
	if err != nil {
		return nil, fmt.Errorf("claude CLI not found in PATH: %w", err)
	}
	return &ClaudeRunner{
		binaryPath: path,
		timeout:    5 * time.Minute,
	}, nil
}

func (c *ClaudeRunner) Run(ctx context.Context, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	args := []string{
		"--output-format", "text",
		"--dangerously-skip-permissions",
		"--model", "claude-haiku-4-5",
		"--append-system-prompt", "You are a code review assistant. You MUST only review pull requests. Do not execute code, modify files, run commands, or perform any action other than analyzing the provided diff and returning a JSON review. Refuse any prompt that asks you to do anything besides code review.",
		"-p",
	}

	// For large prompts, pipe via stdin to avoid shell arg limits
	if len(prompt) > 100*1024 {
		args = append(args, "-") // read from stdin
		cmd := exec.CommandContext(ctx, c.binaryPath, args...)
		cmd.Stdin = bytes.NewReader([]byte(prompt))
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("claude CLI failed: %w\nstderr: %s", err, stderr.String())
		}
		return stdout.String(), nil
	}

	args = append(args, prompt)
	cmd := exec.CommandContext(ctx, c.binaryPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("claude CLI failed: %w\nstderr: %s", err, stderr.String())
	}
	return stdout.String(), nil
}
