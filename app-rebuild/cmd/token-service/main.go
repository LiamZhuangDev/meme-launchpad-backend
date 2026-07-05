// Command token-service owns token reads and exposes them over internal gRPC.
package main

import (
	"context"
	"errors"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/meme-launchpad/app-rebuild/internal/config"
	"github.com/meme-launchpad/app-rebuild/internal/database"
	"github.com/meme-launchpad/app-rebuild/internal/grpcapi"
	"github.com/meme-launchpad/app-rebuild/internal/grpcsecurity"
	"github.com/meme-launchpad/app-rebuild/internal/repository"
	"google.golang.org/grpc"
)

const serviceName = "meme-token-service"

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
		log.Fatalf("open token-service database: %v", err)
	}
	defer pool.Close()

	options, err := grpcsecurity.ServerOptions(cfg.GRPC.TLS)
	if err != nil {
		log.Fatalf("configure token-service gRPC security: %v", err)
	}
	server := grpcapi.NewServer(serviceName, grpcapi.Dependencies{
		Tokens: repository.NewTokenRepository(pool),
	}, options...)
	listener, err := net.Listen("tcp", cfg.TokenService.Address())
	if err != nil {
		log.Fatalf("listen for token-service gRPC: %v", err)
	}

	serverErrors := make(chan error, 1)
	go func() {
		mode := "loopback plaintext"
		if cfg.GRPC.TLS.Enabled() {
			mode = "mutual TLS"
		}
		log.Printf("%s internal gRPC (%s) listening on %s", serviceName, mode, cfg.TokenService.Address())
		if err := server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			serverErrors <- err
		}
	}()

	select {
	case <-ctx.Done():
	case err := <-serverErrors:
		log.Printf("token service failed: %v", err)
	}
	gracefulStop(server, 5*time.Second)
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
