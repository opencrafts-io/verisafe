package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

// Claims structure for JWT
type VerisafeClaims struct {
	Account     repository.Account `json:"user"`
	Roles       []string           `json:"roles"`
	Permissions []string           `json:"permissions"`
	jwt.RegisteredClaims
}

// GenerateJWT creates a new token for a given user ID.
func GenerateJWT(
	account repository.Account,
	roles []string,
	permissions []string,
	cfg config.Config,
) (string, error) {

	claims :=
		&VerisafeClaims{
			Account:     account,
			Roles:       roles,
			Permissions: permissions,
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour * time.Duration(cfg.JWTConfig.ExpireDelta))),
				Audience:  jwt.ClaimStrings{"https://academia.opencrafts.io/"},
				Issuer:    "https://verisafe.opencrafts.io/",
				Subject:   account.ID.String(),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
			},
		}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(cfg.JWTConfig.ApiSecret))
}

// ValidateJWT parses and validates the JWT token and checks expiration.
func ValidateJWT(tokenString string, secret string) (*VerisafeClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &VerisafeClaims{}, func(token *jwt.Token) (interface{}, error) {
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
