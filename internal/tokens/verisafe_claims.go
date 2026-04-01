package tokens

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Claims structure for JWT
type VerisafeClaims struct {
	jwt.RegisteredClaims
}

// JTI extracts the jti claim as a uuid.UUID.
func (c *VerisafeClaims) JTI() (uuid.UUID, error) {
	jtiStr, ok := c.RegisteredClaims.ID, c.RegisteredClaims.ID != ""
	if !ok {
		return uuid.Nil, errors.New("jti claim is missing")
	}
	jti, err := uuid.Parse(jtiStr)
	if err != nil {
		return uuid.Nil, fmt.Errorf("jti is not a valid UUID: %w", err)
	}
	return jti, nil
}

// ValidateJWT parses and validates the JWT signature and expiry.
func ValidateJWT(tokenString string, secret string) (*VerisafeClaims, error) {
	token, err := jwt.ParseWithClaims(
		tokenString,
		&VerisafeClaims{},
		func(token *jwt.Token) (any, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, errors.New("unexpected signing method")
			}
			return []byte(secret), nil
		},
	)
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*VerisafeClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}

	if claims.ExpiresAt == nil {
		return nil, errors.New("token is missing expiry")
	}

	if claims.ExpiresAt.Time.Before(time.Now()) {
		return nil, errors.New("token has expired")
	}

	return claims, nil
}

// HashToken returns the SHA256 hash of the token as a base64 string.
// Use this for storing and comparing API keys and refresh tokens.
func HashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return base64.StdEncoding.EncodeToString(hash[:])
}
