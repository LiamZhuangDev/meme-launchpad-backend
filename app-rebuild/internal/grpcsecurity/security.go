// Package grpcsecurity configures mTLS and authorizes internal service
// identities carried by client certificates.
package grpcsecurity

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/meme-launchpad/app-rebuild/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

type ClientTLSConfig struct {
	CAFile     string
	CertFile   string
	KeyFile    string
	ServerName string
}

func (c ClientTLSConfig) Enabled() bool {
	return c.CAFile != "" || c.CertFile != "" || c.KeyFile != "" || c.ServerName != ""
}

// ServerOptions returns no options for the loopback plaintext development
// mode. When configured, it requires a verified client certificate and an
// explicitly allowed service identity.
func ServerOptions(cfg config.GRPCTLSConfig) ([]grpc.ServerOption, error) {
	if !cfg.Enabled() {
		return nil, nil
	}
	certificate, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load gRPC server certificate: %w", err)
	}
	clientCAs, err := certificatePool(cfg.ClientCAFile)
	if err != nil {
		return nil, fmt.Errorf("load gRPC client CA: %w", err)
	}
	transport := credentials.NewTLS(&tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{certificate},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCAs,
	})
	return []grpc.ServerOption{
		grpc.Creds(transport),
		grpc.UnaryInterceptor(RequireClientIdentity(cfg.AllowedClientIDs)),
		grpc.StreamInterceptor(RequireStreamingClientIdentity(cfg.AllowedClientIDs)),
	}, nil
}

// ClientCredentials returns plaintext credentials only when no TLS values are
// supplied. Private-network callers must provide all four TLS settings.
func ClientCredentials(cfg ClientTLSConfig) (credentials.TransportCredentials, error) {
	configured := 0
	for _, value := range []string{cfg.CAFile, cfg.CertFile, cfg.KeyFile, cfg.ServerName} {
		if value != "" {
			configured++
		}
	}
	if configured == 0 {
		return insecure.NewCredentials(), nil
	}
	if configured != 4 {
		return nil, fmt.Errorf("INTERNAL_GRPC_CA_FILE, INTERNAL_GRPC_CERT_FILE, INTERNAL_GRPC_KEY_FILE, and INTERNAL_GRPC_SERVER_NAME must be set together")
	}
	certificate, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load internal gRPC client certificate: %w", err)
	}
	rootCAs, err := certificatePool(cfg.CAFile)
	if err != nil {
		return nil, fmt.Errorf("load internal gRPC server CA: %w", err)
	}
	return credentials.NewTLS(&tls.Config{
		MinVersion:   tls.VersionTLS12,
		RootCAs:      rootCAs,
		Certificates: []tls.Certificate{certificate},
		ServerName:   cfg.ServerName,
	}), nil
}

func IsLoopbackTarget(target string) bool {
	host, _, err := net.SplitHostPort(target)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func RequireClientIdentity(allowedIdentities []string) grpc.UnaryServerInterceptor {
	allowed := identitySet(allowedIdentities)
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if err := authorizeClient(ctx, allowed); err != nil {
			return nil, err
		}
		return handler(ctx, request)
	}
}

func RequireStreamingClientIdentity(allowedIdentities []string) grpc.StreamServerInterceptor {
	allowed := identitySet(allowedIdentities)
	return func(server any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if err := authorizeClient(stream.Context(), allowed); err != nil {
			return err
		}
		return handler(server, stream)
	}
}

func authorizeClient(ctx context.Context, allowed map[string]struct{}) error {
	certificate, ok := clientCertificate(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "verified client certificate is required")
	}
	for _, identity := range certificateIdentities(certificate) {
		if _, ok := allowed[identity]; ok {
			return nil
		}
	}
	return status.Error(codes.PermissionDenied, "client service identity is not allowed")
}

func identitySet(identities []string) map[string]struct{} {
	allowed := make(map[string]struct{}, len(identities))
	for _, identity := range identities {
		allowed[identity] = struct{}{}
	}
	return allowed
}

func clientCertificate(ctx context.Context) (*x509.Certificate, bool) {
	peerInfo, ok := peer.FromContext(ctx)
	if !ok || peerInfo.AuthInfo == nil {
		return nil, false
	}
	var state tls.ConnectionState
	switch info := peerInfo.AuthInfo.(type) {
	case credentials.TLSInfo:
		state = info.State
	case *credentials.TLSInfo:
		state = info.State
	default:
		return nil, false
	}
	if len(state.PeerCertificates) == 0 {
		return nil, false
	}
	return state.PeerCertificates[0], true
}

func certificateIdentities(certificate *x509.Certificate) []string {
	identities := make([]string, 0, len(certificate.URIs)+1)
	for _, uri := range certificate.URIs {
		identities = append(identities, uri.String())
	}
	if certificate.Subject.CommonName != "" {
		identities = append(identities, certificate.Subject.CommonName)
	}
	return identities
}

func certificatePool(path string) (*x509.CertPool, error) {
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		return nil, fmt.Errorf("no certificates found in %s", path)
	}
	return pool, nil
}
