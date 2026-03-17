// Package main is the buyer-side trigger binary.
// It joins the Betar network, discovers the PR reviewer agent,
// executes it with x402 payment, and exits.
//
// Designed to run inside a GitHub Action.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/asabya/betar/pkg/sdk"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	prRef := os.Getenv("PR_REF") // e.g. "owner/repo#42"
	if prRef == "" {
		log.Fatal("PR_REF is required")
	}

	agentID := os.Getenv("REVIEWER_AGENT_ID")
	agentQuery := os.Getenv("REVIEWER_AGENT_NAME")
	if agentID == "" && agentQuery == "" {
		agentQuery = "pr-reviewer"
	}

	var bootstrapPeers []string
	if bp := os.Getenv("BOOTSTRAP_PEERS"); bp != "" {
		bootstrapPeers = strings.Split(bp, ",")
	}

	// Join the Betar network as a lightweight buyer node
	client, err := sdk.NewClient(sdk.Config{
		DataDir:        os.Getenv("BETAR_DATA_DIR"),
		EthereumKey:    os.Getenv("ETHEREUM_PRIVATE_KEY"),
		BootstrapPeers: bootstrapPeers,
	})
	if err != nil {
		log.Fatalf("Failed to create Betar client: %v", err)
	}
	defer client.Close()

	log.Printf("Buyer node started: peer=%s wallet=%s", client.PeerID(), client.WalletAddress())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// If no agent ID given, discover it by name
	if agentID == "" {
		log.Printf("Discovering agent %q...", agentQuery)

		// Retry discovery a few times to allow CRDT sync
		var found []sdk.AgentListing
		for i := 0; i < 5; i++ {
			found, err = client.Discover(ctx, agentQuery)
			if err == nil && len(found) > 0 {
				break
			}
			time.Sleep(3 * time.Second)
		}
		if err != nil {
			log.Fatalf("Discovery failed: %v", err)
		}
		if len(found) == 0 {
			log.Fatalf("No agent found matching %q", agentQuery)
		}

		agentID = found[0].ID
		log.Printf("Found agent: id=%s name=%s price=%.4f", found[0].ID, found[0].Name, found[0].Price)
	} else {
		<-time.After(30 * time.Second)
	}

	// Execute the agent — SDK handles x402 payment automatically
	log.Printf("Executing agent %s with task: %s", agentID, prRef)
	result, err := client.Execute(ctx, agentID, prRef)
	if err != nil {
		log.Fatalf("Execution failed: %v", err)
	}

	fmt.Println(result)
}
