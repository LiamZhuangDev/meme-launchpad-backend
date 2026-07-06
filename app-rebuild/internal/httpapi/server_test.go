package httpapi

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
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
	folder    string
	mimeType  string
	chainID   int
	token     string
	confirmed bool
}

type fakeTokenCreator struct {
	request tokencreation.Request
	token   string
}

func (f *fakeTokenCreator) Create(ctx context.Context, request tokencreation.Request) (tokencreation.Response, error) {
	f.request = request
	f.token, _ = auth.BearerTokenFromContext(ctx)
	return tokencreation.Response{Data: "0xdata", Signature: "0xsignature"}, nil
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

func (f *fakePresigner) Presign(ctx context.Context, folder, mimeType string, chainID int) (upload.PresignResult, error) {
	f.folder, f.mimeType, f.chainID = folder, mimeType, chainID
	f.token, _ = auth.BearerTokenFromContext(ctx)
	return upload.PresignResult{UploadURL: "https://upload.example", PublicURL: "https://cdn.example/image.png", Key: folder + "/image.png"}, nil
}

func (f *fakePresigner) Confirm(ctx context.Context) error {
	f.token, _ = auth.BearerTokenFromContext(ctx)
	f.confirmed = true
	return nil
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
	address := "0x3333333333333333333333333333333333333333"
	authService, token := testAuth(t, address)
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

func TestCreateTokenCarriesBearerTokenToCreator(t *testing.T) {
	creator := &fakeTokenCreator{}
	tokenAddress := "0x3333333333333333333333333333333333333333"
	authService, token := testAuth(t, tokenAddress)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/token/create", strings.NewReader(`{"name":"Meme","symbol":"MEME"}`))
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()

	NewHandler("test", authService, nil, creator, nil).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body)
	}
	if creator.token != token || creator.request.Creator != common.HexToAddress("0x3333333333333333333333333333333333333333") {
		t.Fatalf("token forwarded = %t, request = %+v", creator.token == token, creator.request)
	}
}

func TestPresignImageRequiresAuthAndUsesFolder(t *testing.T) {
	presigner := &fakePresigner{}
	authService, token := testAuth(t, "0x3333333333333333333333333333333333333333")
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
	if presigner.token != token {
		t.Fatal("validated bearer token was not forwarded to upload service")
	}
	if !strings.Contains(recorder.Body.String(), `"uploadUrl":"https://upload.example"`) {
		t.Fatalf("body = %s", recorder.Body)
	}
}

func TestUploadConfirmCarriesBearerTokenToService(t *testing.T) {
	uploads := &fakePresigner{}
	authService, token := testAuth(t, "0x3333333333333333333333333333333333333333")
	request := httptest.NewRequest(http.MethodPost, "/api/v1/file/upload-confirm", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()

	NewHandler("test", authService, nil, nil, uploads).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || !uploads.confirmed || uploads.token != token {
		t.Fatalf("status = %d, confirmed = %t, token forwarded = %t", recorder.Code, uploads.confirmed, uploads.token == token)
	}
}

func newCreationService(key *ecdsa.PrivateKey, store *fakeCreationStore) (*tokencreation.Service, error) {
	return tokencreation.New(tokencreation.Config{
		ChainID: 97, Core: common.HexToAddress("0x1111111111111111111111111111111111111111"),
		Factory:               common.HexToAddress("0x2222222222222222222222222222222222222222"),
		TokenCreationBytecode: []byte{1, 2, 3}, Signer: key,
	}, store)
}

func testAuth(t *testing.T, address string) (*auth.Service, string) {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	service := auth.New(nil, privateKey, auth.SIWEConfig{})
	token, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, auth.Claims{Address: address}).SignedString(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return service, token
}
