// Package grpcapi contains the gRPC boundary of the rebuild.
package grpcapi

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

// NewServer creates the gRPC transport. Business services will be registered
// here one vertical slice at a time while the REST API remains available.
func NewServer(serviceName string, tokens TokenReader) *grpc.Server {
	server := grpc.NewServer()
	healthService := health.NewServer()
	healthService.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthService.SetServingStatus(serviceName, healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(server, healthService)
	if tokens != nil {
		registerTokenService(server, tokens)
	}
	reflection.Register(server)
	return server
}
