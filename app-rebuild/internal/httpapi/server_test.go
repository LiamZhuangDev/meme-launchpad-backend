package httpapi

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/golang-jwt/jwt/v5"
	"github.com/meme-launchpad/app-rebuild/internal/auth"
	"github.com/meme-launchpad/app-rebuild/internal/repository"
	"github.com/meme-launchpad/app-rebuild/internal/tokencreation"
	"github.com/meme-launchpad/app-rebuild/internal/upload"
)

type fakeTokenReader struct {
	limit  int
	offset int
}

type fakeCreationStore struct {
	value repository.TokenCreationRequest
}

type fakePresigner struct {
	folder   string
	mimeType string
	chainID  int
}

func (s *fakeCreationStore) Create(_ context.Context, value repository.TokenCreationRequest) error {
	s.value = value
	return nil
}

func (f *fakeTokenReader) List(_ context.Context, limit, offset int) ([]repository.Token, error) {
	f.limit, f.offset = limit, offset
	return []repository.Token{{ID: 1, Name: "Meme", Symbol: "MEME"}}, nil
}

func (f *fakeTokenReader) FindByAddress(context.Context, string) (repository.Token, error) {
	return repository.Token{}, nil
}

func (f *fakePresigner) Presign(folder, mimeType string, chainID int) (upload.PresignResult, error) {
	f.folder, f.mimeType, f.chainID = folder, mimeType, chainID
	return upload.PresignResult{UploadURL: "https://upload.example", PublicURL: "https://cdn.example/image.png", Key: folder + "/image.png"}, nil
}

func TestHealth(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()

	NewHandler("meme-launchpad-rebuild-api", nil, nil, nil, nil).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	want := "{\"service\":\"meme-launchpad-rebuild-api\",\"status\":\"ok\"}\n"
	if recorder.Body.String() != want {
		t.Fatalf("body = %q, want %q", recorder.Body.String(), want)
	}
}

func TestTokenListUsesPagination(t *testing.T) {
	tokens := &fakeTokenReader{}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/token/list?page=2&pageSize=5", nil)
	recorder := httptest.NewRecorder()

	NewHandler("test", nil, tokens, nil, nil).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if tokens.limit != 5 || tokens.offset != 5 {
		t.Fatalf("limit, offset = %d, %d; want 5, 5", tokens.limit, tokens.offset)
	}
	if !strings.Contains(recorder.Body.String(), `"symbol":"MEME"`) {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestCreateTokenUsesAuthenticatedWalletAsCreator(t *testing.T) {
	key, err := ethcrypto.HexToECDSA("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeCreationStore{}
	creation, err := newCreationService(key, store)
	if err != nil {
		t.Fatal(err)
	}
	authService := auth.New(nil, "test-secret", auth.SIWEConfig{})
	address := "0x3333333333333333333333333333333333333333"
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{Address: address}).SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/token/create", strings.NewReader(`{"name":"Meme","symbol":"MEME","initialBuyPercentage":1000}`))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	NewHandler("test", authService, nil, creation, nil).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body)
	}
	if store.value.CreatorAddress != common.HexToAddress(address).Hex() {
		t.Fatalf("creator = %s", store.value.CreatorAddress)
	}
	var response map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response["signature"] == "" || response["createArg"] == "" {
		t.Fatalf("response = %#v", response)
	}
}

func TestPresignImageRequiresAuthAndUsesFolder(t *testing.T) {
	authService := auth.New(nil, "test-secret", auth.SIWEConfig{})
	presigner := &fakePresigner{}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{Address: "0x3333333333333333333333333333333333333333"}).SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/file/token-logo-presign?mimeType=image/webp&chainId=56", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()

	NewHandler("test", authService, nil, nil, presigner).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body)
	}
	if presigner.folder != "token-logo" || presigner.mimeType != "image/webp" || presigner.chainID != 56 {
		t.Fatalf("presign args = %s %s %d", presigner.folder, presigner.mimeType, presigner.chainID)
	}
	if !strings.Contains(recorder.Body.String(), `"uploadUrl":"https://upload.example"`) {
		t.Fatalf("body = %s", recorder.Body)
	}
}

func newCreationService(key *ecdsa.PrivateKey, store *fakeCreationStore) (*tokencreation.Service, error) {
	return tokencreation.New(tokencreation.Config{
		ChainID: 97, Core: common.HexToAddress("0x1111111111111111111111111111111111111111"),
		Factory:               common.HexToAddress("0x2222222222222222222222222222222222222222"),
		TokenCreationBytecode: []byte{1, 2, 3}, Signer: key,
	}, store)
}
