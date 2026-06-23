// Package httpapi contains the HTTP boundary of the rebuild.
package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/meme-launchpad/app-rebuild/internal/auth"
)

// NewHandler returns the complete HTTP surface implemented so far.
// Future steps will add routes here, while keeping main.go focused on process
// startup and shutdown.
func NewHandler(serviceName string, authService *auth.Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", health(serviceName))
	if authService != nil {
		mux.HandleFunc("/api/v1/user/sign-msg", signMessage(authService))
		mux.HandleFunc("/api/v1/user/wallet-login", walletLogin(authService))
		mux.HandleFunc("/api/v1/user/me", currentUser(authService))
	}
	return mux
}

func signMessage(service *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		result, err := service.RequestMessage(r.URL.Query().Get("address"))
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
		header := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
			writeError(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		claims, err := service.ParseToken(header[len(prefix):])
		if err != nil {
			writeError(w, err, http.StatusUnauthorized)
			return
		}
		writeJSON(w, claims, http.StatusOK)
	}
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
