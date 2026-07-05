package grpcclient

import (
	"context"
	"fmt"
	"math"
	"time"

	tokenv1 "github.com/meme-launchpad/app-rebuild/gen/token/v1"
	"github.com/meme-launchpad/app-rebuild/internal/repository"
	"google.golang.org/grpc"
)

// TokenReader adapts the internal TokenService client to the token-reading
// interface consumed by the REST transport.
type TokenReader struct {
	client tokenv1.TokenServiceClient
}

func NewTokenReader(connection grpc.ClientConnInterface) *TokenReader {
	return &TokenReader{client: tokenv1.NewTokenServiceClient(connection)}
}

func (r *TokenReader) List(ctx context.Context, limit, offset int) ([]repository.Token, error) {
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("token list limit must be from 1 to 100")
	}
	if offset < 0 || offset%limit != 0 {
		return nil, fmt.Errorf("token list offset must be a non-negative multiple of limit")
	}
	page := offset/limit + 1
	if page > math.MaxInt32 {
		return nil, fmt.Errorf("token list page exceeds gRPC range")
	}

	response, err := r.client.ListTokens(ctx, &tokenv1.ListTokensRequest{
		Page:     int32Pointer(int32(page)),
		PageSize: int32Pointer(int32(limit)),
	})
	if err != nil {
		return nil, fmt.Errorf("list tokens over internal gRPC: %w", err)
	}

	items := make([]repository.Token, 0, len(response.GetItems()))
	for _, message := range response.GetItems() {
		item, err := repositoryToken(message)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (r *TokenReader) FindByAddress(ctx context.Context, address string) (repository.Token, error) {
	response, err := r.client.GetToken(ctx, &tokenv1.GetTokenRequest{ContractAddress: address})
	if err != nil {
		return repository.Token{}, fmt.Errorf("get token over internal gRPC: %w", err)
	}
	return repositoryToken(response.GetToken())
}

func repositoryToken(message *tokenv1.Token) (repository.Token, error) {
	if message == nil {
		return repository.Token{}, fmt.Errorf("internal gRPC returned an empty token")
	}

	createdAt := time.Time{}
	if timestamp := message.GetCreatedAt(); timestamp != nil {
		if err := timestamp.CheckValid(); err != nil {
			return repository.Token{}, fmt.Errorf("internal gRPC returned an invalid token timestamp: %w", err)
		}
		createdAt = timestamp.AsTime()
	}

	var description *string
	if message.Description != nil {
		value := message.GetDescription()
		description = &value
	}
	return repository.Token{
		ID:              message.GetId(),
		Name:            message.GetName(),
		Symbol:          message.GetSymbol(),
		Logo:            message.GetLogo(),
		Description:     description,
		ContractAddress: message.GetContractAddress(),
		CreatorAddress:  message.GetCreatorAddress(),
		LaunchMode:      int(message.GetLaunchMode()),
		BNBCurrent:      message.GetBnbCurrent(),
		BNBTarget:       message.GetBnbTarget(),
		TotalSupply:     message.GetTotalSupply(),
		Status:          int(message.GetStatus()),
		CreatedAt:       createdAt,
	}, nil
}

func int32Pointer(value int32) *int32 { return &value }
