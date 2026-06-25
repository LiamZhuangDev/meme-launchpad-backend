// Package chainindexer consumes MEMECore logs into the raw chain_events ledger.
package chainindexer

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/meme-launchpad/app-rebuild/internal/config"
	"github.com/meme-launchpad/app-rebuild/internal/repository"
)

type Client interface {
	ChainID(context.Context) (*big.Int, error)
	BlockNumber(context.Context) (uint64, error)
	HeaderByNumber(context.Context, *big.Int) (*types.Header, error)
	FilterLogs(context.Context, ethereum.FilterQuery) ([]types.Log, error)
}

type Indexer struct {
	client      Client
	store       repository.ChainEventStore
	cfg         config.IndexerConfig
	contract    common.Address
	contractABI abi.ABI
}

func New(client Client, store repository.ChainEventStore, cfg config.IndexerConfig) (*Indexer, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if client == nil || store == nil {
		return nil, fmt.Errorf("chain client and event store are required")
	}
	contractABI, err := abi.JSON(strings.NewReader(memeCoreABI))
	if err != nil {
		return nil, fmt.Errorf("parse MEMECore ABI: %w", err)
	}
	return &Indexer{client: client, store: store, cfg: cfg, contract: common.HexToAddress(cfg.CoreContract), contractABI: contractABI}, nil
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
		projections, err := i.decodeProjections(ctx, logs)
		if err != nil {
			return fmt.Errorf("decode logs %d-%d: %w", *next, end, err)
		}
		if err := i.store.SaveBlockRange(ctx, i.cfg.ChainID, i.contract.Hex(), end, logs, projections); err != nil {
			return fmt.Errorf("save logs %d-%d: %w", *next, end, err)
		}
		*next = end + 1
	}
	return nil
}

func (i *Indexer) decodeProjections(ctx context.Context, logs []types.Log) ([]repository.ChainProjection, error) {
	blockTimes := make(map[uint64]time.Time)
	projections := make([]repository.ChainProjection, 0, len(logs))
	for _, log := range logs {
		if len(log.Topics) == 0 || log.Removed {
			continue
		}
		blockTime, ok := blockTimes[log.BlockNumber]
		if !ok {
			header, err := i.client.HeaderByNumber(ctx, new(big.Int).SetUint64(log.BlockNumber))
			if err != nil {
				return nil, fmt.Errorf("read block %d header: %w", log.BlockNumber, err)
			}
			blockTime = time.Unix(int64(header.Time), 0).UTC()
			blockTimes[log.BlockNumber] = blockTime
		}
		projection, ok, err := i.decodeProjection(log, blockTime)
		if err != nil {
			return nil, err
		}
		if ok {
			projections = append(projections, projection)
		}
	}
	return projections, nil
}

func (i *Indexer) decodeProjection(log types.Log, blockTime time.Time) (repository.ChainProjection, bool, error) {
	switch log.Topics[0] {
	case i.contractABI.Events["TokenCreated"].ID:
		values, err := i.contractABI.Events["TokenCreated"].Inputs.NonIndexed().Unpack(log.Data)
		if err != nil {
			return repository.ChainProjection{}, false, fmt.Errorf("unpack TokenCreated: %w", err)
		}
		if len(log.Topics) < 3 {
			return repository.ChainProjection{}, false, fmt.Errorf("TokenCreated missing indexed topics")
		}
		requestID := values[3].([32]byte)
		return repository.ChainProjection{TokenCreated: &repository.TokenCreatedProjection{
			TokenAddress:    topicAddress(log.Topics[1]).Hex(),
			CreatorAddress:  topicAddress(log.Topics[2]).Hex(),
			Name:            values[0].(string),
			Symbol:          values[1].(string),
			TotalSupply:     values[2].(*big.Int),
			RequestID:       common.BytesToHash(requestID[:]).Hex(),
			TransactionHash: log.TxHash.Hex(),
			BlockNumber:     log.BlockNumber,
			BlockTimestamp:  blockTime,
			LogIndex:        log.Index,
		}}, true, nil
	case i.contractABI.Events["TokenBought"].ID:
		values, err := i.contractABI.Events["TokenBought"].Inputs.NonIndexed().Unpack(log.Data)
		if err != nil {
			return repository.ChainProjection{}, false, fmt.Errorf("unpack TokenBought: %w", err)
		}
		if len(log.Topics) < 3 {
			return repository.ChainProjection{}, false, fmt.Errorf("TokenBought missing indexed topics")
		}
		return repository.ChainProjection{TokenBought: &repository.TokenTradeProjection{
			TokenAddress:        topicAddress(log.Topics[1]).Hex(),
			UserAddress:         topicAddress(log.Topics[2]).Hex(),
			TradeType:           repository.TradeTypeBuy,
			BNBAmount:           values[0].(*big.Int),
			TokenAmount:         values[1].(*big.Int),
			TradingFee:          values[2].(*big.Int),
			VirtualBNBReserve:   values[3].(*big.Int),
			VirtualTokenReserve: values[4].(*big.Int),
			AvailableTokens:     values[5].(*big.Int),
			CollectedBNB:        values[6].(*big.Int),
			TransactionHash:     log.TxHash.Hex(),
			BlockNumber:         log.BlockNumber,
			BlockTimestamp:      blockTime,
			LogIndex:            log.Index,
		}}, true, nil
	case i.contractABI.Events["TokenSold"].ID:
		values, err := i.contractABI.Events["TokenSold"].Inputs.NonIndexed().Unpack(log.Data)
		if err != nil {
			return repository.ChainProjection{}, false, fmt.Errorf("unpack TokenSold: %w", err)
		}
		if len(log.Topics) < 3 {
			return repository.ChainProjection{}, false, fmt.Errorf("TokenSold missing indexed topics")
		}
		return repository.ChainProjection{TokenSold: &repository.TokenTradeProjection{
			TokenAddress:        topicAddress(log.Topics[1]).Hex(),
			UserAddress:         topicAddress(log.Topics[2]).Hex(),
			TradeType:           repository.TradeTypeSell,
			BNBAmount:           values[1].(*big.Int),
			TokenAmount:         values[0].(*big.Int),
			TradingFee:          values[2].(*big.Int),
			VirtualBNBReserve:   values[3].(*big.Int),
			VirtualTokenReserve: values[4].(*big.Int),
			AvailableTokens:     values[5].(*big.Int),
			CollectedBNB:        values[6].(*big.Int),
			TransactionHash:     log.TxHash.Hex(),
			BlockNumber:         log.BlockNumber,
			BlockTimestamp:      blockTime,
			LogIndex:            log.Index,
		}}, true, nil
	case i.contractABI.Events["TokenGraduated"].ID:
		values, err := i.contractABI.Events["TokenGraduated"].Inputs.NonIndexed().Unpack(log.Data)
		if err != nil {
			return repository.ChainProjection{}, false, fmt.Errorf("unpack TokenGraduated: %w", err)
		}
		if len(log.Topics) < 2 {
			return repository.ChainProjection{}, false, fmt.Errorf("TokenGraduated missing indexed topics")
		}
		return repository.ChainProjection{TokenGraduated: &repository.TokenGraduatedProjection{
			TokenAddress:    topicAddress(log.Topics[1]).Hex(),
			LiquidityBNB:    values[0].(*big.Int),
			LiquidityTokens: values[1].(*big.Int),
			LiquidityResult: values[2].(*big.Int),
			TransactionHash: log.TxHash.Hex(),
			BlockNumber:     log.BlockNumber,
			BlockTimestamp:  blockTime,
			LogIndex:        log.Index,
		}}, true, nil
	default:
		return repository.ChainProjection{}, false, nil
	}
}

func topicAddress(topic common.Hash) common.Address {
	return common.BytesToAddress(topic.Bytes()[12:])
}
