-- Step 7 keeps an append-only raw event ledger and a durable sync cursor.
-- Step 8 will decode these events into the tokens, trades, and K-line views.
CREATE TABLE IF NOT EXISTS chain_event_checkpoints (
    chain_id BIGINT NOT NULL,
    contract_address VARCHAR(42) NOT NULL,
    last_synced_block BIGINT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (chain_id, contract_address)
);

CREATE TABLE IF NOT EXISTS chain_events (
    chain_id BIGINT NOT NULL,
    contract_address VARCHAR(42) NOT NULL,
    block_number BIGINT NOT NULL,
    block_hash VARCHAR(66) NOT NULL,
    transaction_hash VARCHAR(66) NOT NULL,
    log_index BIGINT NOT NULL,
    topic0 VARCHAR(66) NOT NULL,
    topics JSONB NOT NULL,
    data TEXT NOT NULL,
    removed BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (chain_id, transaction_hash, log_index)
);

CREATE INDEX IF NOT EXISTS chain_events_contract_block_index
    ON chain_events (chain_id, contract_address, block_number);
