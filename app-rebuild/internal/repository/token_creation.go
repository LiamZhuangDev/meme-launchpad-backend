package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgconn"
)

type TokenCreationRequest struct {
	RequestID, CreatorAddress, Name, Symbol, Data, Signature, Salt, PredictedAddress string
	Nonce, Timestamp                                                                 uint64
}
type TokenCreationStore interface {
	Create(context.Context, TokenCreationRequest) error
}

// Execer is the narrow database capability needed to persist a creation intent.
// pgxpool.Pool satisfies it, while tests can provide a small fake.
type Execer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type TokenCreationRepository struct {
	db Execer
}

func NewTokenCreationRepository(db Execer) *TokenCreationRepository {
	return &TokenCreationRepository{db: db}
}

func (r *TokenCreationRepository) Create(ctx context.Context, v TokenCreationRequest) error {
	_, err := r.db.Exec(ctx, `INSERT INTO token_creation_requests (request_id,creator_address,name,symbol,encoded_data,signature,create2_salt,predicted_address,nonce,request_timestamp) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, v.RequestID, v.CreatorAddress, v.Name, v.Symbol, v.Data, v.Signature, v.Salt, v.PredictedAddress, v.Nonce, v.Timestamp)
	return err
}
