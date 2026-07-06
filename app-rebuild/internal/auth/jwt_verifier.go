package auth

import (
	"errors"

	"github.com/golang-jwt/jwt/v5"
)

// JWTVerifier validates tokens issued by Service without depending on users,
// SIWE configuration, or challenge storage.
type JWTVerifier struct {
	secret []byte
}

func NewJWTVerifier(secret string) *JWTVerifier {
	return &JWTVerifier{secret: []byte(secret)}
}

func (v *JWTVerifier) ParseToken(raw string) (Claims, error) {
	claims := Claims{}
	token, err := jwt.ParseWithClaims(raw, &claims, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return v.secret, nil
	})
	if err != nil || !token.Valid {
		return Claims{}, errors.New("invalid or expired token")
	}
	return claims, nil
}
