package auth

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/jackc/pgx/v5"
	"github.com/meme-launchpad/app-rebuild/internal/repository"
)

type fakeUsers struct{ user repository.User }

func (f *fakeUsers) FindByAddress(context.Context, string) (repository.User, error) {
	return repository.User{}, pgx.ErrNoRows
}
func (f *fakeUsers) Create(_ context.Context, address, username string) (repository.User, error) {
	f.user = repository.User{ID: 1, Address: address, Username: username, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	return f.user, nil
}

func TestLoginVerifiesWalletSignatureAndConsumesNonce(t *testing.T) {
	key, err := ethcrypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	address := ethcrypto.PubkeyToAddress(key.PublicKey).Hex()
	service := New(&fakeUsers{}, "test-secret")

	challenge, err := service.RequestMessage(address)
	if err != nil {
		t.Fatalf("RequestMessage() error = %v", err)
	}
	prefix := fmt.Sprintf("\x19Ethereum Signed Message:\n%d", len(challenge.Message))
	signature, err := ethcrypto.Sign(ethcrypto.Keccak256Hash([]byte(prefix+challenge.Message)).Bytes(), key)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}

	result, err := service.Login(context.Background(), address, hexutil.Encode(signature))
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if result.User.ID != 1 || result.Token == "" {
		t.Fatalf("result = %+v", result)
	}
	claims, err := service.ParseToken(result.Token)
	if err != nil || claims.UserID != 1 {
		t.Fatalf("ParseToken() = %+v, %v", claims, err)
	}

	_, err = service.Login(context.Background(), address, hexutil.Encode(signature))
	if err != ErrInvalidNonce {
		t.Fatalf("replayed Login() error = %v, want %v", err, ErrInvalidNonce)
	}
}
