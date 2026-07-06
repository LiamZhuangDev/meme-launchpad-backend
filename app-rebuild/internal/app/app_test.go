package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/meme-launchpad/app-rebuild/internal/config"
	"github.com/meme-launchpad/app-rebuild/internal/repository"
)

type fakeTokenReader struct{}

func (fakeTokenReader) List(context.Context, int, int) ([]repository.Token, error) {
	return []repository.Token{{ID: 1, Symbol: "MEME"}}, nil
}

func (fakeTokenReader) FindByAddress(context.Context, string) (repository.Token, error) {
	return repository.Token{ID: 1, Symbol: "MEME"}, nil
}

func TestHTTPServerUsesConfiguration(t *testing.T) {
	application := NewWithPool(config.Config{
		ServiceName: "test-api",
		HTTP:        config.HTTPConfig{Host: "127.0.0.1", Port: 48080},
	}, nil)

	server := application.HTTPServer()
	if server.Addr != "127.0.0.1:48080" {
		t.Fatalf("Addr = %q, want 127.0.0.1:48080", server.Addr)
	}

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, request)

	if recorder.Body.String() != "{\"service\":\"test-api\",\"status\":\"ok\"}\n" {
		t.Fatalf("body = %q", recorder.Body.String())
	}
}

func TestHTTPServerUsesOutboundTokenReader(t *testing.T) {
	application := NewWithPool(config.Config{ServiceName: "test-api"}, nil)
	application.TokenReader = fakeTokenReader{}
	server := application.HTTPServer()

	request := httptest.NewRequest(http.MethodGet, "/api/v1/token/list", nil)
	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"symbol":"MEME"`) {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestConnectTokenServiceRejectsRemotePlaintext(t *testing.T) {
	_, _, err := connectTokenService(context.Background(), config.TokenServiceConfig{Host: "10.0.0.20", Port: 39100})
	if err == nil {
		t.Fatal("connectTokenService() error = nil, want mTLS requirement")
	}
}

func TestConnectTokenCreationServiceRejectsRemotePlaintext(t *testing.T) {
	_, _, err := connectTokenCreationService(context.Background(), config.TokenCreationServiceConfig{Host: "10.0.0.21", Port: 39200})
	if err == nil {
		t.Fatal("connectTokenCreationService() error = nil, want mTLS requirement")
	}
}

func TestConnectUploadServiceRejectsRemotePlaintext(t *testing.T) {
	_, _, err := connectUploadService(context.Background(), config.UploadServiceConfig{Host: "10.0.0.22", Port: 39300})
	if err == nil {
		t.Fatal("connectUploadService() error = nil, want mTLS requirement")
	}
}
