package main

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type GitHubClient struct {
	appID      int64
	privateKey *rsa.PrivateKey
	httpClient *http.Client

	mu     sync.Mutex
	tokens map[string]*tokenEntry // keyed by "owner/repo"
}

type tokenEntry struct {
	installationID int64
	token          string
	expiresAt      time.Time
}

type PRInfo struct {
	Title       string
	Description string
	Author      string
	BaseBranch  string
	HeadBranch  string
}

type Review struct {
	Body     string          `json:"summary"`
	Event    string          `json:"event"`
	Comments []ReviewComment `json:"comments"`
}

type ReviewComment struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Body string `json:"body"`
}

func NewGitHubClient(appID int64, pemPath string) (*GitHubClient, error) {
	data, err := os.ReadFile(pemPath)
	if err != nil {
		return nil, fmt.Errorf("read PEM file: %w", err)
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in %s", pemPath)
	}

	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		// Try PKCS8 as fallback
		parsed, err2 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err2 != nil {
			return nil, fmt.Errorf("parse private key: %w (pkcs8: %w)", err, err2)
		}
		var ok bool
		key, ok = parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("PEM key is not RSA")
		}
	}

	return &GitHubClient{
		appID:      appID,
		privateKey: key,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		tokens:     make(map[string]*tokenEntry),
	}, nil
}

// getInstallationID looks up the installation ID for a given repo.
func (g *GitHubClient) getInstallationID(ctx context.Context, owner, repo string) (int64, error) {
	jwt, err := g.generateJWT()
	if err != nil {
		return 0, fmt.Errorf("generate JWT: %w", err)
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/installation", owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("lookup installation: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return 0, fmt.Errorf("GitHub App is not installed on %s/%s", owner, repo)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("lookup installation (%d): %s", resp.StatusCode, body)
	}

	var result struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode installation: %w", err)
	}
	return result.ID, nil
}

// getTokenForRepo returns a cached or fresh installation token for the given repo.
func (g *GitHubClient) getTokenForRepo(ctx context.Context, owner, repo string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	key := owner + "/" + repo
	if entry, ok := g.tokens[key]; ok {
		if time.Now().Before(entry.expiresAt.Add(-time.Minute)) {
			return entry.token, nil
		}
	}

	// Look up installation ID (outside lock would be better but keep it simple)
	g.mu.Unlock()
	installID, err := g.getInstallationID(ctx, owner, repo)
	g.mu.Lock()
	if err != nil {
		return "", err
	}

	jwt, err := g.generateJWT()
	if err != nil {
		return "", fmt.Errorf("generate JWT: %w", err)
	}

	url := fmt.Sprintf("https://api.github.com/app/installations/%d/access_tokens", installID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request installation token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("installation token request failed (%d): %s", resp.StatusCode, body)
	}

	var result struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}

	g.tokens[key] = &tokenEntry{
		installationID: installID,
		token:          result.Token,
		expiresAt:      result.ExpiresAt,
	}
	return result.Token, nil
}

// generateJWT creates a GitHub App JWT (RS256).
func (g *GitHubClient) generateJWT() (string, error) {
	now := time.Now()
	header := base64URLEncode([]byte(`{"alg":"RS256","typ":"JWT"}`))

	claims, _ := json.Marshal(map[string]interface{}{
		"iat": now.Add(-60 * time.Second).Unix(),
		"exp": now.Add(10 * time.Minute).Unix(),
		"iss": g.appID,
	})
	payload := base64URLEncode(claims)

	signingInput := header + "." + payload

	h := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(nil, g.privateKey, crypto.SHA256, h[:])
	if err != nil {
		return "", err
	}

	return signingInput + "." + base64URLEncode(sig), nil
}

func base64URLEncode(data []byte) string {
	return strings.TrimRight(base64.URLEncoding.EncodeToString(data), "=")
}

// doRequest executes an authenticated GitHub API request for a specific repo.
func (g *GitHubClient) doRequest(ctx context.Context, owner, repo, method, url string, body io.Reader, accept string) (*http.Response, error) {
	token, err := g.getTokenForRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+token)
	if accept != "" {
		req.Header.Set("Accept", accept)
	} else {
		req.Header.Set("Accept", "application/vnd.github+json")
	}

	return g.httpClient.Do(req)
}

func (g *GitHubClient) GetPRInfo(ctx context.Context, owner, repo string, number int) (*PRInfo, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d", owner, repo, number)
	resp, err := g.doRequest(ctx, owner, repo, http.MethodGet, url, nil, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get PR info (%d): %s", resp.StatusCode, body)
	}

	var pr struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		User  struct {
			Login string `json:"login"`
		} `json:"user"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
		Head struct {
			Ref string `json:"ref"`
		} `json:"head"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return nil, fmt.Errorf("decode PR: %w", err)
	}

	return &PRInfo{
		Title:       pr.Title,
		Description: pr.Body,
		Author:      pr.User.Login,
		BaseBranch:  pr.Base.Ref,
		HeadBranch:  pr.Head.Ref,
	}, nil
}

func (g *GitHubClient) GetPRDiff(ctx context.Context, owner, repo string, number int) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d", owner, repo, number)
	resp, err := g.doRequest(ctx, owner, repo, http.MethodGet, url, nil, "application/vnd.github.v3.diff")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("get PR diff (%d): %s", resp.StatusCode, body)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read diff body: %w", err)
	}
	return string(data), nil
}

func (g *GitHubClient) PostReview(ctx context.Context, owner, repo string, number int, review *Review) error {
	type apiComment struct {
		Path string `json:"path"`
		Line int    `json:"line"`
		Body string `json:"body"`
	}

	payload := struct {
		Body     string       `json:"body"`
		Event    string       `json:"event"`
		Comments []apiComment `json:"comments,omitempty"`
	}{
		Body:  review.Body,
		Event: review.Event,
	}

	for _, c := range review.Comments {
		payload.Comments = append(payload.Comments, apiComment{
			Path: c.Path,
			Line: c.Line,
			Body: c.Body,
		})
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal review: %w", err)
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d/reviews", owner, repo, number)
	resp, err := g.doRequest(ctx, owner, repo, http.MethodPost, url, strings.NewReader(string(data)), "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("post review (%d): %s", resp.StatusCode, body)
	}
	return nil
}

var prURLPattern = regexp.MustCompile(`^https?://github\.com/([^/]+)/([^/]+)/pull/(\d+)`)
var prShortPattern = regexp.MustCompile(`^([^/]+)/([^/#]+)#(\d+)$`)

func ParsePRReference(input string) (owner, repo string, number int, err error) {
	input = strings.TrimSpace(input)

	if m := prURLPattern.FindStringSubmatch(input); m != nil {
		number, _ = strconv.Atoi(m[3])
		return m[1], m[2], number, nil
	}
	if m := prShortPattern.FindStringSubmatch(input); m != nil {
		number, _ = strconv.Atoi(m[3])
		return m[1], m[2], number, nil
	}
	return "", "", 0, fmt.Errorf("invalid PR reference %q: expected https://github.com/owner/repo/pull/N or owner/repo#N", input)
}
