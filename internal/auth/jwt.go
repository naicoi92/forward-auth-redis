package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWT signs and parses HS256 tokens. Tokens have no exp claim; validity is
// determined by the Redis session lifetime.
type JWT struct {
	secret []byte
}

// Claims is the JWT payload used by this service. It embeds
// jwt.RegisteredClaims so it satisfies the jwt.Claims interface and exposes
// Subject (sub) and IssuedAt (iat) while adding a custom SessionID (sid).
type Claims struct {
	Sid string `json:"sid"`
	jwt.RegisteredClaims
}

// NewJWT creates a JWT signer/verifier from the configured HMAC secret.
func NewJWT(secret string) *JWT {
	return &JWT{secret: []byte(secret)}
}

// Sign creates a new JWT for the given username and session ID.
func (j *JWT) Sign(username, sessionID string) (string, error) {
	claims := Claims{
		Sid: sessionID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:  username,
			IssuedAt: jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(j.secret)
}

// Parse validates a token string and returns its claims.
func (j *JWT) Parse(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return j.secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse jwt: %w", err)
	}
	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, fmt.Errorf("invalid claims type")
	}
	return claims, nil
}
