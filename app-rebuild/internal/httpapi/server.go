// Package httpapi contains the HTTP boundary of the rebuild.
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
	"github.com/meme-launchpad/app-rebuild/internal/auth"
	"github.com/meme-launchpad/app-rebuild/internal/repository"
	"github.com/meme-launchpad/app-rebuild/internal/tokencreation"
	"github.com/meme-launchpad/app-rebuild/internal/upload"
)

// NewHandler returns the complete HTTP surface implemented so far.
// Future steps will add routes here, while keeping main.go focused on process
// startup and shutdown.
type TokenReader interface {
	List(context.Context, int, int) ([]repository.Token, error)
	FindByAddress(context.Context, string) (repository.Token, error)
}

type Presigner interface {
	Presign(folder, mimeType string, chainID int) (upload.PresignResult, error)
}

func NewHandler(serviceName string, authService *auth.Service, tokens TokenReader, creation *tokencreation.Service, uploads Presigner) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", health(serviceName))
	if tokens != nil {
		mux.HandleFunc("/api/v1/token/list", tokenList(tokens))
		mux.HandleFunc("/api/v1/token/detail", tokenDetail(tokens))
	}
	if authService != nil {
		mux.HandleFunc("/api/v1/user/sign-msg", signMessage(authService))
		mux.HandleFunc("/api/v1/user/wallet-login", walletLogin(authService))
		mux.HandleFunc("/api/v1/user/me", currentUser(authService))
		if creation != nil {
			mux.HandleFunc("/api/v1/token/create", createToken(authService, creation))
		}
		if uploads != nil {
			mux.HandleFunc("/api/v1/file/token-logo-presign", presignImage(authService, uploads, "token-logo"))
			mux.HandleFunc("/api/v1/file/token-banner-presign", presignImage(authService, uploads, "token-banner"))
			mux.HandleFunc("/api/v1/file/activity-image-presign", presignImage(authService, uploads, "activity-image"))
			mux.HandleFunc("/api/v1/file/upload-confirm", uploadConfirm(authService))
		}
	}
	return mux
}

func presignImage(authService *auth.Service, uploads Presigner, folder string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if _, ok := bearerClaims(w, r, authService); !ok {
			return
		}
		mimeType := r.URL.Query().Get("mimeType")
		if mimeType == "" {
			mimeType = "image/png"
		}
		chainID := 97
		if raw := r.URL.Query().Get("chainId"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 {
				writeError(w, "chainId must be positive", http.StatusBadRequest)
				return
			}
			chainID = parsed
		}
		result, err := uploads.Presign(folder, mimeType, chainID)
		if err != nil {
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, result, http.StatusOK)
	}
}

func uploadConfirm(authService *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if _, ok := bearerClaims(w, r, authService); !ok {
			return
		}
		writeJSON(w, map[string]bool{"ok": true}, http.StatusOK)
	}
}

func createToken(authService *auth.Service, creation *tokencreation.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		claims, ok := bearerClaims(w, r, authService)
		if !ok {
			return
		}
		var request struct {
			Name                 string `json:"name"`
			Symbol               string `json:"symbol"`
			LaunchTime           uint64 `json:"launchTime"`
			InitialBuyPercentage uint64 `json:"initialBuyPercentage"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			writeError(w, "invalid request body", http.StatusBadRequest)
			return
		}
		result, err := creation.Create(r.Context(), tokencreation.Request{Name: request.Name, Symbol: request.Symbol, Creator: common.HexToAddress(claims.Address), LaunchTime: request.LaunchTime, InitialBuyPercentage: request.InitialBuyPercentage})
		if err != nil {
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, result, http.StatusOK)
	}
}

func tokenList(tokens TokenReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		page, size, err := pagination(r)
		if err != nil {
			writeError(w, err, http.StatusBadRequest)
			return
		}
		items, err := tokens.List(r.Context(), size, (page-1)*size)
		if err != nil {
			writeError(w, "failed to list tokens", http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"items": items, "page": page, "pageSize": size}, http.StatusOK)
	}
}

func tokenDetail(tokens TokenReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		address := r.URL.Query().Get("address")
		if address == "" {
			writeError(w, "address is required", http.StatusBadRequest)
			return
		}
		token, err := tokens.FindByAddress(r.Context(), address)
		if err != nil {
			writeError(w, "token not found", http.StatusNotFound)
			return
		}
		writeJSON(w, token, http.StatusOK)
	}
}

func pagination(r *http.Request) (int, int, error) {
	page, size := 1, 20
	if raw := r.URL.Query().Get("page"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			return 0, 0, fmt.Errorf("page must be positive")
		}
		page = parsed
	}
	if raw := r.URL.Query().Get("pageSize"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			return 0, 0, fmt.Errorf("pageSize must be from 1 to 100")
		}
		size = parsed
	}
	return page, size, nil
}

func signMessage(service *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		result, err := service.RequestMessage(r.Context(), r.URL.Query().Get("address"))
		if err != nil {
			writeError(w, err, http.StatusBadRequest)
			return
		}
		writeJSON(w, result, http.StatusOK)
	}
}
func walletLogin(service *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var request struct {
			Address   string `json:"address"`
			Signature string `json:"signature"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeError(w, err, http.StatusBadRequest)
			return
		}
		result, err := service.Login(r.Context(), request.Address, request.Signature)
		if err != nil {
			writeError(w, err, http.StatusUnauthorized)
			return
		}
		writeJSON(w, result, http.StatusOK)
	}
}
func currentUser(service *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		claims, ok := bearerClaims(w, r, service)
		if !ok {
			return
		}
		writeJSON(w, claims, http.StatusOK)
	}
}

func bearerClaims(w http.ResponseWriter, r *http.Request, service *auth.Service) (auth.Claims, bool) {
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
		writeError(w, "missing bearer token", http.StatusUnauthorized)
		return auth.Claims{}, false
	}
	claims, err := service.ParseToken(header[len(prefix):])
	if err != nil {
		writeError(w, err, http.StatusUnauthorized)
		return auth.Claims{}, false
	}
	return claims, true
}
func writeJSON(w http.ResponseWriter, value any, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, err any, status int) {
	writeJSON(w, map[string]any{"error": err}, status)
}

func health(serviceName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"service": serviceName,
			"status":  "ok",
		})
	}
}
