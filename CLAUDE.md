# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**betar-github-reviewer** is a pay-per-review GitHub PR reviewer bot built on the Betar decentralized agent marketplace. Contributors tag `@betar-pr-bot` in a PR comment, which triggers a GitHub Action that discovers the reviewer agent via P2P, pays with x402 (USDC on Base Sepolia), and receives an AI-powered code review posted back to the PR.

## Build & Test Commands

```bash
# Build seller bot (persistent server)
go build -o betar-github-reviewer .

# Build buyer trigger (ephemeral, runs in GitHub Actions)
go build -o betar-trigger ./cmd/trigger/

# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run a single test
go test -v -run TestParsePRReference ./...
```

No Makefile — standard Go toolchain only.

## Architecture

Two-tier marketplace model with a single Go module (`github.com/asabya/betar-github-reviewer`):

### Seller Bot (persistent server)
All source files in the root package:

- **main.go** — Entry point. Initializes Betar SDK client, GitHub App client, Claude runner. Registers agent on marketplace with name/price and serves incoming tasks.
- **handler.go** — `ReviewHandler.Handle()` orchestrates the review: parses PR reference, fetches diff via GitHub API, builds prompt, runs Claude CLI, parses JSON response, posts review.
- **github.go** — `GitHubClient` with JWT-based GitHub App auth, installation token caching per repo, methods for fetching PR info/diff and posting reviews.
- **claude.go** — `ClaudeRunner` wraps the local Claude CLI binary (`claude --output-format text -p`), 5-minute timeout, pipes large prompts via stdin.

### Buyer Trigger (ephemeral GitHub Actions binary)
- **cmd/trigger/main.go** — Joins Betar network, discovers agent by name (retries up to 5x), executes with PR reference. SDK handles x402 payment automatically.

### Key Data Flow
1. PR comment triggers GitHub Action → builds & runs buyer trigger
2. Trigger discovers seller agent via Betar P2P network, pays USDC
3. Seller receives task, parses `owner/repo#number` reference
4. Fetches PR diff (max 100KB) via GitHub App API
5. Claude CLI reviews the diff, returns JSON: `{summary, event, comments[]}`
6. Review posted to GitHub (event: COMMENT/APPROVE/REQUEST_CHANGES, with inline comments)

### Key Dependencies
- **github.com/asabya/betar** — Betar SDK (local replace directive pointing to `../betar`)
- **go-libp2p** — P2P networking
- **go-ethereum** — Ethereum/x402 payment integration
- **go.uber.org/fx** — Dependency injection
- **go.uber.org/zap** — Logging

## Configuration

Environment variables (see `.env.example`):
- `ETHEREUM_PRIVATE_KEY` — hex-encoded secp256k1 key
- `BOOTSTRAP_PEERS` — comma-separated Betar node multiaddrs
- `GITHUB_APP_ID`, `GITHUB_APP_PRIVATE_KEY_PATH` — GitHub App credentials
- `AGENT_NAME` (default: `pr-reviewer`), `AGENT_PRICE` (default: `0.005` USDC)
- `BETAR_DATA_DIR` (default: `~/.betar-reviewer`)

## GitHub Action

Defined in `action.yml`. Buyer workflow example in `.github/workflows/pr-review.yml` — triggers on `issue_comment` events containing `@betar-pr-bot`.
