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
	"github.com/meme-launchpad/app-rebuild/internal/grpcclient"
	"github.com/meme-launchpad/app-rebuild/internal/grpcsecurity"
	"github.com/meme-launchpad/app-rebuild/internal/httpapi"
	"github.com/meme-launchpad/app-rebuild/internal/repository"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
)

// Application is the dependency container for one API process.
type Application struct {
	Config               config.Config
	DB                   *pgxpool.Pool
	Redis                *redis.Client
	Users                *repository.UserRepository
	Auth                 *auth.Service
	TokenReader          httpapi.TokenReader
	TokenCreator         httpapi.TokenCreator
	Uploads              httpapi.Presigner
	tokenService         *grpc.ClientConn
	tokenCreationService *grpc.ClientConn
	uploadService        *grpc.ClientConn
}

// New opens the process-wide PostgreSQL pool and wires repositories to it.
func New(ctx context.Context, cfg config.Config) (*Application, error) {
	pool, err := database.Open(ctx, cfg.Database)
	if err != nil {
		return nil, err
	}
	application := NewWithPool(cfg, pool)
	privateKey, err := auth.LoadJWTPrivateKey(cfg.Auth.JWTPrivateKeyFile)
	if err != nil {
		application.Close()
		return nil, fmt.Errorf("configure JWT issuer: %w", err)
	}
	challengeStore := auth.ChallengeStore(auth.NewMemoryChallengeStore())
	if application.Redis != nil {
		challengeStore = auth.NewRedisChallengeStore(application.Redis, "")
	}
	application.Auth = auth.NewWithChallengeStore(application.Users, privateKey, auth.SIWEConfig(cfg.Auth.SIWE), challengeStore)
	connection, reader, err := connectTokenService(ctx, cfg.TokenService)
	if err != nil {
		application.Close()
		return nil, err
	}
	application.tokenService = connection
	application.TokenReader = reader
	creationConnection, creator, err := connectTokenCreationService(ctx, cfg.TokenCreationService)
	if err != nil {
		application.Close()
		return nil, err
	}
	application.tokenCreationService = creationConnection
	application.TokenCreator = creator
	uploadConnection, uploads, err := connectUploadService(ctx, cfg.UploadService)
	if err != nil {
		application.Close()
		return nil, err
	}
	application.uploadService = uploadConnection
	application.Uploads = uploads
	return application, nil
}

// NewWithPool is useful for tests and future commands that own an existing pool.
func NewWithPool(cfg config.Config, pool *pgxpool.Pool) *Application {
	application := &Application{Config: cfg, DB: pool}
	if cfg.Redis.Addr != "" {
		application.Redis = redis.NewClient(&redis.Options{Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB})
	}
	if pool != nil {
		application.Users = repository.NewUserRepository(pool)
	}
	return application
}

func (a *Application) Close() {
	if a.tokenService != nil {
		_ = a.tokenService.Close()
	}
	if a.tokenCreationService != nil {
		_ = a.tokenCreationService.Close()
	}
	if a.uploadService != nil {
		_ = a.uploadService.Close()
	}
	if a.Redis != nil {
		_ = a.Redis.Close()
	}
	if a.DB != nil {
		a.DB.Close()
	}
}

func (a *Application) HTTPServer() *http.Server {
	return &http.Server{
		Addr:              a.Config.HTTP.Address(),
		Handler:           httpapi.NewHandler(a.Config.ServiceName, a.Auth, a.TokenReader, a.TokenCreator, a.Uploads),
		ReadHeaderTimeout: 5 * time.Second,
	}
}

func connectUploadService(ctx context.Context, cfg config.UploadServiceConfig) (*grpc.ClientConn, *grpcclient.UploadService, error) {
	target := cfg.Address()
	if !cfg.TLS.Enabled() && !grpcsecurity.IsLoopbackTarget(target) {
		return nil, nil, fmt.Errorf("upload-service mutual TLS is required when %s is not loopback", target)
	}
	credentials, err := grpcsecurity.ClientCredentials(grpcsecurity.ClientTLSConfig{
		CAFile: cfg.TLS.CAFile, CertFile: cfg.TLS.CertFile, KeyFile: cfg.TLS.KeyFile, ServerName: cfg.TLS.ServerName,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("configure upload-service gRPC client: %w", err)
	}
	connection, err := grpc.DialContext(ctx, target, grpc.WithTransportCredentials(credentials), grpc.WithBlock())
	if err != nil {
		return nil, nil, fmt.Errorf("connect to upload service at %s: %w", target, err)
	}
	if err := grpcclient.New(connection).CheckHealth(ctx, "meme-upload-service"); err != nil {
		_ = connection.Close()
		return nil, nil, fmt.Errorf("verify upload service at %s: %w", target, err)
	}
	return connection, grpcclient.NewUploadService(connection), nil
}

func connectTokenCreationService(ctx context.Context, cfg config.TokenCreationServiceConfig) (*grpc.ClientConn, *grpcclient.TokenCreator, error) {
	target := cfg.Address()
	if !cfg.TLS.Enabled() && !grpcsecurity.IsLoopbackTarget(target) {
		return nil, nil, fmt.Errorf("token-creation-service mutual TLS is required when %s is not loopback", target)
	}
	credentials, err := grpcsecurity.ClientCredentials(grpcsecurity.ClientTLSConfig{
		CAFile: cfg.TLS.CAFile, CertFile: cfg.TLS.CertFile, KeyFile: cfg.TLS.KeyFile, ServerName: cfg.TLS.ServerName,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("configure token-creation-service gRPC client: %w", err)
	}
	connection, err := grpc.DialContext(ctx, target, grpc.WithTransportCredentials(credentials), grpc.WithBlock())
	if err != nil {
		return nil, nil, fmt.Errorf("connect to token-creation service at %s: %w", target, err)
	}
	if err := grpcclient.New(connection).CheckHealth(ctx, "meme-token-creation-service"); err != nil {
		_ = connection.Close()
		return nil, nil, fmt.Errorf("verify token-creation service at %s: %w", target, err)
	}
	return connection, grpcclient.NewTokenCreator(connection), nil
}

func connectTokenService(ctx context.Context, cfg config.TokenServiceConfig) (*grpc.ClientConn, *grpcclient.TokenReader, error) {
	target := cfg.Address()
	if !cfg.TLS.Enabled() && !grpcsecurity.IsLoopbackTarget(target) {
		return nil, nil, fmt.Errorf("token-service mutual TLS is required when %s is not loopback", target)
	}
	credentials, err := grpcsecurity.ClientCredentials(grpcsecurity.ClientTLSConfig{
		CAFile: cfg.TLS.CAFile, CertFile: cfg.TLS.CertFile, KeyFile: cfg.TLS.KeyFile, ServerName: cfg.TLS.ServerName,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("configure token-service gRPC client: %w", err)
	}
	connection, err := grpc.DialContext(ctx, target, grpc.WithTransportCredentials(credentials), grpc.WithBlock())
	if err != nil {
		return nil, nil, fmt.Errorf("connect to token service at %s: %w", target, err)
	}
	if err := grpcclient.New(connection).CheckHealth(ctx, "meme-token-service"); err != nil {
		_ = connection.Close()
		return nil, nil, fmt.Errorf("verify token service at %s: %w", target, err)
	}
	return connection, grpcclient.NewTokenReader(connection), nil
}
