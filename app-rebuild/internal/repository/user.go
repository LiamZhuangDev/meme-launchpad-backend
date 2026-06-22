// Package repository contains persistence operations for application entities.
package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// QueryRower is the narrow database capability the users repository needs.
// pgxpool.Pool satisfies it, and tests can provide a small fake.
type QueryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type User struct {
	ID        int64
	Address   string
	Username  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type UserRepository struct {
	db QueryRower
}

func NewUserRepository(db QueryRower) *UserRepository {
	return &UserRepository{db: db}
}

// FindByAddress returns the user for one normalized wallet address.
func (r *UserRepository) FindByAddress(ctx context.Context, address string) (User, error) {
	const query = `
		SELECT id, address, username, created_at, updated_at
		FROM users
		WHERE LOWER(address) = LOWER($1)
	`

	return scanUser(r.db.QueryRow(ctx, query, address))
}

// Create persists the minimal user profile needed by Step 4 login.
func (r *UserRepository) Create(ctx context.Context, address, username string) (User, error) {
	const query = `
		INSERT INTO users (address, username)
		VALUES ($1, $2)
		RETURNING id, address, username, created_at, updated_at
	`

	return scanUser(r.db.QueryRow(ctx, query, address, username))
}

func scanUser(row pgx.Row) (User, error) {
	var user User
	err := row.Scan(&user.ID, &user.Address, &user.Username, &user.CreatedAt, &user.UpdatedAt)
	return user, err
}
