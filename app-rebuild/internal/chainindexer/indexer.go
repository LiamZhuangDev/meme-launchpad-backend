// Package chainindexer consumes MEMECore logs into the raw chain_events ledger.
package chainindexer

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/meme-launchpad/app-rebuild/internal/config"
	"github.com/meme-launchpad/app-rebuild/internal/repository"
)

type Client interface {
	ChainID(context.Context) (*big.Int, error)
	BlockNumber(context.Context) (uint64, error)
	FilterLogs(context.Context, ethereum.FilterQuery) ([]types.Log, error)
}

type Indexer struct {
	client   Client
	store    repository.ChainEventStore
	cfg      config.IndexerConfig
	contract common.Address
}

func New(client Client, store repository.ChainEventStore, cfg config.IndexerConfig) (*Indexer, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if client == nil || store == nil {
		return nil, fmt.Errorf("chain client and event store are required")
	}
	return &Indexer{client: client, store: store, cfg: cfg, contract: common.HexToAddress(cfg.CoreContract)}, nil
}

func (i *Indexer) VerifyChain(ctx context.Context) error {
	chainID, err := i.client.ChainID(ctx)
	if err != nil {
		return fmt.Errorf("read RPC chain ID: %w", err)
	}
	if chainID.Int64() != i.cfg.ChainID {
		return fmt.Errorf("RPC chain ID is %d, want %d", chainID.Int64(), i.cfg.ChainID)
	}
	return nil
}

// Run continuously catches up then polls. The API never calls this code.
func (i *Indexer) Run(ctx context.Context) error {
	if err := i.VerifyChain(ctx); err != nil {
		return err
	}
	next, err := i.nextBlock(ctx)
	if err != nil {
		return err
	}
	ticker := time.NewTicker(time.Duration(i.cfg.PollInterval) * time.Second)
	defer ticker.Stop()
	for {
		if err := i.syncAvailable(ctx, &next); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (i *Indexer) nextBlock(ctx context.Context) (uint64, error) {
	last, found, err := i.store.LastSyncedBlock(ctx, i.cfg.ChainID, i.contract.Hex())
	if err != nil {
		return 0, err
	}
	if found {
		return last + 1, nil
	}
	return i.cfg.StartBlock, nil
}

func (i *Indexer) syncAvailable(ctx context.Context, next *uint64) error {
	latest, err := i.client.BlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("read latest block: %w", err)
	}
	for *next <= latest {
		end := *next + i.cfg.BlockBatchSize - 1
		if end > latest {
			end = latest
		}
		logs, err := i.client.FilterLogs(ctx, ethereum.FilterQuery{FromBlock: new(big.Int).SetUint64(*next), ToBlock: new(big.Int).SetUint64(end), Addresses: []common.Address{i.contract}})
		if err != nil {
			return fmt.Errorf("filter logs %d-%d: %w", *next, end, err)
		}
		if err := i.store.SaveBlockRange(ctx, i.cfg.ChainID, i.contract.Hex(), end, logs); err != nil {
			return fmt.Errorf("save logs %d-%d: %w", *next, end, err)
		}
		*next = end + 1
	}
	return nil
}
