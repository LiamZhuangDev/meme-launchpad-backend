package grpcapi

import (
	"context"
	"net"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/meme-launchpad/app-rebuild/internal/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

func testConnection(t *testing.T, dependencies Dependencies) *grpc.ClientConn {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := NewServer("test-api", dependencies)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	connection, err := grpc.DialContext(
		context.Background(),
		"bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial gRPC server: %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	return connection
}

func authenticatedContext(t *testing.T, secret, address string, userID int64) context.Context {
	t.Helper()
	token := signedTestToken(t, secret, address, userID)
	return metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+token)
}

func signedTestToken(t *testing.T, secret, address string, userID int64) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{UserID: userID, Address: address}).SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign test JWT: %v", err)
	}
	return token
}
