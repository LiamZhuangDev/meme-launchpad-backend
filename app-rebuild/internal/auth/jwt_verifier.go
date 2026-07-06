package auth

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"

	"github.com/golang-jwt/jwt/v5"
)

// JWTVerifier validates tokens issued by Service without depending on users,
// SIWE configuration, or challenge storage.
type JWTVerifier struct {
	publicKey ed25519.PublicKey
}

func NewJWTVerifier(publicKey ed25519.PublicKey) *JWTVerifier {
	return &JWTVerifier{publicKey: publicKey}
}

func (v *JWTVerifier) ParseToken(raw string) (Claims, error) {
	claims := Claims{}
	token, err := jwt.ParseWithClaims(raw, &claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodEdDSA {
			return nil, errors.New("unexpected signing method")
		}
		return v.publicKey, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}))
	if err != nil || !token.Valid {
		return Claims{}, errors.New("invalid or expired token")
	}
	return claims, nil
}

func LoadJWTPrivateKey(path string) (ed25519.PrivateKey, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read JWT private key: %w", err)
	}
	block, _ := pem.Decode(bytes)
	if block == nil {
		return nil, errors.New("JWT private key is not PEM encoded")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse JWT private key: %w", err)
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("JWT private key must be Ed25519")
	}
	return key, nil
}

func LoadJWTPublicKey(path string) (ed25519.PublicKey, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read JWT public key: %w", err)
	}
	block, _ := pem.Decode(bytes)
	if block == nil {
		return nil, errors.New("JWT public key is not PEM encoded")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse JWT public key: %w", err)
	}
	key, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("JWT public key must be Ed25519")
	}
	return key, nil
}
