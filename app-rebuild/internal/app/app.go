// Package app wires configuration and runtime dependencies together.
package app

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/meme-launchpad/app-rebuild/internal/auth"
	"github.com/meme-launchpad/app-rebuild/internal/config"
	"github.com/meme-launchpad/app-rebuild/internal/database"
	"github.com/meme-launchpad/app-rebuild/internal/grpcapi"
	"github.com/meme-launchpad/app-rebuild/internal/httpapi"
	"github.com/meme-launchpad/app-rebuild/internal/repository"
	"github.com/meme-launchpad/app-rebuild/internal/tokencreation"
	"github.com/meme-launchpad/app-rebuild/internal/upload"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
)

// Application is the dependency container for one API process.
type Application struct {
	Config        config.Config
	DB            *pgxpool.Pool
	Redis         *redis.Client
	Users         *repository.UserRepository
	Tokens        *repository.TokenRepository
	Auth          *auth.Service
	TokenCreation *tokencreation.Service
	Uploads       *upload.Service
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
	if cfg.Redis.Addr != "" {
		application.Redis = redis.NewClient(&redis.Options{Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB})
	}
	if pool != nil {
		application.Users = repository.NewUserRepository(pool)
		application.Tokens = repository.NewTokenRepository(pool)
		challengeStore := auth.ChallengeStore(auth.NewMemoryChallengeStore())
		if application.Redis != nil {
			challengeStore = auth.NewRedisChallengeStore(application.Redis, "")
		}
		application.Auth = auth.NewWithChallengeStore(application.Users, cfg.Auth.JWTSecret, auth.SIWEConfig(cfg.Auth.SIWE), challengeStore)
		application.TokenCreation = newTokenCreation(cfg, repository.NewTokenCreationRepository(pool))
	}
	application.Uploads = newUploadService(cfg)
	return application
}

func (a *Application) Close() {
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
		Handler:           httpapi.NewHandler(a.Config.ServiceName, a.Auth, a.Tokens, a.TokenCreation, a.Uploads),
		ReadHeaderTimeout: 5 * time.Second,
	}
}

func (a *Application) GRPCServer(options ...grpc.ServerOption) *grpc.Server {
	dependencies := grpcapi.Dependencies{}
	if a.Tokens != nil {
		dependencies.Tokens = a.Tokens
	}
	if a.Auth != nil {
		dependencies.Auth = a.Auth
		if a.TokenCreation != nil {
			dependencies.TokenCreation = a.TokenCreation
		}
		if a.Uploads != nil {
			dependencies.Uploads = a.Uploads
		}
	}
	return grpcapi.NewServer(a.Config.ServiceName, dependencies, options...)
}

func newUploadService(cfg config.Config) *upload.Service {
	if cfg.COS.SecretID == "" && cfg.COS.SecretKey == "" && cfg.COS.Bucket == "" && cfg.COS.Region == "" && cfg.COS.Domain == "" {
		return nil
	}
	service, err := upload.New(cfg.COS)
	if err != nil {
		return nil
	}
	return service
}

func newTokenCreation(cfg config.Config, store *repository.TokenCreationRepository) *tokencreation.Service {
	chain := cfg.TokenCreation
	if chain.ChainID == "" && chain.CoreContract == "" && chain.FactoryContract == "" && chain.SignerPrivateKey == "" && chain.TokenBytecode == "" {
		return nil
	}
	chainID, err := strconv.ParseInt(chain.ChainID, 10, 64)
	if err != nil || !common.IsHexAddress(chain.CoreContract) || !common.IsHexAddress(chain.FactoryContract) {
		return nil
	}
	signer, err := ethcrypto.HexToECDSA(strings.TrimPrefix(chain.SignerPrivateKey, "0x"))
	if err != nil {
		return nil
	}
	bytecode, err := tokencreation.ParseBytecode(chain.TokenBytecode)
	if err != nil {
		return nil
	}
	service, err := tokencreation.New(tokencreation.Config{ChainID: chainID, Core: common.HexToAddress(chain.CoreContract), Factory: common.HexToAddress(chain.FactoryContract), TokenCreationBytecode: bytecode, Signer: signer}, store)
	if err != nil {
		return nil
	}
	return service
}
