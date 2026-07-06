// Command token-creation-service owns signed token-creation intents and exposes
// them over internal gRPC.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/meme-launchpad/app-rebuild/internal/auth"
	"github.com/meme-launchpad/app-rebuild/internal/config"
	"github.com/meme-launchpad/app-rebuild/internal/database"
	"github.com/meme-launchpad/app-rebuild/internal/grpcapi"
	"github.com/meme-launchpad/app-rebuild/internal/grpcsecurity"
	"github.com/meme-launchpad/app-rebuild/internal/repository"
	"github.com/meme-launchpad/app-rebuild/internal/tokencreation"
	"google.golang.org/grpc"
)

const serviceName = "meme-token-creation-service"

func main() {
	cfg, err := config.Load(os.LookupEnv)
	if err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startupCtx, cancelStartup := context.WithTimeout(ctx, 5*time.Second)
	defer cancelStartup()

	pool, err := database.Open(startupCtx, cfg.Database)
	if err != nil {
		log.Fatalf("open token-creation database: %v", err)
	}
	defer pool.Close()

	creator, err := newTokenCreator(cfg, pool)
	if err != nil {
		log.Fatalf("configure token creation: %v", err)
	}
	options, err := grpcsecurity.ServerOptions(cfg.GRPC.TLS)
	if err != nil {
		log.Fatalf("configure token-creation gRPC security: %v", err)
	}
	if !cfg.GRPC.TLS.Enabled() && !grpcsecurity.IsLoopbackTarget(cfg.TokenCreationService.Address()) {
		log.Fatal("mutual TLS is required when TOKEN_CREATION_SERVICE_GRPC_HOST is not loopback")
	}

	publicKey, err := auth.LoadJWTPublicKey(cfg.Auth.JWTPublicKeyFile)
	if err != nil {
		log.Fatalf("configure JWT verifier: %v", err)
	}
	tokenVerifier := auth.NewJWTVerifier(publicKey)
	server := grpcapi.NewServer(serviceName, grpcapi.Dependencies{
		TokenCreationAuth: tokenVerifier,
		TokenCreation:     creator,
	}, options...)
	listener, err := net.Listen("tcp", cfg.TokenCreationService.Address())
	if err != nil {
		log.Fatalf("listen for token-creation gRPC: %v", err)
	}

	serverErrors := make(chan error, 1)
	go func() {
		mode := "loopback plaintext"
		if cfg.GRPC.TLS.Enabled() {
			mode = "mutual TLS"
		}
		log.Printf("%s internal gRPC (%s) listening on %s", serviceName, mode, cfg.TokenCreationService.Address())
		if err := server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			serverErrors <- err
		}
	}()

	select {
	case <-ctx.Done():
	case err := <-serverErrors:
		log.Printf("token-creation service failed: %v", err)
	}
	gracefulStop(server, 5*time.Second)
}

func newTokenCreator(cfg config.Config, pool *pgxpool.Pool) (*tokencreation.Service, error) {
	chain := cfg.TokenCreation
	chainID, err := strconv.ParseInt(chain.ChainID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("TOKEN_CREATION_CHAIN_ID is required")
	}
	signer, err := ethcrypto.HexToECDSA(strings.TrimPrefix(chain.SignerPrivateKey, "0x"))
	if err != nil {
		return nil, fmt.Errorf("parse TOKEN_CREATION_SIGNER_KEY: %w", err)
	}
	bytecode, err := tokencreation.ParseBytecode(chain.TokenBytecode)
	if err != nil {
		return nil, err
	}
	return tokencreation.New(tokencreation.Config{
		ChainID: chainID, Core: common.HexToAddress(chain.CoreContract), Factory: common.HexToAddress(chain.FactoryContract),
		TokenCreationBytecode: bytecode, Signer: signer,
	}, repository.NewTokenCreationRepository(pool))
}

func gracefulStop(server *grpc.Server, timeout time.Duration) {
	stopped := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(stopped)
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-stopped:
	case <-timer.C:
		server.Stop()
	}
}
