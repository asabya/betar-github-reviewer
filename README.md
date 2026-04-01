# betar-github-reviewer

A pay-per-review GitHub PR reviewer bot powered by the [Betar](https://github.com/asabya/betar) decentralized agent marketplace.

## How It Works

```
Contributor tags @betar-pr-bot in a PR comment
  → GitHub Action triggers in the repo
  → Action runs the trigger binary (buyer side)
  → Trigger joins Betar network, discovers the reviewer agent
  → x402 payment (USDC on Base Sepolia) happens automatically
  → Seller bot receives the task over P2P
  → Fetches PR diff via GitHub App, reviews with Claude CLI
  → Posts review comments on the PR
```

Two components:

| Component | Role | Runs where |
|---|---|---|
| **Seller bot** (`main.go`) | Persistent Betar node, GitHub App, Claude CLI | Your server/VPS |
| **Trigger binary** (`cmd/trigger/`) | Ephemeral buyer node, pays & executes | GitHub Actions runner |

## Prerequisites

- Go 1.24+
- [Claude CLI](https://docs.anthropic.com/en/docs/claude-code) installed on the seller machine
- A [GitHub App](https://docs.github.com/en/apps/creating-github-apps) with permissions:
  - Pull requests: Read & Write
  - Contents: Read
- An Ethereum private key with USDC on Base Sepolia (for both seller and buyer wallets)

## Seller Setup (Bot Operator)

1. Clone and build:
   ```bash
   git clone https://github.com/asabya/betar-github-reviewer
   cd betar-github-reviewer
   go build -o betar-github-reviewer .
   ```

2. Configure environment (see `.env.example`):
   ```bash
   export GITHUB_APP_ID=123456
   export GITHUB_APP_PRIVATE_KEY_PATH=/path/to/app.pem
   export ETHEREUM_PRIVATE_KEY=0xabc...
   export AGENT_NAME=pr-reviewer
   export AGENT_PRICE=0.005
   ```

3. Run the bot:
   ```bash
   ./betar-github-reviewer
   ```

4. Note the agent ID from the logs — buyers need this.

## Running with Docker

### Build the image

```bash
docker build -t betar-github-reviewer .
```

### Run with `docker run`

```bash
docker run -d \
  --name betar-github-reviewer \
  --restart unless-stopped \
  -e GITHUB_APP_ID=123456 \
  -e GITHUB_APP_PRIVATE_KEY_PATH=/run/secrets/app.pem \
  -e ETHEREUM_PRIVATE_KEY=0xabc... \
  -e BOOTSTRAP_PEERS=/ip4/.../p2p/... \
  -e AGENT_NAME=pr-reviewer \
  -e AGENT_PRICE=0.005 \
  -v /path/to/app.pem:/run/secrets/app.pem:ro \
  -v betar-data:/data \
  betar-github-reviewer
```

- Mount your GitHub App `.pem` file into the container (the path must match `GITHUB_APP_PRIVATE_KEY_PATH`).
- `/data` is the bot's data directory — the named volume `betar-data` persists state across restarts.

### Run with Docker Compose

1. Copy `.env.example` to `.env` and fill in your values:
   ```bash
   cp .env.example .env
   ```

2. Set `GITHUB_APP_PRIVATE_KEY_PATH` in `.env` to the local path of your `.pem` file (it will be mounted read-only into the container).

3. Start the service:
   ```bash
   docker compose up -d
   ```

4. View logs:
   ```bash
   docker compose logs -f
   ```

## Buyer Setup (Repo Owner)

Buyers add a single workflow file and configure secrets. No code to write.

1. Install the seller's **GitHub App** on your repo (so the bot can post reviews).

2. Add **repository secrets** (Settings → Secrets and variables → Actions):

   | Secret | Description |
   |---|---|
   | `BETAR_ETH_PRIVATE_KEY` | Hex-encoded Ethereum private key (funded with USDC) |
   | `BETAR_BOOTSTRAP_PEERS` | Comma-separated multiaddrs of Betar nodes (optional) |
   | `BETAR_REVIEWER_AGENT_ID` | Agent ID from the seller (optional — defaults to name discovery) |

3. Create `.github/workflows/pr-review.yml` in your repo:

   ```yaml
   name: Betar PR Review
   on:
     issue_comment:
       types: [created]

   jobs:
     review:
       if: |
         github.event.issue.pull_request &&
         contains(github.event.comment.body, '@betar-pr-bot')
       runs-on: ubuntu-latest
       timeout-minutes: 10
       steps:
         - uses: asabya/betar-github-reviewer@v1
           with:
             ethereum-private-key: ${{ secrets.BETAR_ETH_PRIVATE_KEY }}
             bootstrap-peers: ${{ secrets.BETAR_BOOTSTRAP_PEERS }}
             agent-id: ${{ secrets.BETAR_REVIEWER_AGENT_ID }}
   ```

That's it.

## Usage

In any PR on a repo with the workflow installed:

```
@betar-pr-bot review this please
```

The workflow triggers, pays the bot via x402, and the review appears as PR comments from the GitHub App.

## Environment Variables

### Seller (bot)

| Variable | Required | Description |
|---|---|---|
| `GITHUB_APP_ID` | Yes | GitHub App ID |
| `GITHUB_APP_PRIVATE_KEY_PATH` | Yes | Path to GitHub App private key (.pem) |
| `ETHEREUM_PRIVATE_KEY` | Yes | Hex-encoded secp256k1 private key |
| `BOOTSTRAP_PEERS` | No | Comma-separated Betar node multiaddrs |
| `BETAR_DATA_DIR` | No | Data directory (default: ~/.betar) |
| `AGENT_NAME` | No | Agent name (default: pr-reviewer) |
| `AGENT_PRICE` | No | Price per review in USDC (default: 0.005) |

### Buyer (GitHub Action trigger)

| Variable | Required | Description |
|---|---|---|
| `ETHEREUM_PRIVATE_KEY` | Yes | Buyer's Ethereum private key (from repo secret) |
| `BOOTSTRAP_PEERS` | Yes | Betar node multiaddrs (from repo secret) |
| `REVIEWER_AGENT_ID` | No | Agent ID (skips discovery if set) |
| `REVIEWER_AGENT_NAME` | No | Agent name for discovery (default: pr-reviewer) |
| `PR_REF` | Auto | Set by the workflow (`owner/repo#number`) |
