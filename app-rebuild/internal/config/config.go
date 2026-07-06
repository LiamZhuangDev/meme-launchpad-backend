// Package config loads process configuration for the rebuild.
package config

import (
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
)

const (
	defaultServiceName              = "meme-launchpad-rebuild-api"
	defaultHTTPHost                 = "0.0.0.0"
	defaultHTTPPort                 = 38081
	defaultGRPCHost                 = "127.0.0.1"
	defaultGRPCPort                 = 39090
	defaultTokenServiceHost         = "127.0.0.1"
	defaultTokenServicePort         = 39100
	defaultTokenCreationServiceHost = "127.0.0.1"
	defaultTokenCreationServicePort = 39200
	defaultUploadServiceHost        = "127.0.0.1"
	defaultUploadServicePort        = 39300
	defaultDatabaseURL              = "postgres://postgres:postgres@localhost:5432/meme_launchpad?sslmode=disable"
)

// Config contains the process configuration shared by the API and indexer
// composition roots.
type Config struct {
	ServiceName          string
	HTTP                 HTTPConfig
	GRPC                 GRPCConfig
	TokenService         TokenServiceConfig
	TokenCreationService TokenCreationServiceConfig
	UploadService        UploadServiceConfig
	Database             DatabaseConfig
	Redis                RedisConfig
	Auth                 AuthConfig
	TokenCreation        TokenCreationConfig
	Indexer              IndexerConfig
	COS                  COSConfig
}

type HTTPConfig struct {
	Host string
	Port int
}

type GRPCConfig struct {
	Host string
	Port int
	TLS  GRPCTLSConfig
}

type GRPCTLSConfig struct {
	CertFile         string
	KeyFile          string
	ClientCAFile     string
	AllowedClientIDs []string
}

type TokenServiceConfig struct {
	Host string
	Port int
	TLS  GRPCClientTLSConfig
}

type TokenCreationServiceConfig struct {
	Host string
	Port int
	TLS  GRPCClientTLSConfig
}

type UploadServiceConfig struct {
	Host string
	Port int
}

type GRPCClientTLSConfig struct {
	CAFile     string
	CertFile   string
	KeyFile    string
	ServerName string
}

func (c GRPCTLSConfig) Enabled() bool { return c.CertFile != "" }
func (c GRPCClientTLSConfig) Enabled() bool {
	return c.CAFile != "" || c.CertFile != "" || c.KeyFile != "" || c.ServerName != ""
}

func (c HTTPConfig) Address() string { return net.JoinHostPort(c.Host, strconv.Itoa(c.Port)) }
func (c GRPCConfig) Address() string { return net.JoinHostPort(c.Host, strconv.Itoa(c.Port)) }
func (c TokenServiceConfig) Address() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}
func (c TokenCreationServiceConfig) Address() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}
func (c UploadServiceConfig) Address() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}

type DatabaseConfig struct {
	URL string
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
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

type COSConfig struct {
	SecretID  string
	SecretKey string
	Bucket    string
	Region    string
	Domain    string
}

// LookupEnv matches os.LookupEnv and makes configuration parsing testable
// without mutating the process environment.
type LookupEnv func(string) (string, bool)

// Load reads optional environment variables and validates their values.
//
// APP_NAME defaults to meme-launchpad-rebuild-api.
// HTTP_HOST defaults to 0.0.0.0 and HTTP_PORT defaults to 38081.
// GRPC_HOST defaults to 127.0.0.1 and GRPC_PORT defaults to 39090.
// TOKEN_SERVICE_GRPC_HOST defaults to 127.0.0.1 and TOKEN_SERVICE_GRPC_PORT defaults to 39100.
// TOKEN_CREATION_SERVICE_GRPC_HOST defaults to 127.0.0.1 and TOKEN_CREATION_SERVICE_GRPC_PORT defaults to 39200.
// UPLOAD_SERVICE_GRPC_HOST defaults to 127.0.0.1 and UPLOAD_SERVICE_GRPC_PORT defaults to 39300.
// DATABASE_URL defaults to a local development PostgreSQL database.
func Load(lookup LookupEnv) (Config, error) {
	config := Config{
		ServiceName: defaultServiceName,
		HTTP: HTTPConfig{
			Host: defaultHTTPHost,
			Port: defaultHTTPPort,
		},
		GRPC: GRPCConfig{
			Host: defaultGRPCHost,
			Port: defaultGRPCPort,
		},
		TokenService: TokenServiceConfig{
			Host: defaultTokenServiceHost,
			Port: defaultTokenServicePort,
		},
		TokenCreationService: TokenCreationServiceConfig{
			Host: defaultTokenCreationServiceHost,
			Port: defaultTokenCreationServicePort,
		},
		UploadService: UploadServiceConfig{
			Host: defaultUploadServiceHost,
			Port: defaultUploadServicePort,
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
	if host, ok := lookup("HTTP_HOST"); ok && host != "" {
		config.HTTP.Host = host
	}
	if host, ok := lookup("GRPC_HOST"); ok && host != "" {
		config.GRPC.Host = host
	}
	if host, ok := lookup("TOKEN_SERVICE_GRPC_HOST"); ok && host != "" {
		config.TokenService.Host = host
	}
	if host, ok := lookup("TOKEN_CREATION_SERVICE_GRPC_HOST"); ok && host != "" {
		config.TokenCreationService.Host = host
	}
	if host, ok := lookup("UPLOAD_SERVICE_GRPC_HOST"); ok && host != "" {
		config.UploadService.Host = host
	}

	if rawPort, ok := lookup("HTTP_PORT"); ok && rawPort != "" {
		port, err := strconv.Atoi(rawPort)
		if err != nil || port < 1 || port > 65535 {
			return Config{}, fmt.Errorf("HTTP_PORT must be an integer from 1 to 65535")
		}
		config.HTTP.Port = port
	}
	if rawPort, ok := lookup("GRPC_PORT"); ok && rawPort != "" {
		port, err := strconv.Atoi(rawPort)
		if err != nil || port < 1 || port > 65535 {
			return Config{}, fmt.Errorf("GRPC_PORT must be an integer from 1 to 65535")
		}
		config.GRPC.Port = port
	}
	if rawPort, ok := lookup("TOKEN_SERVICE_GRPC_PORT"); ok && rawPort != "" {
		port, err := strconv.Atoi(rawPort)
		if err != nil || port < 1 || port > 65535 {
			return Config{}, fmt.Errorf("TOKEN_SERVICE_GRPC_PORT must be an integer from 1 to 65535")
		}
		config.TokenService.Port = port
	}
	if rawPort, ok := lookup("TOKEN_CREATION_SERVICE_GRPC_PORT"); ok && rawPort != "" {
		port, err := strconv.Atoi(rawPort)
		if err != nil || port < 1 || port > 65535 {
			return Config{}, fmt.Errorf("TOKEN_CREATION_SERVICE_GRPC_PORT must be an integer from 1 to 65535")
		}
		config.TokenCreationService.Port = port
	}
	if rawPort, ok := lookup("UPLOAD_SERVICE_GRPC_PORT"); ok && rawPort != "" {
		port, err := strconv.Atoi(rawPort)
		if err != nil || port < 1 || port > 65535 {
			return Config{}, fmt.Errorf("UPLOAD_SERVICE_GRPC_PORT must be an integer from 1 to 65535")
		}
		config.UploadService.Port = port
	}
	if value, ok := lookup("TOKEN_SERVICE_GRPC_CA_FILE"); ok && value != "" {
		config.TokenService.TLS.CAFile = value
	}
	if value, ok := lookup("TOKEN_SERVICE_GRPC_CERT_FILE"); ok && value != "" {
		config.TokenService.TLS.CertFile = value
	}
	if value, ok := lookup("TOKEN_SERVICE_GRPC_KEY_FILE"); ok && value != "" {
		config.TokenService.TLS.KeyFile = value
	}
	if value, ok := lookup("TOKEN_SERVICE_GRPC_SERVER_NAME"); ok && value != "" {
		config.TokenService.TLS.ServerName = value
	}
	if value, ok := lookup("TOKEN_CREATION_SERVICE_GRPC_CA_FILE"); ok && value != "" {
		config.TokenCreationService.TLS.CAFile = value
	}
	if value, ok := lookup("TOKEN_CREATION_SERVICE_GRPC_CERT_FILE"); ok && value != "" {
		config.TokenCreationService.TLS.CertFile = value
	}
	if value, ok := lookup("TOKEN_CREATION_SERVICE_GRPC_KEY_FILE"); ok && value != "" {
		config.TokenCreationService.TLS.KeyFile = value
	}
	if value, ok := lookup("TOKEN_CREATION_SERVICE_GRPC_SERVER_NAME"); ok && value != "" {
		config.TokenCreationService.TLS.ServerName = value
	}
	if value, ok := lookup("GRPC_TLS_CERT_FILE"); ok && value != "" {
		config.GRPC.TLS.CertFile = value
	}
	if value, ok := lookup("GRPC_TLS_KEY_FILE"); ok && value != "" {
		config.GRPC.TLS.KeyFile = value
	}
	if value, ok := lookup("GRPC_TLS_CLIENT_CA_FILE"); ok && value != "" {
		config.GRPC.TLS.ClientCAFile = value
	}
	if value, ok := lookup("GRPC_ALLOWED_CLIENT_IDS"); ok && value != "" {
		for _, identity := range strings.Split(value, ",") {
			if identity = strings.TrimSpace(identity); identity != "" {
				config.GRPC.TLS.AllowedClientIDs = append(config.GRPC.TLS.AllowedClientIDs, identity)
			}
		}
	}
	if err := config.GRPC.TLS.Validate(); err != nil {
		return Config{}, err
	}
	if err := config.TokenService.TLS.Validate("TOKEN_SERVICE_GRPC"); err != nil {
		return Config{}, err
	}
	if err := config.TokenCreationService.TLS.Validate("TOKEN_CREATION_SERVICE_GRPC"); err != nil {
		return Config{}, err
	}
	if !config.GRPC.TLS.Enabled() && !loopbackHost(config.GRPC.Host) {
		return Config{}, fmt.Errorf("gRPC TLS is required when GRPC_HOST is not loopback")
	}

	if databaseURL, ok := lookup("DATABASE_URL"); ok && databaseURL != "" {
		config.Database.URL = databaseURL
	}
	if redisAddr, ok := lookup("REDIS_ADDR"); ok && redisAddr != "" {
		config.Redis.Addr = redisAddr
	}
	if redisPassword, ok := lookup("REDIS_PASSWORD"); ok {
		config.Redis.Password = redisPassword
	}
	if redisDB, ok := lookup("REDIS_DB"); ok && redisDB != "" {
		db, err := strconv.Atoi(redisDB)
		if err != nil || db < 0 {
			return Config{}, fmt.Errorf("REDIS_DB must be a non-negative integer")
		}
		config.Redis.DB = db
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
	for env, target := range map[string]*string{
		"COS_SECRET_ID":  &config.COS.SecretID,
		"COS_SECRET_KEY": &config.COS.SecretKey,
		"COS_BUCKET":     &config.COS.Bucket,
		"COS_REGION":     &config.COS.Region,
		"COS_DOMAIN":     &config.COS.Domain,
	} {
		if value, ok := lookup(env); ok && value != "" {
			*target = value
		}
	}
	if err := config.validateCOS(); err != nil {
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

func (c GRPCClientTLSConfig) Validate(environmentPrefix string) error {
	configured := 0
	for _, value := range []string{c.CAFile, c.CertFile, c.KeyFile, c.ServerName} {
		if value != "" {
			configured++
		}
	}
	if configured != 0 && configured != 4 {
		return fmt.Errorf("%s_CA_FILE, %s_CERT_FILE, %s_KEY_FILE, and %s_SERVER_NAME must be set together", environmentPrefix, environmentPrefix, environmentPrefix, environmentPrefix)
	}
	return nil
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

func (c GRPCTLSConfig) Validate() error {
	configured := 0
	for _, value := range []bool{c.CertFile != "", c.KeyFile != "", c.ClientCAFile != "", len(c.AllowedClientIDs) > 0} {
		if value {
			configured++
		}
	}
	if configured == 0 {
		return nil
	}
	if configured != 4 {
		return fmt.Errorf("GRPC_TLS_CERT_FILE, GRPC_TLS_KEY_FILE, GRPC_TLS_CLIENT_CA_FILE, and GRPC_ALLOWED_CLIENT_IDS must be set together")
	}
	return nil
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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

func (c Config) validateCOS() error {
	v := c.COS
	required := []string{v.SecretID, v.SecretKey, v.Bucket, v.Region}
	configured := 0
	for _, value := range append(required, v.Domain) {
		if value != "" {
			configured++
		}
	}
	if configured == 0 {
		return nil
	}
	for _, value := range required {
		if value == "" {
			return fmt.Errorf("COS_SECRET_ID, COS_SECRET_KEY, COS_BUCKET, and COS_REGION must be set together")
		}
	}
	return nil
}
