package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

const maxDiffSize = 100 * 1024 // 100KB

type ReviewHandler struct {
	github *GitHubClient
	claude *ClaudeRunner
}

func (h *ReviewHandler) Handle(ctx context.Context, agentID, input string) (string, error) {
	owner, repo, number, err := ParsePRReference(input)
	if err != nil {
		return "", err
	}

	log.Printf("Reviewing PR %s/%s#%d", owner, repo, number)

	info, err := h.github.GetPRInfo(ctx, owner, repo, number)
	if err != nil {
		return "", fmt.Errorf("get PR info: %w", err)
	}

	diff, err := h.github.GetPRDiff(ctx, owner, repo, number)
	if err != nil {
		return "", fmt.Errorf("get PR diff: %w", err)
	}

	truncated := false
	if len(diff) > maxDiffSize {
		diff = truncateDiff(diff, maxDiffSize)
		truncated = true
	}

	prompt := buildReviewPrompt(owner, repo, number, info, diff, truncated)

	log.Printf("Running Claude CLI for review...")
	output, err := h.claude.Run(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("claude review: %w", err)
	}

	review, err := parseReviewResponse(output)
	if err != nil {
		log.Printf("Failed to parse structured review, using raw text: %v", err)
		review = &Review{
			Body:  output,
			Event: "COMMENT",
		}
	}

	if err := h.github.PostReview(ctx, owner, repo, number, review); err != nil {
		return "", fmt.Errorf("post review: %w", err)
	}

	summary := fmt.Sprintf("Review posted on %s/%s#%d: %s (%d inline comments)",
		owner, repo, number, review.Event, len(review.Comments))
	log.Println(summary)
	return summary, nil
}

func buildReviewPrompt(owner, repo string, number int, info *PRInfo, diff string, truncated bool) string {
	var b strings.Builder
	b.WriteString("Review this GitHub Pull Request.\n\n")
	fmt.Fprintf(&b, "PR: %s/%s#%d\n", owner, repo, number)
	fmt.Fprintf(&b, "Title: %s\n", info.Title)
	fmt.Fprintf(&b, "Author: %s\n", info.Author)
	fmt.Fprintf(&b, "Base: %s ← %s\n", info.BaseBranch, info.HeadBranch)
	if info.Description != "" {
		fmt.Fprintf(&b, "Description:\n%s\n", info.Description)
	}
	if truncated {
		b.WriteString("\nNote: The diff was truncated to fit within size limits. File headers are preserved.\n")
	}
	fmt.Fprintf(&b, "\nDiff:\n```\n%s\n```\n", diff)
	b.WriteString(`
Provide your code review as JSON with the following structure:
{"summary":"overall review summary","event":"COMMENT","comments":[{"path":"relative/file.go","line":42,"body":"comment text"}]}

The "event" field must be one of: "COMMENT", "APPROVE", or "REQUEST_CHANGES".
The "comments" array contains inline comments on specific lines.
Respond ONLY with the JSON object, no markdown fences or extra text.
`)
	return b.String()
}

func truncateDiff(diff string, maxSize int) string {
	lines := strings.Split(diff, "\n")
	var result strings.Builder
	for _, line := range lines {
		if result.Len()+len(line)+1 > maxSize {
			result.WriteString("\n... (diff truncated)\n")
			break
		}
		result.WriteString(line)
		result.WriteByte('\n')
	}
	return result.String()
}

func parseReviewResponse(output string) (*Review, error) {
	output = strings.TrimSpace(output)
	// Strip markdown code fences if present
	if strings.HasPrefix(output, "```") {
		lines := strings.SplitN(output, "\n", 2)
		if len(lines) > 1 {
			output = lines[1]
		}
		if idx := strings.LastIndex(output, "```"); idx > 0 {
			output = output[:idx]
		}
		output = strings.TrimSpace(output)
	}

	var review Review
	if err := json.Unmarshal([]byte(output), &review); err != nil {
		return nil, fmt.Errorf("parse review JSON: %w", err)
	}

	// Validate event field
	switch review.Event {
	case "COMMENT", "APPROVE", "REQUEST_CHANGES":
		// ok
	case "":
		review.Event = "COMMENT"
	default:
		review.Event = "COMMENT"
	}

	return &review, nil
}
