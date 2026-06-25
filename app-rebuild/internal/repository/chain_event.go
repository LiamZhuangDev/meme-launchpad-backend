package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/jackc/pgx/v5"
)

// ChainEventStore is the durable boundary between the chain and later
// projections. Saving a range and advancing its checkpoint is atomic.
type ChainEventStore interface {
	LastSyncedBlock(context.Context, int64, string) (uint64, bool, error)
	SaveBlockRange(context.Context, int64, string, uint64, []types.Log, []ChainProjection) error
}

// TransactionBeginner is the only pool capability this repository needs.
type TransactionBeginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

type ChainEventRepository struct{ db TransactionBeginner }

func NewChainEventRepository(db TransactionBeginner) *ChainEventRepository {
	return &ChainEventRepository{db: db}
}

const (
	TradeTypeBuy  = 10
	TradeTypeSell = 20

	TokenStatusTrading   = 1
	TokenStatusGraduated = 3
)

type ChainProjection struct {
	TokenCreated   *TokenCreatedProjection
	TokenBought    *TokenTradeProjection
	TokenSold      *TokenTradeProjection
	TokenGraduated *TokenGraduatedProjection
}

type TokenCreatedProjection struct {
	TokenAddress       string
	CreatorAddress     string
	Name               string
	Symbol             string
	TotalSupply        *big.Int
	RequestID          string
	TransactionHash    string
	BlockNumber        uint64
	BlockTimestamp     time.Time
	LogIndex           uint
	InitialBNBTarget   *big.Int
	InitialBNBCurrent  *big.Int
	InitialAvailable   *big.Int
	InitialLaunchMode  int
	InitialTokenStatus int
}

type TokenTradeProjection struct {
	TokenAddress        string
	UserAddress         string
	TradeType           int
	BNBAmount           *big.Int
	TokenAmount         *big.Int
	TradingFee          *big.Int
	VirtualBNBReserve   *big.Int
	VirtualTokenReserve *big.Int
	AvailableTokens     *big.Int
	CollectedBNB        *big.Int
	TransactionHash     string
	BlockNumber         uint64
	BlockTimestamp      time.Time
	LogIndex            uint
}

type TokenGraduatedProjection struct {
	TokenAddress    string
	LiquidityBNB    *big.Int
	LiquidityTokens *big.Int
	LiquidityResult *big.Int
	TransactionHash string
	BlockNumber     uint64
	BlockTimestamp  time.Time
	LogIndex        uint
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

func (r *ChainEventRepository) SaveBlockRange(ctx context.Context, chainID int64, contract string, lastBlock uint64, logs []types.Log, projections []ChainProjection) error {
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
	for _, projection := range projections {
		if err := applyProjection(ctx, tx, projection); err != nil {
			return err
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

func applyProjection(ctx context.Context, tx pgx.Tx, projection ChainProjection) error {
	switch {
	case projection.TokenCreated != nil:
		return applyTokenCreated(ctx, tx, projection.TokenCreated)
	case projection.TokenBought != nil:
		return applyTokenTrade(ctx, tx, projection.TokenBought)
	case projection.TokenSold != nil:
		return applyTokenTrade(ctx, tx, projection.TokenSold)
	case projection.TokenGraduated != nil:
		return applyTokenGraduated(ctx, tx, projection.TokenGraduated)
	default:
		return nil
	}
}

func applyTokenCreated(ctx context.Context, tx pgx.Tx, event *TokenCreatedProjection) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO token_created_events (
			token_address, creator_address, name, symbol, total_supply, request_id,
			transaction_hash, block_number, block_timestamp, log_index
		) VALUES (LOWER($1), LOWER($2), $3, $4, $5::numeric, $6, $7, $8, $9, $10)
		ON CONFLICT (transaction_hash, log_index) DO NOTHING`,
		event.TokenAddress, event.CreatorAddress, event.Name, event.Symbol, intString(event.TotalSupply), event.RequestID,
		event.TransactionHash, event.BlockNumber, event.BlockTimestamp, event.LogIndex)
	if err != nil {
		return fmt.Errorf("insert token created event: %w", err)
	}
	bnbTarget := intString(event.InitialBNBTarget)
	if bnbTarget == "0" {
		bnbTarget = "24000000000000000000"
	}
	launchMode := event.InitialLaunchMode
	if launchMode == 0 {
		launchMode = 1
	}
	status := event.InitialTokenStatus
	if status == 0 {
		status = TokenStatusTrading
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO tokens (
			name, symbol, token_contract_address, creator_address, launch_mode,
			bnb_current, bnb_target, total_supply, available_tokens, status,
			created_at, updated_at
		) VALUES ($1, $2, LOWER($3), LOWER($4), $5, $6::numeric, $7::numeric, $8::numeric, $9::numeric, $10, $11, $11)
		ON CONFLICT (token_contract_address) DO UPDATE SET
			name = EXCLUDED.name,
			symbol = EXCLUDED.symbol,
			creator_address = EXCLUDED.creator_address,
			total_supply = EXCLUDED.total_supply,
			updated_at = EXCLUDED.updated_at`,
		event.Name, event.Symbol, event.TokenAddress, event.CreatorAddress, launchMode, intString(event.InitialBNBCurrent),
		bnbTarget, intString(event.TotalSupply), intString(event.InitialAvailable), status, event.BlockTimestamp)
	if err != nil {
		return fmt.Errorf("upsert token projection: %w", err)
	}
	return nil
}

func applyTokenTrade(ctx context.Context, tx pgx.Tx, event *TokenTradeProjection) error {
	table := "token_bought_events"
	userColumn := "buyer_address"
	if event.TradeType == TradeTypeSell {
		table = "token_sold_events"
		userColumn = "seller_address"
	}
	_, err := tx.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s (
			token_address, %s, bnb_amount, token_amount, trading_fee,
			virtual_bnb_reserve, virtual_token_reserve, available_tokens, collected_bnb,
			transaction_hash, block_number, block_timestamp, log_index
		) VALUES (LOWER($1), LOWER($2), $3::numeric, $4::numeric, $5::numeric, $6::numeric, $7::numeric, $8::numeric, $9::numeric, $10, $11, $12, $13)
		ON CONFLICT (transaction_hash, log_index) DO NOTHING`, table, userColumn),
		event.TokenAddress, event.UserAddress, intString(event.BNBAmount), intString(event.TokenAmount), intString(event.TradingFee),
		intString(event.VirtualBNBReserve), intString(event.VirtualTokenReserve), intString(event.AvailableTokens), intString(event.CollectedBNB),
		event.TransactionHash, event.BlockNumber, event.BlockTimestamp, event.LogIndex)
	if err != nil {
		return fmt.Errorf("insert token trade event: %w", err)
	}
	commandTag, err := tx.Exec(ctx, `
		INSERT INTO trades (
			token_address, user_address, trade_type, bnb_amount, token_amount, price,
			transaction_hash, log_index, block_number, block_timestamp
		) VALUES (LOWER($1), LOWER($2), $3, $4::numeric, $5::numeric, $6::numeric, $7, $8, $9, $10)
		ON CONFLICT (transaction_hash, log_index, trade_type) DO NOTHING`,
		event.TokenAddress, event.UserAddress, event.TradeType, intString(event.BNBAmount), intString(event.TokenAmount),
		priceString(event.BNBAmount, event.TokenAmount), event.TransactionHash, event.LogIndex, event.BlockNumber, event.BlockTimestamp)
	if err != nil {
		return fmt.Errorf("insert trade projection: %w", err)
	}
	_, err = tx.Exec(ctx, `
		UPDATE tokens SET
			bnb_current = $1::numeric,
			available_tokens = $2::numeric,
			updated_at = $3
		WHERE LOWER(token_contract_address) = LOWER($4)`,
		intString(event.CollectedBNB), intString(event.AvailableTokens), event.BlockTimestamp, event.TokenAddress)
	if err != nil {
		return fmt.Errorf("update token trade totals: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return nil
	}
	if err := upsertKlines(ctx, tx, event); err != nil {
		return fmt.Errorf("upsert klines: %w", err)
	}
	return nil
}

func applyTokenGraduated(ctx context.Context, tx pgx.Tx, event *TokenGraduatedProjection) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO token_graduated_events (
			token_address, liquidity_bnb, liquidity_tokens, liquidity_result,
			transaction_hash, block_number, block_timestamp, log_index
		) VALUES (LOWER($1), $2::numeric, $3::numeric, $4::numeric, $5, $6, $7, $8)
		ON CONFLICT (transaction_hash, log_index) DO NOTHING`,
		event.TokenAddress, intString(event.LiquidityBNB), intString(event.LiquidityTokens), intString(event.LiquidityResult),
		event.TransactionHash, event.BlockNumber, event.BlockTimestamp, event.LogIndex)
	if err != nil {
		return fmt.Errorf("insert token graduated event: %w", err)
	}
	_, err = tx.Exec(ctx, `
		UPDATE tokens SET status = $1, updated_at = $2 WHERE LOWER(token_contract_address) = LOWER($3)`,
		TokenStatusGraduated, event.BlockTimestamp, event.TokenAddress)
	if err != nil {
		return fmt.Errorf("mark token graduated: %w", err)
	}
	return nil
}

func upsertKlines(ctx context.Context, tx pgx.Tx, event *TokenTradeProjection) error {
	price := priceString(event.BNBAmount, event.TokenAmount)
	volume := intString(event.TokenAmount)
	for _, interval := range []string{"1m", "5m", "15m", "30m", "1h", "4h", "1d", "1w"} {
		openTime := alignTime(event.BlockTimestamp, interval)
		_, err := tx.Exec(ctx, `
			INSERT INTO klines (
				token_address, interval, open_time, open_price, high_price, low_price, close_price, volume
			) VALUES (LOWER($1), $2, $3, $4::numeric, $4::numeric, $4::numeric, $4::numeric, $5::numeric)
			ON CONFLICT (token_address, interval, open_time) DO UPDATE SET
				high_price = GREATEST(klines.high_price, EXCLUDED.high_price),
				low_price = LEAST(klines.low_price, EXCLUDED.low_price),
				close_price = EXCLUDED.close_price,
				volume = klines.volume + EXCLUDED.volume,
				updated_at = NOW()`,
			event.TokenAddress, interval, openTime, price, volume)
		if err != nil {
			return err
		}
	}
	return nil
}

func alignTime(value time.Time, interval string) time.Time {
	value = value.UTC()
	switch interval {
	case "1m":
		return value.Truncate(time.Minute)
	case "5m":
		return value.Truncate(5 * time.Minute)
	case "15m":
		return value.Truncate(15 * time.Minute)
	case "30m":
		return value.Truncate(30 * time.Minute)
	case "1h":
		return value.Truncate(time.Hour)
	case "4h":
		return value.Truncate(4 * time.Hour)
	case "1d":
		return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
	case "1w":
		dayStart := time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
		weekday := int(dayStart.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		return dayStart.AddDate(0, 0, -(weekday - 1))
	default:
		return value
	}
}

func intString(value *big.Int) string {
	if value == nil {
		return "0"
	}
	return value.String()
}

func priceString(bnbAmount, tokenAmount *big.Int) string {
	if bnbAmount == nil || tokenAmount == nil || tokenAmount.Sign() == 0 {
		return "0"
	}
	return new(big.Rat).SetFrac(bnbAmount, tokenAmount).FloatString(18)
}
