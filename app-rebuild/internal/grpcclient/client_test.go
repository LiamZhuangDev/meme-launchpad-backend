package grpcclient

import (
	"context"
	"net"
	"testing"

	tokenv1 "github.com/meme-launchpad/app-rebuild/gen/token/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/test/bufconn"
)

type tokenServer struct {
	tokenv1.UnimplementedTokenServiceServer
	request *tokenv1.ListTokensRequest
}

func (s *tokenServer) ListTokens(_ context.Context, request *tokenv1.ListTokensRequest) (*tokenv1.ListTokensResponse, error) {
	s.request = request
	return &tokenv1.ListTokensResponse{
		Items: []*tokenv1.Token{{Id: 1, Name: "Meme", Symbol: "MEME"}},
		Page:  request.GetPage(), PageSize: request.GetPageSize(),
	}, nil
}

func TestClientChecksHealthAndListsTokens(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(server, healthServer)
	tokens := &tokenServer{}
	tokenv1.RegisterTokenServiceServer(server, tokens)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	connection, err := grpc.DialContext(
		context.Background(),
		"bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial test server: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	client := New(connection)
	if err := client.CheckHealth(context.Background(), ""); err != nil {
		t.Fatalf("CheckHealth() error = %v", err)
	}
	response, err := client.ListTokens(context.Background(), 2, 5)
	if err != nil {
		t.Fatalf("ListTokens() error = %v", err)
	}
	if tokens.request.GetPage() != 2 || tokens.request.GetPageSize() != 5 {
		t.Fatalf("request = %+v", tokens.request)
	}
	if len(response.Items) != 1 || response.Items[0].Symbol != "MEME" {
		t.Fatalf("response = %+v", response)
	}
}
