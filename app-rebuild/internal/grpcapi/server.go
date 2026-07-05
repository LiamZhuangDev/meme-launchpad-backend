// Package grpcapi contains the gRPC boundary of the rebuild.
package grpcapi

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

// Dependencies contains the application capabilities exposed over gRPC.
// Each transport owns its interfaces while sharing the concrete services and
// repositories assembled by internal/app.
type Dependencies struct {
	Tokens        TokenReader
	Auth          Authenticator
	TokenCreation TokenCreator
	Uploads       UploadPresigner
}

// NewServer creates the gRPC transport. Business services are registered here
// one vertical slice at a time while the REST API remains available.
func NewServer(serviceName string, dependencies Dependencies, options ...grpc.ServerOption) *grpc.Server {
	server := grpc.NewServer(options...)
	healthService := health.NewServer()
	healthService.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthService.SetServingStatus(serviceName, healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(server, healthService)
	if dependencies.Tokens != nil {
		registerTokenService(server, dependencies.Tokens)
	}
	if dependencies.Auth != nil {
		registerAuthService(server, dependencies.Auth)
		if dependencies.TokenCreation != nil {
			registerTokenCreationService(server, dependencies.Auth, dependencies.TokenCreation)
		}
		if dependencies.Uploads != nil {
			registerUploadService(server, dependencies.Auth, dependencies.Uploads)
		}
	}
	reflection.Register(server)
	return server
}
