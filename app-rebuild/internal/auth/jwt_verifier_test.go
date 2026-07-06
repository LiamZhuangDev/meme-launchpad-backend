package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestJWTVerifierParsesValidToken(t *testing.T) {
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		UserID: 7, Address: "0x3333333333333333333333333333333333333333",
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))},
	}).SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatal(err)
	}

	claims, err := NewJWTVerifier("test-secret").ParseToken(raw)
	if err != nil {
		t.Fatalf("ParseToken() error = %v", err)
	}
	if claims.UserID != 7 || claims.Address != "0x3333333333333333333333333333333333333333" {
		t.Fatalf("claims = %+v", claims)
	}
}

func TestJWTVerifierRejectsWrongSecret(t *testing.T) {
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{}).SignedString([]byte("issuer-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewJWTVerifier("different-secret").ParseToken(raw); err == nil {
		t.Fatal("ParseToken() error = nil, want signature validation error")
	}
}
