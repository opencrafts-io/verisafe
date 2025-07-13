package utils

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

func GenerateServiceToken(
	account repository.Account,
	roles []repository.UserRolesView,
	permissions []repository.UserPermissionsView,
	cfg *config.Config,
) (string, error) {

	var perms []string

	for _, p := range permissions {
		perms = append(perms, p.Permission)
	}

	claims :=
		&VerisafeClaims{
			Account:     account,
			Roles:       roles,
			Permissions: perms,
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
