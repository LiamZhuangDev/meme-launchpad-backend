package config

import "testing"

func TestLoadUsesDefaults(t *testing.T) {
	config, err := Load(func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if config.ServiceName != defaultServiceName {
		t.Fatalf("ServiceName = %q, want %q", config.ServiceName, defaultServiceName)
	}
	if config.HTTP.Port != defaultHTTPPort {
		t.Fatalf("HTTP.Port = %d, want %d", config.HTTP.Port, defaultHTTPPort)
	}
	if config.HTTP.Host != defaultHTTPHost {
		t.Fatalf("HTTP.Host = %q, want %q", config.HTTP.Host, defaultHTTPHost)
	}
	if config.GRPC.Port != defaultGRPCPort {
		t.Fatalf("GRPC.Port = %d, want %d", config.GRPC.Port, defaultGRPCPort)
	}
	if config.GRPC.Host != defaultGRPCHost {
		t.Fatalf("GRPC.Host = %q, want %q", config.GRPC.Host, defaultGRPCHost)
	}
	if config.HTTP.Address() != "0.0.0.0:38081" || config.GRPC.Address() != "127.0.0.1:39090" {
		t.Fatalf("addresses = %q and %q", config.HTTP.Address(), config.GRPC.Address())
	}
	if config.TokenService.Address() != "127.0.0.1:39100" {
		t.Fatalf("TokenService.Address() = %q", config.TokenService.Address())
	}
	if config.TokenCreationService.Address() != "127.0.0.1:39200" {
		t.Fatalf("TokenCreationService.Address() = %q", config.TokenCreationService.Address())
	}
	if config.Database.URL != defaultDatabaseURL {
		t.Fatalf("Database.URL = %q, want %q", config.Database.URL, defaultDatabaseURL)
	}
}

func TestLoadReadsEnvironment(t *testing.T) {
	values := map[string]string{
		"APP_NAME":                         "local-api",
		"HTTP_HOST":                        "192.0.2.10",
		"HTTP_PORT":                        "48080",
		"GRPC_HOST":                        "10.0.0.10",
		"GRPC_PORT":                        "49090",
		"TOKEN_SERVICE_GRPC_HOST":          "10.0.0.11",
		"TOKEN_SERVICE_GRPC_PORT":          "49100",
		"TOKEN_CREATION_SERVICE_GRPC_HOST": "10.0.0.12",
		"TOKEN_CREATION_SERVICE_GRPC_PORT": "49200",
		"TOKEN_SERVICE_GRPC_CA_FILE":       "/certs/ca.crt",
		"TOKEN_SERVICE_GRPC_CERT_FILE":     "/certs/api-client.crt",
		"TOKEN_SERVICE_GRPC_KEY_FILE":      "/certs/api-client.key",
		"TOKEN_SERVICE_GRPC_SERVER_NAME":   "token-service",
		"GRPC_TLS_CERT_FILE":               "/certs/server.crt",
		"GRPC_TLS_KEY_FILE":                "/certs/server.key",
		"GRPC_TLS_CLIENT_CA_FILE":          "/certs/ca.crt",
		"GRPC_ALLOWED_CLIENT_IDS":          "spiffe://meme/client,worker",
		"DATABASE_URL":                     "postgres://local/test",
		"REDIS_ADDR":                       "localhost:6379",
		"REDIS_PASSWORD":                   "secret",
		"REDIS_DB":                         "2",
		"COS_SECRET_ID":                    "cos-id",
		"COS_SECRET_KEY":                   "cos-key",
		"COS_BUCKET":                       "bucket",
		"COS_REGION":                       "ap-guangzhou",
		"COS_DOMAIN":                       "https://cdn.example",
	}

	config, err := Load(func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if config.ServiceName != "local-api" || config.HTTP.Address() != "192.0.2.10:48080" || config.GRPC.Address() != "10.0.0.10:49090" || config.Database.URL != "postgres://local/test" {
		t.Fatalf("config = %+v, want local-api on 48080", config)
	}
	if config.TokenService.Address() != "10.0.0.11:49100" {
		t.Fatalf("token service config = %+v", config.TokenService)
	}
	if config.TokenCreationService.Address() != "10.0.0.12:49200" {
		t.Fatalf("token creation service config = %+v", config.TokenCreationService)
	}
	if !config.TokenService.TLS.Enabled() || config.TokenService.TLS.ServerName != "token-service" {
		t.Fatalf("token service TLS config = %+v", config.TokenService.TLS)
	}
	if config.Redis.Addr != "localhost:6379" || config.Redis.Password != "secret" || config.Redis.DB != 2 {
		t.Fatalf("redis config = %+v", config.Redis)
	}
	if config.COS.SecretID != "cos-id" || config.COS.Domain != "https://cdn.example" {
		t.Fatalf("cos config = %+v", config.COS)
	}
	if !config.GRPC.TLS.Enabled() || len(config.GRPC.TLS.AllowedClientIDs) != 2 {
		t.Fatalf("gRPC TLS config = %+v", config.GRPC.TLS)
	}
}

func TestLoadRejectsInvalidPort(t *testing.T) {
	_, err := Load(func(key string) (string, bool) {
		if key == "HTTP_PORT" {
			return "not-a-port", true
		}
		return "", false
	})
	if err == nil {
		t.Fatal("Load() error = nil, want invalid port error")
	}
}

func TestLoadRejectsInvalidGRPCPort(t *testing.T) {
	_, err := Load(func(key string) (string, bool) {
		if key == "GRPC_PORT" {
			return "70000", true
		}
		return "", false
	})
	if err == nil {
		t.Fatal("Load() error = nil, want invalid gRPC port error")
	}
}

func TestLoadRejectsInvalidTokenServiceGRPCPort(t *testing.T) {
	_, err := Load(func(key string) (string, bool) {
		if key == "TOKEN_SERVICE_GRPC_PORT" {
			return "70000", true
		}
		return "", false
	})
	if err == nil {
		t.Fatal("Load() error = nil, want invalid token-service gRPC port error")
	}
}

func TestLoadRejectsInvalidTokenCreationServiceGRPCPort(t *testing.T) {
	_, err := Load(func(key string) (string, bool) {
		if key == "TOKEN_CREATION_SERVICE_GRPC_PORT" {
			return "70000", true
		}
		return "", false
	})
	if err == nil {
		t.Fatal("Load() error = nil, want invalid token-creation service gRPC port error")
	}
}

func TestLoadRejectsPartialTokenServiceGRPCTLSConfig(t *testing.T) {
	_, err := Load(func(key string) (string, bool) {
		if key == "TOKEN_SERVICE_GRPC_CA_FILE" {
			return "/certs/ca.crt", true
		}
		return "", false
	})
	if err == nil {
		t.Fatal("Load() error = nil, want partial token-service gRPC TLS config error")
	}
}

func TestLoadAllowsRemoteTokenServiceWithClientTLS(t *testing.T) {
	values := map[string]string{
		"TOKEN_SERVICE_GRPC_HOST":        "token-service.internal",
		"TOKEN_SERVICE_GRPC_CA_FILE":     "/certs/ca.crt",
		"TOKEN_SERVICE_GRPC_CERT_FILE":   "/certs/api-client.crt",
		"TOKEN_SERVICE_GRPC_KEY_FILE":    "/certs/api-client.key",
		"TOKEN_SERVICE_GRPC_SERVER_NAME": "token-service.internal",
	}
	config, err := Load(func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if config.TokenService.Host != "token-service.internal" || !config.TokenService.TLS.Enabled() {
		t.Fatalf("token service config = %+v", config.TokenService)
	}
}

func TestLoadRejectsPartialGRPCTLSConfig(t *testing.T) {
	_, err := Load(func(key string) (string, bool) {
		if key == "GRPC_TLS_CERT_FILE" {
			return "/certs/server.crt", true
		}
		return "", false
	})
	if err == nil {
		t.Fatal("Load() error = nil, want partial gRPC TLS config error")
	}
}

func TestLoadRejectsNonLoopbackPlaintextGRPC(t *testing.T) {
	_, err := Load(func(key string) (string, bool) {
		if key == "GRPC_HOST" {
			return "0.0.0.0", true
		}
		return "", false
	})
	if err == nil {
		t.Fatal("Load() error = nil, want TLS requirement for routable gRPC")
	}
}

func TestLoadRejectsPartialCOSConfig(t *testing.T) {
	_, err := Load(func(key string) (string, bool) {
		if key == "COS_BUCKET" {
			return "bucket", true
		}
		return "", false
	})
	if err == nil {
		t.Fatal("Load() error = nil, want COS config error")
	}
}
