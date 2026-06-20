// Package httpapi contains the HTTP boundary of the rebuild.
package httpapi

import (
	"encoding/json"
	"net/http"
)

// NewHandler returns the complete HTTP surface implemented in Step 1.
// Future steps will add routes here, while keeping main.go focused on process
// startup and shutdown.
func NewHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", health)
	return mux
}

func health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"service": "meme-launchpad-rebuild-api",
		"status":  "ok",
	})
}
