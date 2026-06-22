package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/meme-launchpad/app-rebuild/internal/config"
)

func TestHTTPServerUsesConfiguration(t *testing.T) {
	application := New(config.Config{
		ServiceName: "test-api",
		HTTP:        config.HTTPConfig{Port: 48080},
	})

	server := application.HTTPServer()
	if server.Addr != ":48080" {
		t.Fatalf("Addr = %q, want :48080", server.Addr)
	}

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, request)

	if recorder.Body.String() != "{\"service\":\"test-api\",\"status\":\"ok\"}\n" {
		t.Fatalf("body = %q", recorder.Body.String())
	}
}
