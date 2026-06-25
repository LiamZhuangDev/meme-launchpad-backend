-- Step 8 turns raw MEMECore logs into query-friendly read models.
ALTER TABLE tokens ADD COLUMN IF NOT EXISTS available_tokens NUMERIC(78, 0) NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS token_created_events (
    id BIGSERIAL PRIMARY KEY,
    token_address VARCHAR(42) NOT NULL,
    creator_address VARCHAR(42) NOT NULL,
    name VARCHAR(255) NOT NULL,
    symbol VARCHAR(50) NOT NULL,
    total_supply NUMERIC(78, 0) NOT NULL,
    request_id VARCHAR(66) NOT NULL,
    transaction_hash VARCHAR(66) NOT NULL,
    block_number BIGINT NOT NULL,
    block_timestamp TIMESTAMPTZ NOT NULL,
    log_index BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (transaction_hash, log_index)
);

CREATE TABLE IF NOT EXISTS token_bought_events (
    id BIGSERIAL PRIMARY KEY,
    token_address VARCHAR(42) NOT NULL,
    buyer_address VARCHAR(42) NOT NULL,
    bnb_amount NUMERIC(78, 0) NOT NULL,
    token_amount NUMERIC(78, 0) NOT NULL,
    trading_fee NUMERIC(78, 0) NOT NULL,
    virtual_bnb_reserve NUMERIC(78, 0) NOT NULL,
    virtual_token_reserve NUMERIC(78, 0) NOT NULL,
    available_tokens NUMERIC(78, 0) NOT NULL,
    collected_bnb NUMERIC(78, 0) NOT NULL,
    transaction_hash VARCHAR(66) NOT NULL,
    block_number BIGINT NOT NULL,
    block_timestamp TIMESTAMPTZ NOT NULL,
    log_index BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (transaction_hash, log_index)
);

CREATE TABLE IF NOT EXISTS token_sold_events (
    id BIGSERIAL PRIMARY KEY,
    token_address VARCHAR(42) NOT NULL,
    seller_address VARCHAR(42) NOT NULL,
    token_amount NUMERIC(78, 0) NOT NULL,
    bnb_amount NUMERIC(78, 0) NOT NULL,
    trading_fee NUMERIC(78, 0) NOT NULL,
    virtual_bnb_reserve NUMERIC(78, 0) NOT NULL,
    virtual_token_reserve NUMERIC(78, 0) NOT NULL,
    available_tokens NUMERIC(78, 0) NOT NULL,
    collected_bnb NUMERIC(78, 0) NOT NULL,
    transaction_hash VARCHAR(66) NOT NULL,
    block_number BIGINT NOT NULL,
    block_timestamp TIMESTAMPTZ NOT NULL,
    log_index BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (transaction_hash, log_index)
);

CREATE TABLE IF NOT EXISTS token_graduated_events (
    id BIGSERIAL PRIMARY KEY,
    token_address VARCHAR(42) NOT NULL,
    liquidity_bnb NUMERIC(78, 0) NOT NULL,
    liquidity_tokens NUMERIC(78, 0) NOT NULL,
    liquidity_result NUMERIC(78, 0) NOT NULL,
    transaction_hash VARCHAR(66) NOT NULL,
    block_number BIGINT NOT NULL,
    block_timestamp TIMESTAMPTZ NOT NULL,
    log_index BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (transaction_hash, log_index)
);

CREATE TABLE IF NOT EXISTS trades (
    id BIGSERIAL PRIMARY KEY,
    token_address VARCHAR(42) NOT NULL,
    user_address VARCHAR(42) NOT NULL,
    trade_type INTEGER NOT NULL, -- 10=buy, 20=sell
    bnb_amount NUMERIC(78, 0) NOT NULL,
    token_amount NUMERIC(78, 0) NOT NULL,
    price NUMERIC(78, 18) NOT NULL,
    transaction_hash VARCHAR(66) NOT NULL,
    log_index BIGINT NOT NULL,
    block_number BIGINT NOT NULL,
    block_timestamp TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (transaction_hash, log_index, trade_type)
);

ALTER TABLE trades ADD COLUMN IF NOT EXISTS log_index BIGINT NOT NULL DEFAULT 0;
CREATE UNIQUE INDEX IF NOT EXISTS trades_tx_log_type_unique
    ON trades (transaction_hash, log_index, trade_type);
CREATE INDEX IF NOT EXISTS trades_token_index ON trades (LOWER(token_address), block_timestamp DESC);
CREATE INDEX IF NOT EXISTS trades_user_index ON trades (LOWER(user_address), block_timestamp DESC);
CREATE INDEX IF NOT EXISTS trades_type_index ON trades (trade_type);

CREATE TABLE IF NOT EXISTS klines (
    id BIGSERIAL PRIMARY KEY,
    token_address VARCHAR(42) NOT NULL,
    interval VARCHAR(10) NOT NULL,
    open_time TIMESTAMPTZ NOT NULL,
    open_price NUMERIC(78, 18) NOT NULL,
    high_price NUMERIC(78, 18) NOT NULL,
    low_price NUMERIC(78, 18) NOT NULL,
    close_price NUMERIC(78, 18) NOT NULL,
    volume NUMERIC(78, 0) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (token_address, interval, open_time)
);

CREATE INDEX IF NOT EXISTS klines_token_interval_index
    ON klines (LOWER(token_address), interval, open_time DESC);
