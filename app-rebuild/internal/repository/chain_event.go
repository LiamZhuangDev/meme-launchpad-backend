package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/jackc/pgx/v5"
)

// ChainEventStore is the durable boundary between the chain and later
// projections. Saving a range and advancing its checkpoint is atomic.
type ChainEventStore interface {
	LastSyncedBlock(context.Context, int64, string) (uint64, bool, error)
	SaveBlockRange(context.Context, int64, string, uint64, []types.Log) error
}

// TransactionBeginner is the only pool capability this repository needs.
type TransactionBeginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

type ChainEventRepository struct{ db TransactionBeginner }

func NewChainEventRepository(db TransactionBeginner) *ChainEventRepository {
	return &ChainEventRepository{db: db}
}

func (r *ChainEventRepository) LastSyncedBlock(ctx context.Context, chainID int64, contract string) (uint64, bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, false, fmt.Errorf("begin checkpoint query: %w", err)
	}
	defer tx.Rollback(ctx)
	var block uint64
	err = tx.QueryRow(ctx, `SELECT last_synced_block FROM chain_event_checkpoints WHERE chain_id = $1 AND LOWER(contract_address) = LOWER($2)`, chainID, contract).Scan(&block)
	if err == pgx.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read checkpoint: %w", err)
	}
	return block, true, nil
}

func (r *ChainEventRepository) SaveBlockRange(ctx context.Context, chainID int64, contract string, lastBlock uint64, logs []types.Log) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin event save: %w", err)
	}
	defer tx.Rollback(ctx)
	for _, log := range logs {
		topics := make([]string, len(log.Topics))
		for i, topic := range log.Topics {
			topics[i] = topic.Hex()
		}
		topicsJSON, err := json.Marshal(topics)
		if err != nil {
			return fmt.Errorf("encode log topics: %w", err)
		}
		topic0 := ""
		if len(topics) > 0 {
			topic0 = topics[0]
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO chain_events (chain_id, contract_address, block_number, block_hash, transaction_hash, log_index, topic0, topics, data, removed)
			VALUES ($1, LOWER($2), $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (chain_id, transaction_hash, log_index) DO NOTHING`,
			chainID, contract, log.BlockNumber, log.BlockHash.Hex(), log.TxHash.Hex(), log.Index, topic0, topicsJSON, "0x"+fmt.Sprintf("%x", log.Data), log.Removed)
		if err != nil {
			return fmt.Errorf("insert chain event: %w", err)
		}
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO chain_event_checkpoints (chain_id, contract_address, last_synced_block)
		VALUES ($1, LOWER($2), $3)
		ON CONFLICT (chain_id, contract_address) DO UPDATE SET last_synced_block = EXCLUDED.last_synced_block, updated_at = NOW()`, chainID, contract, lastBlock)
	if err != nil {
		return fmt.Errorf("update checkpoint: %w", err)
	}
	return tx.Commit(ctx)
}
