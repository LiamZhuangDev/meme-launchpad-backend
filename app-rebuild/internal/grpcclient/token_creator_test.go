package grpcclient

import (
	"context"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	tokencreationv1 "github.com/meme-launchpad/app-rebuild/gen/tokencreation/v1"
	"github.com/meme-launchpad/app-rebuild/internal/auth"
	"github.com/meme-launchpad/app-rebuild/internal/httpapi"
	"github.com/meme-launchpad/app-rebuild/internal/tokencreation"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var _ httpapi.TokenCreator = (*TokenCreator)(nil)

type fakeTokenCreationClient struct {
	request *tokencreationv1.CreateTokenRequest
	result  *tokencreationv1.CreateTokenResponse
	err     error
	token   string
}

func (f *fakeTokenCreationClient) CreateToken(ctx context.Context, request *tokencreationv1.CreateTokenRequest, _ ...grpc.CallOption) (*tokencreationv1.CreateTokenResponse, error) {
	f.request = request
	f.token = firstMetadataValue(ctx, "authorization")
	return f.result, f.err
}

func TestTokenCreatorForwardsBearerAndConvertsResponse(t *testing.T) {
	client := &fakeTokenCreationClient{result: &tokencreationv1.CreateTokenResponse{
		CreateArg: "0xdata", Signature: "0xsignature", RequestId: "0xrequest", Create2Salt: "0xsalt",
		PredictedAddress: "0x4444444444444444444444444444444444444444", Nonce: 12, Timestamp: 34,
	}}
	creator := &TokenCreator{client: client}
	ctx := auth.WithBearerToken(context.Background(), "jwt-value")

	response, err := creator.Create(ctx, tokencreation.Request{
		Name: "Meme", Symbol: "MEME", Creator: common.HexToAddress("0x3333333333333333333333333333333333333333"),
		LaunchTime: 123, InitialBuyPercentage: 1000,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if client.token != "Bearer jwt-value" {
		t.Fatalf("authorization metadata = %q", client.token)
	}
	if client.request.GetName() != "Meme" || client.request.GetLaunchTime() != 123 || client.request.GetInitialBuyPercentage() != 1000 {
		t.Fatalf("CreateToken request = %+v", client.request)
	}
	if response.Data != "0xdata" || response.RequestID != "0xrequest" || response.PredictedAddress != client.result.PredictedAddress {
		t.Fatalf("response = %+v", response)
	}
}

func TestTokenCreatorRequiresBearerToken(t *testing.T) {
	creator := &TokenCreator{client: &fakeTokenCreationClient{}}
	if _, err := creator.Create(context.Background(), tokencreation.Request{}); err == nil {
		t.Fatal("Create() error = nil, want missing bearer token error")
	}
}

func TestTokenCreatorPreservesRPCError(t *testing.T) {
	rpcError := status.Error(codes.InvalidArgument, "invalid token request")
	creator := &TokenCreator{client: &fakeTokenCreationClient{err: rpcError}}
	_, err := creator.Create(auth.WithBearerToken(context.Background(), "jwt-value"), tokencreation.Request{})
	if !errors.Is(err, rpcError) {
		t.Fatalf("Create() error = %v, want wrapped RPC error", err)
	}
}

func firstMetadataValue(ctx context.Context, key string) string {
	values, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		return ""
	}
	entries := values.Get(key)
	if len(entries) == 0 {
		return ""
	}
	return entries[0]
}
