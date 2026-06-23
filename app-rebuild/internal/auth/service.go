// Package auth implements wallet login and JWT authentication.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/meme-launchpad/app-rebuild/internal/repository"
)

var (
	ErrInvalidAddress   = errors.New("invalid wallet address")
	ErrInvalidSignature = errors.New("invalid wallet signature")
	ErrInvalidNonce     = errors.New("nonce is missing, expired, or already used")
)

type UserStore interface {
	FindByAddress(context.Context, string) (repository.User, error)
	Create(context.Context, string, string) (repository.User, error)
}

type Claims struct {
	UserID  int64  `json:"userId"`
	Address string `json:"address"`
	jwt.RegisteredClaims
}

type SignMessage struct {
	Message string `json:"message"`
	Nonce   string `json:"nonce"`
	Expires int64  `json:"expires"`
}

type LoginResult struct {
	Token   string          `json:"token"`
	User    repository.User `json:"user"`
	Expires int64           `json:"expiresIn"`
}

type nonceEntry struct {
	message string
	expires time.Time
}

type SIWEConfig struct {
	Domain  string
	URI     string
	ChainID int64
}

// Service uses in-memory nonces until Step 9 replaces this implementation with Redis.
type Service struct {
	users  UserStore
	secret []byte
	siwe   SIWEConfig
	nonces map[string]nonceEntry
	mu     sync.Mutex
}

func New(users UserStore, jwtSecret string, siwe SIWEConfig) *Service {
	return &Service{users: users, secret: []byte(jwtSecret), siwe: siwe, nonces: make(map[string]nonceEntry)}
}

func (s *Service) RequestMessage(address string) (SignMessage, error) {
	address, err := normalizeAddress(address)
	if err != nil {
		return SignMessage{}, err
	}
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return SignMessage{}, fmt.Errorf("generate nonce: %w", err)
	}
	nonce := hex.EncodeToString(nonceBytes)
	issuedAt := time.Now().UTC()
	expires := issuedAt.Add(5 * time.Minute)
	message := s.siweMessage(address, nonce, issuedAt, expires)
	s.mu.Lock()
	s.nonces[address] = nonceEntry{message: message, expires: expires}
	s.mu.Unlock()
	return SignMessage{Message: message, Nonce: nonce, Expires: expires.Unix()}, nil
}

func (s *Service) Login(ctx context.Context, address, signature string) (LoginResult, error) {
	address, err := normalizeAddress(address)
	if err != nil {
		return LoginResult{}, err
	}
	entry, err := s.getNonce(address)
	if err != nil {
		return LoginResult{}, err
	}
	if !verifySignature(address, entry.message, signature) {
		return LoginResult{}, ErrInvalidSignature
	}
	if err := s.consumeNonce(address); err != nil {
		return LoginResult{}, err
	}
	user, err := s.users.FindByAddress(ctx, address)
	if errors.Is(err, pgx.ErrNoRows) {
		user, err = s.users.Create(ctx, address, shortenAddress(address))
	}
	if err != nil {
		return LoginResult{}, fmt.Errorf("find or create user: %w", err)
	}
	token, expires, err := s.issueToken(user)
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{Token: token, User: user, Expires: expires}, nil
}

func (s *Service) ParseToken(raw string) (Claims, error) {
	claims := Claims{}
	token, err := jwt.ParseWithClaims(raw, &claims, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return s.secret, nil
	})
	if err != nil || !token.Valid {
		return Claims{}, errors.New("invalid or expired token")
	}
	return claims, nil
}

func (s *Service) getNonce(address string) (nonceEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.nonces[address]
	if !ok || time.Now().After(entry.expires) {
		return nonceEntry{}, ErrInvalidNonce
	}
	return entry, nil
}

func (s *Service) consumeNonce(address string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.nonces[address]
	if !ok || time.Now().After(entry.expires) {
		return ErrInvalidNonce
	}
	delete(s.nonces, address)
	return nil
}

func (s *Service) issueToken(user repository.User) (string, int64, error) {
	expires := time.Now().Add(24 * time.Hour)
	claims := Claims{UserID: user.ID, Address: user.Address, RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(expires), IssuedAt: jwt.NewNumericDate(time.Now())}}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	return token, expires.Unix(), err
}

func normalizeAddress(address string) (string, error) {
	if !common.IsHexAddress(address) {
		return "", ErrInvalidAddress
	}
	return common.HexToAddress(address).Hex(), nil
}
func (s *Service) siweMessage(address, nonce string, issuedAt, expires time.Time) string {
	return fmt.Sprintf("%s wants you to sign in with your Ethereum account:\n%s\n\nSign in to MEME Launchpad.\n\nURI: %s\nVersion: 1\nChain ID: %d\nNonce: %s\nIssued At: %s\nExpiration Time: %s",
		s.siwe.Domain, address, s.siwe.URI, s.siwe.ChainID, nonce,
		issuedAt.Format(time.RFC3339), expires.Format(time.RFC3339))
}
func shortenAddress(address string) string { return address[:6] + "..." + address[len(address)-4:] }
func verifySignature(address, message, signature string) bool {
	sig, err := hexutil.Decode(signature)
	if err != nil || len(sig) != 65 {
		return false
	}
	if sig[64] >= 27 {
		sig[64] -= 27
	}
	if sig[64] > 1 {
		return false
	}
	prefix := fmt.Sprintf("\x19Ethereum Signed Message:\n%d", len(message))
	publicKey, err := ethcrypto.SigToPub(ethcrypto.Keccak256Hash([]byte(prefix+message)).Bytes(), sig)
	return err == nil && strings.EqualFold(ethcrypto.PubkeyToAddress(*publicKey).Hex(), address)
}
