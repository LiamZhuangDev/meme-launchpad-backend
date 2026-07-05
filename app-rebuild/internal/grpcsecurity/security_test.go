package grpcsecurity

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"net/url"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

func TestRequireClientIdentityAllowsConfiguredURISAN(t *testing.T) {
	identity, err := url.Parse("spiffe://meme-launchpad/internal-client")
	if err != nil {
		t.Fatal(err)
	}
	ctx := tlsPeerContext(&x509.Certificate{URIs: []*url.URL{identity}})
	interceptor := RequireClientIdentity([]string{identity.String()})

	response, err := interceptor(ctx, "request", &grpc.UnaryServerInfo{}, func(context.Context, any) (any, error) {
		return "response", nil
	})
	if err != nil || response != "response" {
		t.Fatalf("interceptor response = %v, error = %v", response, err)
	}
}

func TestRequireClientIdentityRejectsUnknownOrMissingIdentity(t *testing.T) {
	interceptor := RequireClientIdentity([]string{"spiffe://meme-launchpad/allowed"})
	unknown := tlsPeerContext(&x509.Certificate{Subject: pkix.Name{CommonName: "unknown-worker"}})

	_, err := interceptor(unknown, nil, &grpc.UnaryServerInfo{}, func(context.Context, any) (any, error) {
		return nil, nil
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("unknown identity code = %s, want PermissionDenied", status.Code(err))
	}
	_, err = interceptor(context.Background(), nil, &grpc.UnaryServerInfo{}, func(context.Context, any) (any, error) {
		return nil, nil
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("missing certificate code = %s, want Unauthenticated", status.Code(err))
	}
}

func TestClientCredentialsRequireCompleteTLSConfig(t *testing.T) {
	credentials, err := ClientCredentials(ClientTLSConfig{})
	if err != nil || credentials.Info().SecurityProtocol != "insecure" {
		t.Fatalf("plaintext credentials = %v, %v", credentials, err)
	}
	_, err = ClientCredentials(ClientTLSConfig{CAFile: "ca.crt"})
	if err == nil {
		t.Fatal("partial client TLS config error = nil")
	}
}

func TestLoopbackTargetDetection(t *testing.T) {
	for _, target := range []string{"127.0.0.1:39090", "localhost:39090", "[::1]:39090"} {
		if !IsLoopbackTarget(target) {
			t.Errorf("IsLoopbackTarget(%q) = false", target)
		}
	}
	if IsLoopbackTarget("meme-api:39090") {
		t.Fatal("private DNS target incorrectly treated as loopback")
	}
}

func tlsPeerContext(certificate *x509.Certificate) context.Context {
	return peer.NewContext(context.Background(), &peer.Peer{AuthInfo: credentials.TLSInfo{
		State: tls.ConnectionState{PeerCertificates: []*x509.Certificate{certificate}},
	}})
}
