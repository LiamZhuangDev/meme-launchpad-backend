// Command internal-client demonstrates service-to-service gRPC consumption.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/meme-launchpad/app-rebuild/internal/grpcclient"
	"github.com/meme-launchpad/app-rebuild/internal/grpcsecurity"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
)

func main() {
	target := envOrDefault("INTERNAL_GRPC_TARGET", "127.0.0.1:39090")
	timeout := durationOrDefault("INTERNAL_GRPC_TIMEOUT", 5*time.Second)
	page := int32OrDefault("TOKEN_PAGE", 1)
	pageSize := int32OrDefault("TOKEN_PAGE_SIZE", 20)
	clientTLS := grpcsecurity.ClientTLSConfig{
		CAFile:     os.Getenv("INTERNAL_GRPC_CA_FILE"),
		CertFile:   os.Getenv("INTERNAL_GRPC_CERT_FILE"),
		KeyFile:    os.Getenv("INTERNAL_GRPC_KEY_FILE"),
		ServerName: os.Getenv("INTERNAL_GRPC_SERVER_NAME"),
	}
	if !clientTLS.Enabled() && !grpcsecurity.IsLoopbackTarget(target) {
		log.Fatal("mutual TLS is required when INTERNAL_GRPC_TARGET is not loopback")
	}
	transportCredentials, err := grpcsecurity.ClientCredentials(clientTLS)
	if err != nil {
		log.Fatalf("configure internal gRPC security: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	connection, err := grpc.DialContext(
		ctx,
		target,
		grpc.WithTransportCredentials(transportCredentials),
		grpc.WithBlock(),
	)
	if err != nil {
		log.Fatalf("connect to internal gRPC at %s: %v", target, err)
	}
	defer func() { _ = connection.Close() }()

	client := grpcclient.New(connection)
	if err := client.CheckHealth(ctx, ""); err != nil {
		log.Fatal(err)
	}
	response, err := client.ListTokens(ctx, page, pageSize)
	if err != nil {
		log.Fatal(err)
	}
	encoded, err := protojson.MarshalOptions{Indent: "  ", UseProtoNames: true}.Marshal(response)
	if err != nil {
		log.Fatalf("encode token response: %v", err)
	}
	fmt.Println(string(encoded))
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func durationOrDefault(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		log.Fatalf("%s must be a positive duration", key)
	}
	return parsed
}

func int32OrDefault(key string, fallback int32) int32 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil || parsed < 1 {
		log.Fatalf("%s must be a positive integer", key)
	}
	return int32(parsed)
}
