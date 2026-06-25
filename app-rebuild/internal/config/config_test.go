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
	if config.Database.URL != defaultDatabaseURL {
		t.Fatalf("Database.URL = %q, want %q", config.Database.URL, defaultDatabaseURL)
	}
}

func TestLoadReadsEnvironment(t *testing.T) {
	values := map[string]string{
		"APP_NAME":       "local-api",
		"HTTP_PORT":      "48080",
		"DATABASE_URL":   "postgres://local/test",
		"REDIS_ADDR":     "localhost:6379",
		"REDIS_PASSWORD": "secret",
		"REDIS_DB":       "2",
		"COS_SECRET_ID":  "cos-id",
		"COS_SECRET_KEY": "cos-key",
		"COS_BUCKET":     "bucket",
		"COS_REGION":     "ap-guangzhou",
		"COS_DOMAIN":     "https://cdn.example",
	}

	config, err := Load(func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if config.ServiceName != "local-api" || config.HTTP.Port != 48080 || config.Database.URL != "postgres://local/test" {
		t.Fatalf("config = %+v, want local-api on 48080", config)
	}
	if config.Redis.Addr != "localhost:6379" || config.Redis.Password != "secret" || config.Redis.DB != 2 {
		t.Fatalf("redis config = %+v", config.Redis)
	}
	if config.COS.SecretID != "cos-id" || config.COS.Domain != "https://cdn.example" {
		t.Fatalf("cos config = %+v", config.COS)
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
