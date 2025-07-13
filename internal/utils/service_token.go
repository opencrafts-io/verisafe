package utils

import (
	"crypto/sha256"
	"encoding/base64"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

func GenerateServiceToken(
	account repository.Account,
	cfg *config.Config,
) (string, error) {

	claims :=
		&VerisafeClaims{
			Account:     account,
			Roles:       []repository.UserRolesView{},
			Permissions: []string{},
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour * 24)),
				Audience:  jwt.ClaimStrings{"https://academia.opencrafts.io/"},
				Issuer:    "https://verisafe.opencrafts.io/",
				Subject:   account.ID.String(),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
			},
		}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(cfg.JWTConfig.ApiSecret))
}

// HashToken returns the SHA256 hash of the token as base64 string
func HashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return base64.StdEncoding.EncodeToString(hash[:])
}
