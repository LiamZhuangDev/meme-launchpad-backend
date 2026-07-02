package grpcapi

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	tokenv1 "github.com/meme-launchpad/app-rebuild/gen/token/v1"
	"github.com/meme-launchpad/app-rebuild/internal/repository"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TokenReader is owned by the gRPC transport. The concrete token repository
// also satisfies the REST transport's equivalent interface.
type TokenReader interface {
	List(context.Context, int, int) ([]repository.Token, error)
	FindByAddress(context.Context, string) (repository.Token, error)
}

type tokenService struct {
	tokenv1.UnimplementedTokenServiceServer
	tokens TokenReader
}

func registerTokenService(server *grpc.Server, tokens TokenReader) {
	tokenv1.RegisterTokenServiceServer(server, &tokenService{tokens: tokens})
}

func (s *tokenService) ListTokens(ctx context.Context, request *tokenv1.ListTokensRequest) (*tokenv1.ListTokensResponse, error) {
	page, pageSize, err := grpcPagination(request)
	if err != nil {
		return nil, err
	}

	items, err := s.tokens.List(ctx, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to list tokens")
	}

	response := &tokenv1.ListTokensResponse{
		Items:    make([]*tokenv1.Token, 0, len(items)),
		Page:     int32(page),
		PageSize: int32(pageSize),
	}
	for _, item := range items {
		response.Items = append(response.Items, tokenMessage(item))
	}
	return response, nil
}

func (s *tokenService) GetToken(ctx context.Context, request *tokenv1.GetTokenRequest) (*tokenv1.GetTokenResponse, error) {
	if request.GetContractAddress() == "" {
		return nil, status.Error(codes.InvalidArgument, "contract_address is required")
	}

	item, err := s.tokens.FindByAddress(ctx, request.GetContractAddress())
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, status.Error(codes.NotFound, "token not found")
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to get token")
	}
	return &tokenv1.GetTokenResponse{Token: tokenMessage(item)}, nil
}

func grpcPagination(request *tokenv1.ListTokensRequest) (int, int, error) {
	page, pageSize := int32(1), int32(20)
	if request.Page != nil {
		page = request.GetPage()
		if page < 1 {
			return 0, 0, status.Error(codes.InvalidArgument, "page must be positive")
		}
	}
	if request.PageSize != nil {
		pageSize = request.GetPageSize()
		if pageSize < 1 || pageSize > 100 {
			return 0, 0, status.Error(codes.InvalidArgument, "page_size must be from 1 to 100")
		}
	}
	return int(page), int(pageSize), nil
}

func tokenMessage(item repository.Token) *tokenv1.Token {
	result := &tokenv1.Token{
		Id:              item.ID,
		Name:            item.Name,
		Symbol:          item.Symbol,
		Logo:            item.Logo,
		ContractAddress: item.ContractAddress,
		CreatorAddress:  item.CreatorAddress,
		LaunchMode:      int32(item.LaunchMode),
		BnbCurrent:      item.BNBCurrent,
		BnbTarget:       item.BNBTarget,
		TotalSupply:     item.TotalSupply,
		Status:          int32(item.Status),
		CreatedAt:       timestamppb.New(item.CreatedAt),
	}
	if item.Description != nil {
		result.Description = item.Description
	}
	return result
}
