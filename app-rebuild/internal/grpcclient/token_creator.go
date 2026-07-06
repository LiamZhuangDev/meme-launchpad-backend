package grpcclient

import (
	"context"
	"fmt"

	tokencreationv1 "github.com/meme-launchpad/app-rebuild/gen/tokencreation/v1"
	"github.com/meme-launchpad/app-rebuild/internal/auth"
	"github.com/meme-launchpad/app-rebuild/internal/tokencreation"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// TokenCreator adapts the internal TokenCreationService client to the
// token-creation interface consumed by the REST transport.
type TokenCreator struct {
	client tokencreationv1.TokenCreationServiceClient
}

func NewTokenCreator(connection grpc.ClientConnInterface) *TokenCreator {
	return &TokenCreator{client: tokencreationv1.NewTokenCreationServiceClient(connection)}
}

func (c *TokenCreator) Create(ctx context.Context, request tokencreation.Request) (tokencreation.Response, error) {
	token, ok := auth.BearerTokenFromContext(ctx)
	if !ok {
		return tokencreation.Response{}, fmt.Errorf("bearer token is required for internal token creation")
	}
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
	response, err := c.client.CreateToken(ctx, &tokencreationv1.CreateTokenRequest{
		Name:                 request.Name,
		Symbol:               request.Symbol,
		LaunchTime:           request.LaunchTime,
		InitialBuyPercentage: request.InitialBuyPercentage,
	})
	if err != nil {
		return tokencreation.Response{}, fmt.Errorf("create token over internal gRPC: %w", err)
	}
	return tokencreation.Response{
		Data:             response.GetCreateArg(),
		Signature:        response.GetSignature(),
		RequestID:        response.GetRequestId(),
		Salt:             response.GetCreate2Salt(),
		PredictedAddress: response.GetPredictedAddress(),
		Nonce:            response.GetNonce(),
		Timestamp:        response.GetTimestamp(),
	}, nil
}
