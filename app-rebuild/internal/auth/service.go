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
	value   string
	expires time.Time
}

// Service uses in-memory nonces until Step 9 replaces this implementation with Redis.
type Service struct {
	users  UserStore
	secret []byte
	nonces map[string]nonceEntry
	mu     sync.Mutex
}

func New(users UserStore, jwtSecret string) *Service {
	return &Service{users: users, secret: []byte(jwtSecret), nonces: make(map[string]nonceEntry)}
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
	expires := time.Now().Add(5 * time.Minute)
	s.mu.Lock()
	s.nonces[address] = nonceEntry{value: nonce, expires: expires}
	s.mu.Unlock()
	return SignMessage{Message: loginMessage(address, nonce), Nonce: nonce, Expires: expires.Unix()}, nil
}

func (s *Service) Login(ctx context.Context, address, signature string) (LoginResult, error) {
	address, err := normalizeAddress(address)
	if err != nil {
		return LoginResult{}, err
	}
	nonce, err := s.consumeNonce(address)
	if err != nil {
		return LoginResult{}, err
	}
	if !verifySignature(address, loginMessage(address, nonce), signature) {
		return LoginResult{}, ErrInvalidSignature
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

func (s *Service) consumeNonce(address string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.nonces[address]
	delete(s.nonces, address)
	if !ok || time.Now().After(entry.expires) {
		return "", ErrInvalidNonce
	}
	return entry.value, nil
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
	return strings.ToLower(address), nil
}
func loginMessage(address, nonce string) string {
	return fmt.Sprintf("MEME Launchpad sign-in\n\nWallet address: %s\nNonce: %s", address, nonce)
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
