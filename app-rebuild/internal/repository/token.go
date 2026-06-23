package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type Token struct {
	ID              int64     `json:"id"`
	Name            string    `json:"name"`
	Symbol          string    `json:"symbol"`
	Logo            string    `json:"logo"`
	Description     *string   `json:"description,omitempty"`
	ContractAddress string    `json:"contractAddress"`
	CreatorAddress  string    `json:"creatorAddress"`
	LaunchMode      int       `json:"launchMode"`
	BNBCurrent      string    `json:"bnbCurrent"`
	BNBTarget       string    `json:"bnbTarget"`
	TotalSupply     string    `json:"totalSupply"`
	Status          int       `json:"status"`
	CreatedAt       time.Time `json:"createdAt"`
}

type TokenQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type TokenRepository struct{ db TokenQueryer }

func NewTokenRepository(db TokenQueryer) *TokenRepository { return &TokenRepository{db: db} }

func (r *TokenRepository) List(ctx context.Context, limit, offset int) ([]Token, error) {
	const query = `
		SELECT id, name, symbol, logo, description, token_contract_address,
			creator_address, launch_mode, bnb_current::text, bnb_target::text,
			total_supply::text, status, created_at
		FROM tokens ORDER BY created_at DESC LIMIT $1 OFFSET $2`
	rows, err := r.db.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	defer rows.Close()
	return collectTokens(rows)
}

func (r *TokenRepository) FindByAddress(ctx context.Context, address string) (Token, error) {
	const query = `
		SELECT id, name, symbol, logo, description, token_contract_address,
			creator_address, launch_mode, bnb_current::text, bnb_target::text,
			total_supply::text, status, created_at
		FROM tokens WHERE LOWER(token_contract_address) = LOWER($1)`
	return scanToken(r.db.QueryRow(ctx, query, address))
}

func collectTokens(rows pgx.Rows) ([]Token, error) {
	var tokens []Token
	for rows.Next() {
		token, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tokens, nil
}

func scanToken(row interface{ Scan(...any) error }) (Token, error) {
	var token Token
	err := row.Scan(&token.ID, &token.Name, &token.Symbol, &token.Logo, &token.Description,
		&token.ContractAddress, &token.CreatorAddress, &token.LaunchMode, &token.BNBCurrent,
		&token.BNBTarget, &token.TotalSupply, &token.Status, &token.CreatedAt)
	return token, err
}
