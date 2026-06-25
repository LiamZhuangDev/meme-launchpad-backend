// Package auth implements wallet login and JWT authentication.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
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
	ErrInvalidSIWE      = errors.New("invalid SIWE challenge")
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

type SIWEConfig struct {
	Domain  string
	URI     string
	ChainID int64
}

type ChallengeStore interface {
	Save(context.Context, string, SIWEChallenge, time.Duration) error
	Get(context.Context, string) (SIWEChallenge, error)
	Delete(context.Context, string) error
}

// SIWEChallenge is the structured server-issued authentication request.
// Message returns its canonical EIP-4361 plaintext representation.
type SIWEChallenge struct {
	Domain         string
	Address        string
	Statement      string
	URI            string
	Version        string
	ChainID        int64
	Nonce          string
	IssuedAt       time.Time
	ExpirationTime time.Time
}

func (c SIWEChallenge) Message() string {
	return fmt.Sprintf("%s wants you to sign in with your Ethereum account:\n%s\n\n%s\n\nURI: %s\nVersion: %s\nChain ID: %d\nNonce: %s\nIssued At: %s\nExpiration Time: %s",
		c.Domain, c.Address, c.Statement, c.URI, c.Version, c.ChainID, c.Nonce,
		c.IssuedAt.UTC().Format(time.RFC3339), c.ExpirationTime.UTC().Format(time.RFC3339))
}

type Service struct {
	users  UserStore
	secret []byte
	siwe   SIWEConfig
	store  ChallengeStore
}

func New(users UserStore, jwtSecret string, siwe SIWEConfig) *Service {
	return NewWithChallengeStore(users, jwtSecret, siwe, NewMemoryChallengeStore())
}

func NewWithChallengeStore(users UserStore, jwtSecret string, siwe SIWEConfig, store ChallengeStore) *Service {
	if store == nil {
		store = NewMemoryChallengeStore()
	}
	return &Service{users: users, secret: []byte(jwtSecret), siwe: siwe, store: store}
}

func (s *Service) RequestMessage(ctx context.Context, address string) (SignMessage, error) {
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
	challenge := SIWEChallenge{
		Domain: s.siwe.Domain, Address: address, Statement: "Sign in to MEME Launchpad.",
		URI: s.siwe.URI, Version: "1", ChainID: s.siwe.ChainID, Nonce: nonce,
		IssuedAt: issuedAt, ExpirationTime: expires,
	}
	if err := s.store.Save(ctx, address, challenge, time.Until(expires)); err != nil {
		return SignMessage{}, fmt.Errorf("store SIWE challenge: %w", err)
	}
	return SignMessage{Message: challenge.Message(), Nonce: nonce, Expires: expires.Unix()}, nil
}

func (s *Service) Login(ctx context.Context, address, signature string) (LoginResult, error) {
	address, err := normalizeAddress(address)
	if err != nil {
		return LoginResult{}, err
	}
	entry, err := s.getChallenge(ctx, address)
	if err != nil {
		return LoginResult{}, err
	}
	if err := s.validateChallenge(entry, address); err != nil {
		return LoginResult{}, err
	}
	if !verifySignature(address, entry.Message(), signature) {
		return LoginResult{}, ErrInvalidSignature
	}
	if err := s.consumeNonce(ctx, address); err != nil {
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

func (s *Service) getChallenge(ctx context.Context, address string) (SIWEChallenge, error) {
	entry, err := s.store.Get(ctx, address)
	if err != nil || time.Now().After(entry.ExpirationTime) {
		return SIWEChallenge{}, ErrInvalidNonce
	}
	return entry, nil
}

func (s *Service) consumeNonce(ctx context.Context, address string) error {
	return s.store.Delete(ctx, address)
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

func (s *Service) validateChallenge(challenge SIWEChallenge, address string) error {
	if challenge.Domain != s.siwe.Domain || challenge.URI != s.siwe.URI || challenge.ChainID != s.siwe.ChainID || challenge.Version != "1" || challenge.Address != address {
		return ErrInvalidSIWE
	}
	if challenge.Statement == "" || strings.Contains(challenge.Statement, "\n") || !validNonce(challenge.Nonce) {
		return ErrInvalidSIWE
	}
	uri, err := url.ParseRequestURI(challenge.URI)
	if err != nil || !uri.IsAbs() || uri.Host == "" || strings.ContainsAny(challenge.Domain, " \t\n") {
		return ErrInvalidSIWE
	}
	now := time.Now().UTC()
	if challenge.IssuedAt.After(now.Add(time.Minute)) || !challenge.ExpirationTime.After(challenge.IssuedAt) || !now.Before(challenge.ExpirationTime) {
		return ErrInvalidSIWE
	}
	return nil
}

func validNonce(nonce string) bool {
	if len(nonce) < 8 {
		return false
	}
	for _, char := range nonce {
		if !(char >= 'a' && char <= 'z') && !(char >= 'A' && char <= 'Z') && !(char >= '0' && char <= '9') {
			return false
		}
	}
	return true
}

func shortenAddress(address string) string { return address[:6] + "..." + address[len(address)-4:] }

// It checks whether the given Ethereum address actually signed the provided message using the supplied signature.
// Therefore the signer controls the private key for that Ethereum address.
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
