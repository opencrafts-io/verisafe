package utils

import (
	"github.com/golang-jwt/jwt/v5"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

// Claims structure for JWT
type VerisafeClaims struct {
	Account     repository.Account         `json:"user"`
	Roles       []repository.UserRolesView `json:"roles"`
	Permissions []string                   `json:"permissions"`
	jwt.RegisteredClaims
}
