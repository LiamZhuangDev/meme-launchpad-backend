// Package app wires configuration and runtime dependencies together.
package app

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/meme-launchpad/app-rebuild/internal/auth"
	"github.com/meme-launchpad/app-rebuild/internal/config"
	"github.com/meme-launchpad/app-rebuild/internal/database"
	"github.com/meme-launchpad/app-rebuild/internal/httpapi"
	"github.com/meme-launchpad/app-rebuild/internal/repository"
)

// Application is the dependency container for one API process.
type Application struct {
	Config config.Config
	DB     *pgxpool.Pool
	Users  *repository.UserRepository
	Auth   *auth.Service
}

// New opens the process-wide PostgreSQL pool and wires repositories to it.
func New(ctx context.Context, cfg config.Config) (*Application, error) {
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		return nil, err
	}
	return NewWithPool(cfg, pool), nil
}

// NewWithPool is useful for tests and future commands that own an existing pool.
func NewWithPool(cfg config.Config, pool *pgxpool.Pool) *Application {
	application := &Application{Config: cfg, DB: pool}
	if pool != nil {
		application.Users = repository.NewUserRepository(pool)
		application.Auth = auth.New(application.Users, cfg.Auth.JWTSecret)
	}
	return application
}

func (a *Application) Close() {
	if a.DB != nil {
		a.DB.Close()
	}
}

func (a *Application) HTTPServer() *http.Server {
	return &http.Server{
		Addr:              fmt.Sprintf(":%d", a.Config.HTTP.Port),
		Handler:           httpapi.NewHandler(a.Config.ServiceName, a.Auth),
		ReadHeaderTimeout: 5 * time.Second,
	}
}
