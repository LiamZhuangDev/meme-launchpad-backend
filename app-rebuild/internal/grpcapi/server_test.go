package grpcapi

import (
	"context"
	"net"
	"testing"

	"github.com/meme-launchpad/app-rebuild/internal/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/test/bufconn"
)

func TestHealthCheckReportsServing(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	server := NewServer("test-api", Dependencies{})
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(server.Stop)

	connection, err := grpc.DialContext(
		context.Background(),
		"bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial gRPC server: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	response, err := healthpb.NewHealthClient(connection).Check(context.Background(), &healthpb.HealthCheckRequest{Service: "test-api"})
	if err != nil {
		t.Fatalf("check health: %v", err)
	}
	if response.Status != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("status = %s, want SERVING", response.Status)
	}
}

func TestServerRegistersEveryParallelApplicationService(t *testing.T) {
	server := NewServer("test-api", Dependencies{
		Tokens:        &fakeTokenReader{},
		Auth:          auth.New(nil, "test-secret", auth.SIWEConfig{}),
		TokenCreation: &fakeTokenCreator{},
		Uploads:       &fakeUploadPresigner{},
	})
	t.Cleanup(server.Stop)

	services := server.GetServiceInfo()
	for _, name := range []string{
		"grpc.health.v1.Health",
		"meme.token.v1.TokenService",
		"meme.auth.v1.AuthService",
		"meme.tokencreation.v1.TokenCreationService",
		"meme.upload.v1.UploadService",
	} {
		if _, ok := services[name]; !ok {
			t.Errorf("gRPC service %q is not registered", name)
		}
	}
}

func TestServerCanExposeTokenCreationWithoutAuthRPCs(t *testing.T) {
	parser := auth.NewJWTVerifier("test-secret")
	server := NewServer("token-creation-service", Dependencies{
		TokenCreationAuth: parser,
		TokenCreation:     &fakeTokenCreator{},
	})
	t.Cleanup(server.Stop)

	services := server.GetServiceInfo()
	if _, ok := services["meme.tokencreation.v1.TokenCreationService"]; !ok {
		t.Fatal("token-creation gRPC service is not registered")
	}
	if _, ok := services["meme.auth.v1.AuthService"]; ok {
		t.Fatal("standalone token-creation server must not expose auth RPCs")
	}
}

func TestServerCanExposeUploadWithoutAuthRPCs(t *testing.T) {
	parser := auth.NewJWTVerifier("test-secret")
	server := NewServer("upload-service", Dependencies{
		UploadAuth: parser,
		Uploads:    &fakeUploadPresigner{},
	})
	t.Cleanup(server.Stop)

	services := server.GetServiceInfo()
	if _, ok := services["meme.upload.v1.UploadService"]; !ok {
		t.Fatal("upload gRPC service is not registered")
	}
	if _, ok := services["meme.auth.v1.AuthService"]; ok {
		t.Fatal("standalone upload server must not expose auth RPCs")
	}
}
