package grpcapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	authv1 "github.com/meme-launchpad/app-rebuild/gen/auth/v1"
	tokenv1 "github.com/meme-launchpad/app-rebuild/gen/token/v1"
	tokencreationv1 "github.com/meme-launchpad/app-rebuild/gen/tokencreation/v1"
	uploadv1 "github.com/meme-launchpad/app-rebuild/gen/upload/v1"
	"github.com/meme-launchpad/app-rebuild/internal/auth"
	"github.com/meme-launchpad/app-rebuild/internal/httpapi"
	"github.com/meme-launchpad/app-rebuild/internal/repository"
	"github.com/meme-launchpad/app-rebuild/internal/tokencreation"
)

func TestTransportParityTokenListUsesSamePagination(t *testing.T) {
	reader := &fakeTokenReader{items: []repository.Token{{ID: 1, Symbol: "MEME"}}}
	httpRequest := httptest.NewRequest(http.MethodGet, "/api/v1/token/list?page=2&pageSize=5", nil)
	httpResponse := httptest.NewRecorder()
	httpapi.NewHandler("test", nil, reader, nil, nil).ServeHTTP(httpResponse, httpRequest)
	if httpResponse.Code != http.StatusOK {
		t.Fatalf("REST status = %d", httpResponse.Code)
	}
	restLimit, restOffset := reader.limit, reader.offset

	client := tokenv1.NewTokenServiceClient(testConnection(t, Dependencies{Tokens: reader}))
	page, pageSize := int32(2), int32(5)
	grpcResponse, err := client.ListTokens(context.Background(), &tokenv1.ListTokensRequest{Page: &page, PageSize: &pageSize})
	if err != nil {
		t.Fatalf("gRPC ListTokens() error = %v", err)
	}
	if restLimit != reader.limit || restOffset != reader.offset || reader.limit != 5 || reader.offset != 5 {
		t.Fatalf("REST pagination = %d/%d, gRPC pagination = %d/%d", restLimit, restOffset, reader.limit, reader.offset)
	}
	if len(grpcResponse.Items) != 1 || grpcResponse.Items[0].Symbol != "MEME" {
		t.Fatalf("gRPC response = %+v", grpcResponse)
	}
}

func TestTransportParityResolvesSameJWTIdentity(t *testing.T) {
	const secret = "test-secret"
	const address = "0x3333333333333333333333333333333333333333"
	authenticator := auth.New(nil, secret, auth.SIWEConfig{})
	token := signedTestToken(t, secret, address, 7)

	httpRequest := httptest.NewRequest(http.MethodGet, "/api/v1/user/me", nil)
	httpRequest.Header.Set("Authorization", "Bearer "+token)
	httpResponse := httptest.NewRecorder()
	httpapi.NewHandler("test", authenticator, nil, nil, nil).ServeHTTP(httpResponse, httpRequest)
	if httpResponse.Code != http.StatusOK {
		t.Fatalf("REST status = %d", httpResponse.Code)
	}
	var restClaims struct {
		UserID  int64  `json:"userId"`
		Address string `json:"address"`
	}
	if err := json.NewDecoder(httpResponse.Body).Decode(&restClaims); err != nil {
		t.Fatalf("decode REST claims: %v", err)
	}

	client := authv1.NewAuthServiceClient(testConnection(t, Dependencies{Auth: authenticator}))
	grpcUser, err := client.GetCurrentUser(authenticatedContext(t, secret, address, 7), &authv1.GetCurrentUserRequest{})
	if err != nil {
		t.Fatalf("gRPC GetCurrentUser() error = %v", err)
	}
	if restClaims.UserID != grpcUser.UserId || restClaims.Address != grpcUser.Address {
		t.Fatalf("REST identity = %+v, gRPC identity = %+v", restClaims, grpcUser)
	}
}

type parityCreationStore struct {
	requests []repository.TokenCreationRequest
}

func (s *parityCreationStore) Create(_ context.Context, request repository.TokenCreationRequest) error {
	s.requests = append(s.requests, request)
	return nil
}

func TestTransportParityTokenCreationBindsSameWallet(t *testing.T) {
	const secret = "test-secret"
	const address = "0x3333333333333333333333333333333333333333"
	authenticator := auth.New(nil, secret, auth.SIWEConfig{})
	store := &parityCreationStore{}
	key, err := ethcrypto.HexToECDSA("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	creator, err := tokencreation.New(tokencreation.Config{
		ChainID: 97, Core: common.HexToAddress("0x1111111111111111111111111111111111111111"),
		Factory:               common.HexToAddress("0x2222222222222222222222222222222222222222"),
		TokenCreationBytecode: []byte{1, 2, 3}, Signer: key,
	}, store)
	if err != nil {
		t.Fatal(err)
	}

	httpRequest := httptest.NewRequest(http.MethodPost, "/api/v1/token/create", strings.NewReader(`{"name":"Meme","symbol":"MEME","initialBuyPercentage":1000}`))
	httpRequest.Header.Set("Authorization", "Bearer "+signedTestToken(t, secret, address, 7))
	httpResponse := httptest.NewRecorder()
	httpapi.NewHandler("test", authenticator, nil, creator, nil).ServeHTTP(httpResponse, httpRequest)
	if httpResponse.Code != http.StatusOK {
		t.Fatalf("REST status = %d, body = %s", httpResponse.Code, httpResponse.Body)
	}

	client := tokencreationv1.NewTokenCreationServiceClient(testConnection(t, Dependencies{Auth: authenticator, TokenCreation: creator}))
	_, err = client.CreateToken(authenticatedContext(t, secret, address, 7), &tokencreationv1.CreateTokenRequest{
		Name: "Meme", Symbol: "MEME", InitialBuyPercentage: 1000,
	})
	if err != nil {
		t.Fatalf("gRPC CreateToken() error = %v", err)
	}
	if len(store.requests) != 2 || store.requests[0].CreatorAddress != store.requests[1].CreatorAddress || store.requests[0].CreatorAddress != common.HexToAddress(address).Hex() {
		t.Fatalf("stored creation requests = %+v", store.requests)
	}
}

func TestTransportParityUploadUsesSameStorageArguments(t *testing.T) {
	const secret = "test-secret"
	const address = "0x3333333333333333333333333333333333333333"
	authenticator := auth.New(nil, secret, auth.SIWEConfig{})
	uploads := &fakeUploadPresigner{}

	httpRequest := httptest.NewRequest(http.MethodGet, "/api/v1/file/token-logo-presign?mimeType=image/webp&chainId=56", nil)
	httpRequest.Header.Set("Authorization", "Bearer "+signedTestToken(t, secret, address, 7))
	httpResponse := httptest.NewRecorder()
	httpapi.NewHandler("test", authenticator, nil, nil, uploads).ServeHTTP(httpResponse, httpRequest)
	if httpResponse.Code != http.StatusOK {
		t.Fatalf("REST status = %d", httpResponse.Code)
	}
	restFolder, restMIME, restChainID := uploads.folder, uploads.mimeType, uploads.chainID

	client := uploadv1.NewUploadServiceClient(testConnection(t, Dependencies{Auth: authenticator, Uploads: uploads}))
	chainID := int32(56)
	_, err := client.PresignImage(authenticatedContext(t, secret, address, 7), &uploadv1.PresignImageRequest{
		Kind: uploadv1.ImageKind_IMAGE_KIND_TOKEN_LOGO, MimeType: "image/webp", ChainId: &chainID,
	})
	if err != nil {
		t.Fatalf("gRPC PresignImage() error = %v", err)
	}
	if restFolder != uploads.folder || restMIME != uploads.mimeType || restChainID != uploads.chainID {
		t.Fatalf("REST upload args = %s/%s/%d, gRPC args = %s/%s/%d", restFolder, restMIME, restChainID, uploads.folder, uploads.mimeType, uploads.chainID)
	}
}
