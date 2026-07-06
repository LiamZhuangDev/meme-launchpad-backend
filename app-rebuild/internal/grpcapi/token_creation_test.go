package grpcapi

import (
	"context"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	tokencreationv1 "github.com/meme-launchpad/app-rebuild/gen/tokencreation/v1"
	"github.com/meme-launchpad/app-rebuild/internal/auth"
	"github.com/meme-launchpad/app-rebuild/internal/tokencreation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeTokenCreator struct {
	request  tokencreation.Request
	response tokencreation.Response
	err      error
}

func (f *fakeTokenCreator) Create(_ context.Context, request tokencreation.Request) (tokencreation.Response, error) {
	f.request = request
	return f.response, f.err
}

func TestTokenCreationUsesAuthenticatedWallet(t *testing.T) {
	const secret = "test-secret"
	address := "0x3333333333333333333333333333333333333333"
	authenticator := auth.NewJWTVerifier(secret)
	creator := &fakeTokenCreator{response: tokencreation.Response{
		Data: "0xdata", Signature: "0xsignature", RequestID: "0xrequest",
		Salt: "0xsalt", PredictedAddress: "0x4444444444444444444444444444444444444444",
		Nonce: 12, Timestamp: 34,
	}}
	connection := testConnection(t, Dependencies{TokenCreationAuth: authenticator, TokenCreation: creator})
	client := tokencreationv1.NewTokenCreationServiceClient(connection)

	response, err := client.CreateToken(authenticatedContext(t, secret, address, 7), &tokencreationv1.CreateTokenRequest{
		Name: "Meme", Symbol: "MEME", LaunchTime: 123, InitialBuyPercentage: 1000,
	})
	if err != nil {
		t.Fatalf("CreateToken() error = %v", err)
	}
	if creator.request.Creator != common.HexToAddress(address) || creator.request.Name != "Meme" || creator.request.InitialBuyPercentage != 1000 {
		t.Fatalf("creation request = %+v", creator.request)
	}
	if response.CreateArg != "0xdata" || response.PredictedAddress != creator.response.PredictedAddress {
		t.Fatalf("response = %+v", response)
	}
}

func TestTokenCreationRequiresAuthenticationAndMapsValidationErrors(t *testing.T) {
	const secret = "test-secret"
	authenticator := auth.NewJWTVerifier(secret)
	creator := &fakeTokenCreator{err: errors.New("name, symbol, and creator are required")}
	connection := testConnection(t, Dependencies{TokenCreationAuth: authenticator, TokenCreation: creator})
	client := tokencreationv1.NewTokenCreationServiceClient(connection)

	_, err := client.CreateToken(context.Background(), &tokencreationv1.CreateTokenRequest{})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("unauthenticated code = %s", status.Code(err))
	}
	ctx := authenticatedContext(t, secret, "0x3333333333333333333333333333333333333333", 7)
	_, err = client.CreateToken(ctx, &tokencreationv1.CreateTokenRequest{})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("validation code = %s", status.Code(err))
	}
}
