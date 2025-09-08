package utils

import (
	"errors"
	"time"

	"crypto/sha256"
	"encoding/base64"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/opencrafts-io/verisafe/internal/config"
)

type VerisafeTokenType int

const (
	UserToken VerisafeTokenType = iota
	UserRefreshToken
	ServiceToken
)

// HashToken returns the SHA256 hash of the token as base64 string
func HashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return base64.StdEncoding.EncodeToString(hash[:])
}

// GenerateJWT creates a new token for a given user ID.
// Provide an optional token type although by default its goin
// to generate a basic user token
func GenerateJWT(
	subject uuid.UUID,
	cfg config.Config,
	tokenTypeOptional ...VerisafeTokenType,
) (string, error) {

	tokenType := UserToken

	if len(tokenTypeOptional) > 0 {
		tokenType = tokenTypeOptional[0]
	}

	var expiry time.Time

	switch tokenType {
	case UserToken:
		expiry = time.Now().Add(time.Hour * 24 * time.Duration(cfg.JWTConfig.ExpireDelta))
	case UserRefreshToken:
		expiry = time.Now().Add(time.Hour * 24 * time.Duration(cfg.JWTConfig.RefreshExpireDelta))
	case ServiceToken:
		expiry = time.Now().Add(time.Hour * 24 * time.Duration(cfg.JWTConfig.RefreshExpireDelta))
	}

	claims :=
		&VerisafeClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(expiry),
				Audience:  jwt.ClaimStrings{"https://academia.opencrafts.io/"},
				Issuer:    "https://verisafe.opencrafts.io/",
				Subject:   subject.String(),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
			},
		}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(cfg.JWTConfig.ApiSecret))
}

// ValidateJWT parses and validates the JWT token and checks expiration.
func ValidateJWT(tokenString string, secret string) (*VerisafeClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &VerisafeClaims{}, func(token *jwt.Token) (any, error) {
		// Ensure the token is signed with the expected method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	})

	if err != nil {
		return nil, err
	}

	// Extract and validate the claims
	claims, ok := token.Claims.(*VerisafeClaims)
	if !ok || !token.Valid {
		return nil, errors.New("Invalid token you have. Create a valid one you must!")
	}

	if claims.RegisteredClaims.ExpiresAt == nil {
		return nil, errors.New("Seems your access token is malformed please relogin to continue")
	}

	// Check if the token is expired
	if claims.RegisteredClaims.ExpiresAt.Time.Before(time.Now()) {
		return nil, errors.New("Your token expired it is. Refresh it you must")
	}

	return claims, nil
}
