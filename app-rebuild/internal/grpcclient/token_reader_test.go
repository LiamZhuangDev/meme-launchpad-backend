package grpcclient

import (
	"context"
	"errors"
	"testing"
	"time"

	tokenv1 "github.com/meme-launchpad/app-rebuild/gen/token/v1"
	"github.com/meme-launchpad/app-rebuild/internal/httpapi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var _ httpapi.TokenReader = (*TokenReader)(nil)

type fakeTokenClient struct {
	listRequest *tokenv1.ListTokensRequest
	getRequest  *tokenv1.GetTokenRequest
	listResult  *tokenv1.ListTokensResponse
	getResult   *tokenv1.GetTokenResponse
	listError   error
	getError    error
}

func (f *fakeTokenClient) ListTokens(_ context.Context, request *tokenv1.ListTokensRequest, _ ...grpc.CallOption) (*tokenv1.ListTokensResponse, error) {
	f.listRequest = request
	return f.listResult, f.listError
}

func (f *fakeTokenClient) GetToken(_ context.Context, request *tokenv1.GetTokenRequest, _ ...grpc.CallOption) (*tokenv1.GetTokenResponse, error) {
	f.getRequest = request
	return f.getResult, f.getError
}

func TestTokenReaderListsAndConvertsTokens(t *testing.T) {
	description := "community token"
	createdAt := time.Date(2026, time.July, 5, 12, 0, 0, 0, time.UTC)
	client := &fakeTokenClient{listResult: &tokenv1.ListTokensResponse{Items: []*tokenv1.Token{{
		Id: 7, Name: "Meme", Symbol: "MEME", Logo: "logo.png", Description: &description,
		ContractAddress: "0xToken", CreatorAddress: "0xCreator", LaunchMode: 2,
		BnbCurrent: "1.5", BnbTarget: "10", TotalSupply: "1000000", Status: 1,
		CreatedAt: timestamppb.New(createdAt),
	}}}}
	reader := &TokenReader{client: client}

	items, err := reader.List(context.Background(), 5, 5)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if client.listRequest.GetPage() != 2 || client.listRequest.GetPageSize() != 5 {
		t.Fatalf("ListTokens request = %+v", client.listRequest)
	}
	if len(items) != 1 || items[0].ID != 7 || items[0].Description == nil || *items[0].Description != description || !items[0].CreatedAt.Equal(createdAt) {
		t.Fatalf("tokens = %+v", items)
	}
}

func TestTokenReaderFindsAndConvertsToken(t *testing.T) {
	client := &fakeTokenClient{getResult: &tokenv1.GetTokenResponse{Token: &tokenv1.Token{Id: 9, Symbol: "DOGE"}}}
	reader := &TokenReader{client: client}

	item, err := reader.FindByAddress(context.Background(), "0xToken")
	if err != nil {
		t.Fatalf("FindByAddress() error = %v", err)
	}
	if client.getRequest.GetContractAddress() != "0xToken" || item.ID != 9 || item.Symbol != "DOGE" {
		t.Fatalf("request = %+v, token = %+v", client.getRequest, item)
	}
}

func TestTokenReaderRejectsUnrepresentablePagination(t *testing.T) {
	reader := &TokenReader{client: &fakeTokenClient{}}
	if _, err := reader.List(context.Background(), 5, 1); err == nil {
		t.Fatal("List() error = nil, want offset validation error")
	}
}

func TestTokenReaderPreservesRPCError(t *testing.T) {
	rpcError := status.Error(codes.Unavailable, "token service unavailable")
	reader := &TokenReader{client: &fakeTokenClient{getError: rpcError}}

	_, err := reader.FindByAddress(context.Background(), "0xToken")
	if !errors.Is(err, rpcError) {
		t.Fatalf("FindByAddress() error = %v, want wrapped RPC error", err)
	}
}
