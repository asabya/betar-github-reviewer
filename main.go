package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/asabya/betar/pkg/sdk"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// GitHub App config
	appID, err := strconv.ParseInt(os.Getenv("GITHUB_APP_ID"), 10, 64)
	if err != nil {
		log.Fatal("GITHUB_APP_ID is required (integer)")
	}
	pemPath := os.Getenv("GITHUB_APP_PRIVATE_KEY_PATH")
	if pemPath == "" {
		log.Fatal("GITHUB_APP_PRIVATE_KEY_PATH is required")
	}

	// Agent config
	agentName := os.Getenv("AGENT_NAME")
	if agentName == "" {
		agentName = "pr-reviewer"
	}
	price := 0.005
	if p := os.Getenv("AGENT_PRICE"); p != "" {
		if v, err := strconv.ParseFloat(p, 64); err == nil {
			price = v
		}
	}

	// Bootstrap peers
	var bootstrapPeers []string
	if bp := os.Getenv("BOOTSTRAP_PEERS"); bp != "" {
		bootstrapPeers = strings.Split(bp, ",")
	}

	// Create Betar SDK client
	client, err := sdk.NewClient(sdk.Config{
		DataDir:        os.Getenv("BETAR_DATA_DIR"),
		EthereumKey:    os.Getenv("ETHEREUM_PRIVATE_KEY"),
		BootstrapPeers: bootstrapPeers,
	})
	if err != nil {
		log.Fatalf("Failed to create Betar client: %v", err)
	}
	defer client.Close()

	log.Printf("Betar node started: peer=%s wallet=%s", client.PeerID(), client.WalletAddress())
	for _, addr := range client.Addrs() {
		log.Printf("  listening: %s", addr)
	}

	// Create GitHub client
	gh, err := NewGitHubClient(appID, pemPath)
	if err != nil {
		log.Fatalf("Failed to create GitHub client: %v", err)
	}

	// Create Claude runner
	claude, err := NewClaudeRunner()
	if err != nil {
		log.Fatalf("Failed to find Claude CLI: %v", err)
	}

	// Create handler
	handler := &ReviewHandler{github: gh, claude: claude}

	// Register agent on marketplace
	ctx := context.Background()
	agent, err := client.Register(ctx, sdk.AgentSpec{
		Name:          agentName,
		Description:   "Reviews GitHub PRs for code quality, bugs, and security issues using Claude AI",
		Price:         price,
		X402Support:   true,
		CustomHandler: true,
	})
	if err != nil {
		log.Fatalf("Failed to register agent: %v", err)
	}
	log.Printf("Agent registered: id=%s name=%s price=%.4f USDC", agent.AgentID, agent.Name, agent.Price)

	// Register custom task handler
	client.Serve(handler.Handle)

	fmt.Println("\nPR Reviewer bot is running. Send a PR URL (e.g., owner/repo#123) as a task.")
	fmt.Println("Press Ctrl+C to stop.")

	// Block until signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down...")
}
