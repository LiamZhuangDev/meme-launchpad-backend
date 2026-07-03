package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/meme-launchpad/app-rebuild/internal/config"
)

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

func TestGRPCServerIsConfigured(t *testing.T) {
	application := NewWithPool(config.Config{ServiceName: "test-api"}, nil)
	server := application.GRPCServer()
	t.Cleanup(server.Stop)

	if _, ok := server.GetServiceInfo()["grpc.health.v1.Health"]; !ok {
		t.Fatal("standard gRPC health service is not registered")
	}
}
