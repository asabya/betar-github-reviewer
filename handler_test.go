package main

import (
	"testing"
)

func TestParsePRReference(t *testing.T) {
	tests := []struct {
		input  string
		owner  string
		repo   string
		number int
		ok     bool
	}{
		{"https://github.com/asabya/betar/pull/42", "asabya", "betar", 42, true},
		{"https://github.com/org/repo-name/pull/1", "org", "repo-name", 1, true},
		{"http://github.com/foo/bar/pull/100", "foo", "bar", 100, true},
		{"asabya/betar#42", "asabya", "betar", 42, true},
		{"org/repo-name#1", "org", "repo-name", 1, true},
		{"not-a-pr-url", "", "", 0, false},
		{"", "", "", 0, false},
		{"github.com/foo/bar/pull/1", "", "", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			owner, repo, number, err := ParsePRReference(tt.input)
			if tt.ok {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if owner != tt.owner || repo != tt.repo || number != tt.number {
					t.Fatalf("got %s/%s#%d, want %s/%s#%d", owner, repo, number, tt.owner, tt.repo, tt.number)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error for input %q", tt.input)
				}
			}
		})
	}
}

func TestParseReviewResponse(t *testing.T) {
	t.Run("valid JSON", func(t *testing.T) {
		input := `{"summary":"Looks good","event":"APPROVE","comments":[{"path":"main.go","line":10,"body":"Nice!"}]}`
		review, err := parseReviewResponse(input)
		if err != nil {
			t.Fatal(err)
		}
		if review.Body != "Looks good" {
			t.Errorf("body = %q, want %q", review.Body, "Looks good")
		}
		if review.Event != "APPROVE" {
			t.Errorf("event = %q, want APPROVE", review.Event)
		}
		if len(review.Comments) != 1 {
			t.Fatalf("comments = %d, want 1", len(review.Comments))
		}
		if review.Comments[0].Path != "main.go" {
			t.Errorf("comment path = %q, want main.go", review.Comments[0].Path)
		}
	})

	t.Run("JSON in markdown fences", func(t *testing.T) {
		input := "```json\n{\"summary\":\"OK\",\"event\":\"COMMENT\",\"comments\":[]}\n```"
		review, err := parseReviewResponse(input)
		if err != nil {
			t.Fatal(err)
		}
		if review.Body != "OK" {
			t.Errorf("body = %q, want OK", review.Body)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		_, err := parseReviewResponse("This is just text, not JSON")
		if err == nil {
			t.Fatal("expected error for non-JSON input")
		}
	})

	t.Run("empty event defaults to COMMENT", func(t *testing.T) {
		input := `{"summary":"test","event":"","comments":[]}`
		review, err := parseReviewResponse(input)
		if err != nil {
			t.Fatal(err)
		}
		if review.Event != "COMMENT" {
			t.Errorf("event = %q, want COMMENT", review.Event)
		}
	})
}

func TestTruncateDiff(t *testing.T) {
	diff := "line1\nline2\nline3\nline4\nline5\n"
	result := truncateDiff(diff, 15)
	if len(result) > 40 { // some room for the truncation message
		t.Errorf("truncated diff too long: %d bytes", len(result))
	}
	if result == diff {
		t.Error("diff should have been truncated")
	}
}

func TestBuildReviewPrompt(t *testing.T) {
	info := &PRInfo{
		Title:       "Test PR",
		Description: "A test",
		Author:      "alice",
		BaseBranch:  "main",
		HeadBranch:  "feature",
	}
	prompt := buildReviewPrompt("owner", "repo", 1, info, "diff content", false)
	if prompt == "" {
		t.Fatal("prompt should not be empty")
	}
	if !contains(prompt, "owner/repo#1") {
		t.Error("prompt should contain PR reference")
	}
	if !contains(prompt, "Test PR") {
		t.Error("prompt should contain PR title")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchString(s, sub)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
