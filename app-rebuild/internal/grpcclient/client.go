// Package grpcclient wraps generated gRPC clients for internal Go processes.
package grpcclient

import (
	"context"
	"fmt"

	tokenv1 "github.com/meme-launchpad/app-rebuild/gen/token/v1"
	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

type Client struct {
	health healthpb.HealthClient
	tokens tokenv1.TokenServiceClient
}

// New accepts grpc.ClientConnInterface so production code can use a real
// connection while tests can exercise the generated clients in memory.
func New(connection grpc.ClientConnInterface) *Client {
	return &Client{
		health: healthpb.NewHealthClient(connection),
		tokens: tokenv1.NewTokenServiceClient(connection),
	}
}

func (c *Client) CheckHealth(ctx context.Context, service string) error {
	response, err := c.health.Check(ctx, &healthpb.HealthCheckRequest{Service: service})
	if err != nil {
		return fmt.Errorf("check internal gRPC health: %w", err)
	}
	if response.Status != healthpb.HealthCheckResponse_SERVING {
		return fmt.Errorf("internal gRPC service is %s", response.Status)
	}
	return nil
}

func (c *Client) ListTokens(ctx context.Context, page, pageSize int32) (*tokenv1.ListTokensResponse, error) {
	response, err := c.tokens.ListTokens(ctx, &tokenv1.ListTokensRequest{Page: &page, PageSize: &pageSize})
	if err != nil {
		return nil, fmt.Errorf("list tokens over internal gRPC: %w", err)
	}
	return response, nil
}
