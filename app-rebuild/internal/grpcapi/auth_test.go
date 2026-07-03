package grpcapi

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/jackc/pgx/v5"
	authv1 "github.com/meme-launchpad/app-rebuild/gen/auth/v1"
	"github.com/meme-launchpad/app-rebuild/internal/auth"
	"github.com/meme-launchpad/app-rebuild/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type fakeAuthUsers struct{ user repository.User }

func (f *fakeAuthUsers) FindByAddress(context.Context, string) (repository.User, error) {
	return repository.User{}, pgx.ErrNoRows
}

func (f *fakeAuthUsers) Create(_ context.Context, address, username string) (repository.User, error) {
	now := time.Now().UTC()
	f.user = repository.User{ID: 7, Address: address, Username: username, CreatedAt: now, UpdatedAt: now}
	return f.user, nil
}

func TestAuthServiceCompletesWalletLoginAndReadsMetadata(t *testing.T) {
	key, err := ethcrypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	address := ethcrypto.PubkeyToAddress(key.PublicKey).Hex()
	authenticator := auth.New(&fakeAuthUsers{}, "test-secret", auth.SIWEConfig{
		Domain: "app.example", URI: "https://app.example/login", ChainID: 97,
	})
	client := authClient(t, authenticator)

	challenge, err := client.RequestSignMessage(context.Background(), &authv1.RequestSignMessageRequest{Address: address})
	if err != nil {
		t.Fatalf("RequestSignMessage() error = %v", err)
	}
	// prefix implements Ethereum’s personal_sign / ERC-191 signing format
	prefix := fmt.Sprintf("\x19Ethereum Signed Message:\n%d", len(challenge.Message))
	signature, err := ethcrypto.Sign(ethcrypto.Keccak256Hash([]byte(prefix+challenge.Message)).Bytes(), key)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}

	login, err := client.WalletLogin(context.Background(), &authv1.WalletLoginRequest{
		Address: address, Signature: hexutil.Encode(signature),
	})
	if err != nil {
		t.Fatalf("WalletLogin() error = %v", err)
	}
	if login.Token == "" || login.User.Id != 7 || login.User.Address != address {
		t.Fatalf("login = %+v", login)
	}

	authorized := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+login.Token)
	current, err := client.GetCurrentUser(authorized, &authv1.GetCurrentUserRequest{})
	if err != nil {
		t.Fatalf("GetCurrentUser() error = %v", err)
	}
	if current.UserId != 7 || current.Address != address || current.ExpiresAt == nil {
		t.Fatalf("current user = %+v", current)
	}

	_, err = client.WalletLogin(context.Background(), &authv1.WalletLoginRequest{
		Address: address, Signature: hexutil.Encode(signature),
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("replayed WalletLogin() code = %s, want Unauthenticated", status.Code(err))
	}
}

func TestAuthServiceMapsInvalidInputAndMissingToken(t *testing.T) {
	authenticator := auth.New(&fakeAuthUsers{}, "test-secret", auth.SIWEConfig{})
	client := authClient(t, authenticator)

	_, err := client.RequestSignMessage(context.Background(), &authv1.RequestSignMessageRequest{Address: "not-an-address"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("RequestSignMessage() code = %s, want InvalidArgument", status.Code(err))
	}
	_, err = client.GetCurrentUser(context.Background(), &authv1.GetCurrentUserRequest{})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("GetCurrentUser() code = %s, want Unauthenticated", status.Code(err))
	}
}

func authClient(t *testing.T, authenticator Authenticator) authv1.AuthServiceClient {
	t.Helper()
	return authv1.NewAuthServiceClient(testConnection(t, Dependencies{Auth: authenticator}))
}
