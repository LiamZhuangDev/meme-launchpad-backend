// Package config loads process configuration for the rebuild.
package config

import (
	"fmt"
	"strconv"
)

const (
	defaultServiceName = "meme-launchpad-rebuild-api"
	defaultHTTPPort    = 38081
	defaultDatabaseURL = "postgres://postgres:postgres@localhost:5432/meme_launchpad?sslmode=disable"
)

// Config contains only the configuration Step 2 needs. Future steps will add
// database, Redis, and chain settings here when their clients are introduced.
type Config struct {
	ServiceName string
	HTTP        HTTPConfig
	Database    DatabaseConfig
	Auth        AuthConfig
}

type HTTPConfig struct {
	Port int
}

type DatabaseConfig struct {
	URL string
}

type AuthConfig struct {
	JWTSecret string
	SIWE      SIWEConfig
}

type SIWEConfig struct {
	Domain  string
	URI     string
	ChainID int64
}

// LookupEnv matches os.LookupEnv and makes configuration parsing testable
// without mutating the process environment.
type LookupEnv func(string) (string, bool)

// Load reads optional environment variables and validates their values.
//
// APP_NAME defaults to meme-launchpad-rebuild-api.
// HTTP_PORT defaults to 38081 and must be a valid TCP port.
// DATABASE_URL defaults to a local development PostgreSQL database.
func Load(lookup LookupEnv) (Config, error) {
	config := Config{
		ServiceName: defaultServiceName,
		HTTP: HTTPConfig{
			Port: defaultHTTPPort,
		},
		Database: DatabaseConfig{
			URL: defaultDatabaseURL,
		},
		Auth: AuthConfig{
			JWTSecret: "development-only-secret-change-me",
			SIWE:      SIWEConfig{Domain: "localhost:38081", URI: "http://localhost:38081", ChainID: 97},
		},
	}

	if name, ok := lookup("APP_NAME"); ok && name != "" {
		config.ServiceName = name
	}

	if rawPort, ok := lookup("HTTP_PORT"); ok && rawPort != "" {
		port, err := strconv.Atoi(rawPort)
		if err != nil || port < 1 || port > 65535 {
			return Config{}, fmt.Errorf("HTTP_PORT must be an integer from 1 to 65535")
		}
		config.HTTP.Port = port
	}

	if databaseURL, ok := lookup("DATABASE_URL"); ok && databaseURL != "" {
		config.Database.URL = databaseURL
	}
	if secret, ok := lookup("JWT_SECRET"); ok && secret != "" {
		config.Auth.JWTSecret = secret
	}
	if domain, ok := lookup("SIWE_DOMAIN"); ok && domain != "" {
		config.Auth.SIWE.Domain = domain
	}
	if uri, ok := lookup("SIWE_URI"); ok && uri != "" {
		config.Auth.SIWE.URI = uri
	}
	if rawChainID, ok := lookup("SIWE_CHAIN_ID"); ok && rawChainID != "" {
		chainID, err := strconv.ParseInt(rawChainID, 10, 64)
		if err != nil || chainID < 1 {
			return Config{}, fmt.Errorf("SIWE_CHAIN_ID must be a positive integer")
		}
		config.Auth.SIWE.ChainID = chainID
	}

	return config, nil
}
