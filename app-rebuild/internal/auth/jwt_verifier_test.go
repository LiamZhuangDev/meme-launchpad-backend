package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestJWTVerifierParsesValidToken(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, Claims{
		UserID: 7, Address: "0x3333333333333333333333333333333333333333",
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))},
	}).SignedString(privateKey)
	if err != nil {
		t.Fatal(err)
	}

	claims, err := NewJWTVerifier(publicKey).ParseToken(raw)
	if err != nil {
		t.Fatalf("ParseToken() error = %v", err)
	}
	if claims.UserID != 7 || claims.Address != "0x3333333333333333333333333333333333333333" {
		t.Fatalf("claims = %+v", claims)
	}
}

func TestJWTVerifierRejectsWrongPublicKey(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	otherPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, Claims{}).SignedString(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewJWTVerifier(otherPublicKey).ParseToken(raw); err == nil {
		t.Fatal("ParseToken() error = nil, want signature validation error")
	}
}

func TestJWTVerifierRejectsSymmetricAlgorithm(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{}).SignedString([]byte("old-shared-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewJWTVerifier(publicKey).ParseToken(raw); err == nil {
		t.Fatal("ParseToken() error = nil, want non-EdDSA algorithm rejection")
	}
}

func TestLoadJWTKeysFromPEM(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	privatePath := filepath.Join(directory, "private.pem")
	publicPath := filepath.Join(directory, "public.pem")
	if err := os.WriteFile(privatePath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(publicPath, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}), 0o644); err != nil {
		t.Fatal(err)
	}
	loadedPrivate, err := LoadJWTPrivateKey(privatePath)
	if err != nil {
		t.Fatal(err)
	}
	loadedPublic, err := LoadJWTPublicKey(publicPath)
	if err != nil {
		t.Fatal(err)
	}
	if !loadedPrivate.Equal(privateKey) || !loadedPublic.Equal(publicKey) {
		t.Fatal("loaded JWT keys do not match generated keys")
	}
}

func testJWTPrivateKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return privateKey
}
