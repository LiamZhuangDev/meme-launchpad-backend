// Package config loads process configuration for the rebuild.
package config

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
)

const (
	defaultServiceName = "meme-launchpad-rebuild-api"
	defaultHTTPPort    = 38081
	defaultDatabaseURL = "postgres://postgres:postgres@localhost:5432/meme_launchpad?sslmode=disable"
)

// Config contains only the configuration Step 2 needs. Future steps will add
// database, Redis, and chain settings here when their clients are introduced.
type Config struct {
	ServiceName   string
	HTTP          HTTPConfig
	Database      DatabaseConfig
	Auth          AuthConfig
	TokenCreation TokenCreationConfig
	Indexer       IndexerConfig
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

// TokenCreationConfig is deliberately separate from SIWE's chain identity:
// the former signs the exact payload checked by MEMECore.createToken.
type TokenCreationConfig struct {
	ChainID, CoreContract, FactoryContract, SignerPrivateKey, TokenBytecode string
}

// IndexerConfig describes the independently deployed blockchain consumer.
// It remains optional for the API process; cmd/indexer requires it explicitly.
type IndexerConfig struct {
	RPCURL         string
	ChainID        int64
	CoreContract   string
	StartBlock     uint64
	BlockBatchSize uint64
	PollInterval   int
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
		Indexer: IndexerConfig{BlockBatchSize: 500, PollInterval: 5},
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
	for env, target := range map[string]*string{
		"TOKEN_CREATION_CHAIN_ID":   &config.TokenCreation.ChainID,
		"TOKEN_CREATION_CORE":       &config.TokenCreation.CoreContract,
		"TOKEN_CREATION_FACTORY":    &config.TokenCreation.FactoryContract,
		"TOKEN_CREATION_SIGNER_KEY": &config.TokenCreation.SignerPrivateKey,
		"TOKEN_CREATION_BYTECODE":   &config.TokenCreation.TokenBytecode,
	} {
		if value, ok := lookup(env); ok && value != "" {
			*target = value
		}
	}
	if err := config.validateTokenCreation(); err != nil {
		return Config{}, err
	}
	if value, ok := lookup("INDEXER_RPC_URL"); ok && value != "" {
		config.Indexer.RPCURL = value
	}
	if value, ok := lookup("INDEXER_CORE"); ok && value != "" {
		config.Indexer.CoreContract = value
	}
	if value, ok := lookup("INDEXER_CHAIN_ID"); ok && value != "" {
		chainID, err := strconv.ParseInt(value, 10, 64)
		if err != nil || chainID < 1 {
			return Config{}, fmt.Errorf("INDEXER_CHAIN_ID must be a positive integer")
		}
		config.Indexer.ChainID = chainID
	}
	if value, ok := lookup("INDEXER_START_BLOCK"); ok && value != "" {
		block, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("INDEXER_START_BLOCK must be a non-negative integer")
		}
		config.Indexer.StartBlock = block
	}
	if value, ok := lookup("INDEXER_BLOCK_BATCH_SIZE"); ok && value != "" {
		size, err := strconv.ParseUint(value, 10, 64)
		if err != nil || size < 1 {
			return Config{}, fmt.Errorf("INDEXER_BLOCK_BATCH_SIZE must be positive")
		}
		config.Indexer.BlockBatchSize = size
	}
	if value, ok := lookup("INDEXER_POLL_INTERVAL_SECONDS"); ok && value != "" {
		seconds, err := strconv.Atoi(value)
		if err != nil || seconds < 1 {
			return Config{}, fmt.Errorf("INDEXER_POLL_INTERVAL_SECONDS must be positive")
		}
		config.Indexer.PollInterval = seconds
	}

	return config, nil
}

// Validate verifies the values required when cmd/indexer is started.
func (c IndexerConfig) Validate() error {
	if c.RPCURL == "" || c.ChainID < 1 || !common.IsHexAddress(c.CoreContract) {
		return fmt.Errorf("INDEXER_RPC_URL, INDEXER_CHAIN_ID, and INDEXER_CORE are required for the indexer")
	}
	if c.BlockBatchSize < 1 || c.PollInterval < 1 {
		return fmt.Errorf("indexer batch size and poll interval must be positive")
	}
	return nil
}

func (c Config) validateTokenCreation() error {
	v := c.TokenCreation
	values := []string{v.ChainID, v.CoreContract, v.FactoryContract, v.SignerPrivateKey, v.TokenBytecode}
	configured := 0
	for _, value := range values {
		if value != "" {
			configured++
		}
	}
	if configured == 0 {
		return nil
	}
	if configured != len(values) {
		return fmt.Errorf("TOKEN_CREATION_CHAIN_ID, TOKEN_CREATION_CORE, TOKEN_CREATION_FACTORY, TOKEN_CREATION_SIGNER_KEY, and TOKEN_CREATION_BYTECODE must be set together")
	}
	if chainID, err := strconv.ParseInt(v.ChainID, 10, 64); err != nil || chainID < 1 {
		return fmt.Errorf("TOKEN_CREATION_CHAIN_ID must be a positive integer")
	}
	if !common.IsHexAddress(v.CoreContract) || !common.IsHexAddress(v.FactoryContract) {
		return fmt.Errorf("TOKEN_CREATION_CORE and TOKEN_CREATION_FACTORY must be Ethereum addresses")
	}
	if _, err := ethcrypto.HexToECDSA(strings.TrimPrefix(v.SignerPrivateKey, "0x")); err != nil {
		return fmt.Errorf("TOKEN_CREATION_SIGNER_KEY is invalid: %w", err)
	}
	if decoded, err := hex.DecodeString(strings.TrimPrefix(v.TokenBytecode, "0x")); err != nil || len(decoded) == 0 {
		return fmt.Errorf("TOKEN_CREATION_BYTECODE must be non-empty hexadecimal bytecode")
	}
	return nil
}
