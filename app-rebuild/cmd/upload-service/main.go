// Command upload-service owns presigned upload URL creation and exposes it
// over internal gRPC.
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

	"github.com/meme-launchpad/app-rebuild/internal/auth"
	"github.com/meme-launchpad/app-rebuild/internal/config"
	"github.com/meme-launchpad/app-rebuild/internal/grpcapi"
	"github.com/meme-launchpad/app-rebuild/internal/grpcsecurity"
	"github.com/meme-launchpad/app-rebuild/internal/upload"
	"google.golang.org/grpc"
)

const serviceName = "meme-upload-service"

func main() {
	cfg, err := config.Load(os.LookupEnv)
	if err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}
	uploads, err := upload.New(cfg.COS)
	if err != nil {
		log.Fatalf("configure upload service: %v", err)
	}
	options, err := grpcsecurity.ServerOptions(cfg.GRPC.TLS)
	if err != nil {
		log.Fatalf("configure upload-service gRPC security: %v", err)
	}
	if !cfg.GRPC.TLS.Enabled() && !grpcsecurity.IsLoopbackTarget(cfg.UploadService.Address()) {
		log.Fatal("mutual TLS is required when UPLOAD_SERVICE_GRPC_HOST is not loopback")
	}

	// This auth service is only a JWT parser; AuthService RPCs are not exposed.
	tokenParser := auth.New(nil, cfg.Auth.JWTSecret, auth.SIWEConfig(cfg.Auth.SIWE))
	server := grpcapi.NewServer(serviceName, grpcapi.Dependencies{
		UploadAuth: tokenParser,
		Uploads:    uploads,
	}, options...)
	listener, err := net.Listen("tcp", cfg.UploadService.Address())
	if err != nil {
		log.Fatalf("listen for upload-service gRPC: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serverErrors := make(chan error, 1)
	go func() {
		mode := "loopback plaintext"
		if cfg.GRPC.TLS.Enabled() {
			mode = "mutual TLS"
		}
		log.Printf("%s internal gRPC (%s) listening on %s", serviceName, mode, cfg.UploadService.Address())
		if err := server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			serverErrors <- err
		}
	}()

	select {
	case <-ctx.Done():
	case err := <-serverErrors:
		log.Printf("upload service failed: %v", err)
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
