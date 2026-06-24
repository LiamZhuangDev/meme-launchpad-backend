// Command indexer runs independently from cmd/api and only consumes chain logs.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/meme-launchpad/app-rebuild/internal/chainindexer"
	"github.com/meme-launchpad/app-rebuild/internal/config"
	"github.com/meme-launchpad/app-rebuild/internal/database"
	"github.com/meme-launchpad/app-rebuild/internal/repository"
)

func main() {
	cfg, err := config.Load(os.LookupEnv)
	if err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}
	if err := cfg.Indexer.Validate(); err != nil {
		log.Fatalf("invalid indexer configuration: %v", err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	startup, stopStartup := context.WithTimeout(ctx, 10*time.Second)
	defer stopStartup()
	pool, err := database.Open(startup, cfg.Database)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer pool.Close()
	client, err := ethclient.DialContext(startup, cfg.Indexer.RPCURL)
	if err != nil {
		log.Fatalf("connect RPC: %v", err)
	}
	defer client.Close()
	indexer, err := chainindexer.New(client, repository.NewChainEventRepository(pool), cfg.Indexer)
	if err != nil {
		log.Fatalf("create indexer: %v", err)
	}
	log.Printf("indexing MEMECore %s on chain %d", cfg.Indexer.CoreContract, cfg.Indexer.ChainID)
	if err := indexer.Run(ctx); err != nil {
		log.Fatalf("indexer stopped: %v", err)
	}
}
