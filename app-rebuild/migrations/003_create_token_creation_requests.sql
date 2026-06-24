CREATE TABLE IF NOT EXISTS token_creation_requests (
    request_id VARCHAR(66) PRIMARY KEY,
    creator_address VARCHAR(42) NOT NULL,
    name VARCHAR(100) NOT NULL,
    symbol VARCHAR(50) NOT NULL,
    encoded_data TEXT NOT NULL,
    signature VARCHAR(132) NOT NULL,
    create2_salt VARCHAR(66) NOT NULL,
    predicted_address VARCHAR(42) NOT NULL,
    nonce NUMERIC(20,0) NOT NULL,
    request_timestamp BIGINT NOT NULL,
    status SMALLINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
