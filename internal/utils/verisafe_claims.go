package utils

import (
	"github.com/golang-jwt/jwt/v5"
)

// Claims structure for JWT
type VerisafeClaims struct {
	jwt.RegisteredClaims
}
