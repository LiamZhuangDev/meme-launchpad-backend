CREATE TABLE IF NOT EXISTS tokens (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    symbol VARCHAR(50) NOT NULL,
    logo VARCHAR(500) NOT NULL DEFAULT '',
    description TEXT,
    token_contract_address VARCHAR(42) NOT NULL UNIQUE,
    creator_address VARCHAR(42) NOT NULL,
    launch_mode INTEGER NOT NULL DEFAULT 1,
    bnb_current NUMERIC(78, 0) NOT NULL DEFAULT 0,
    bnb_target NUMERIC(78, 0) NOT NULL,
    total_supply NUMERIC(78, 0) NOT NULL,
    status INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS tokens_contract_address_lower_unique
    ON tokens (LOWER(token_contract_address));

CREATE INDEX IF NOT EXISTS tokens_created_at_desc_index
    ON tokens (created_at DESC);
