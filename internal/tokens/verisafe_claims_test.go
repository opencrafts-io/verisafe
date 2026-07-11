package tokens

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSecret = "test-secret-for-verisafe-claims"

func signHS256(t *testing.T, claims *VerisafeClaims, secret string) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	require.NoError(t, err)
	return signed
}

func validClaims() *VerisafeClaims {
	return &VerisafeClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.NewString(),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
}

func TestVerisafeClaims_JTI(t *testing.T) {
	t.Run("valid jti", func(t *testing.T) {
		want := uuid.New()
		claims := &VerisafeClaims{
			RegisteredClaims: jwt.RegisteredClaims{ID: want.String()},
		}
		got, err := claims.JTI()
		assert.NoError(t, err)
		assert.Equal(t, want, got)
	})

	t.Run("missing jti", func(t *testing.T) {
		claims := &VerisafeClaims{}
		_, err := claims.JTI()
		assert.ErrorContains(t, err, "jti claim is missing")
	})

	t.Run("malformed jti", func(t *testing.T) {
		claims := &VerisafeClaims{
			RegisteredClaims: jwt.RegisteredClaims{ID: "not-a-uuid"},
		}
		_, err := claims.JTI()
		assert.ErrorContains(t, err, "not a valid UUID")
	})
}

func TestValidateJWT(t *testing.T) {
	t.Run("valid token", func(t *testing.T) {
		want := validClaims()
		signed := signHS256(t, want, testSecret)

		got, err := ValidateJWT(signed, testSecret)
		require.NoError(t, err)
		assert.Equal(t, want.ID, got.ID)
	})

	t.Run("expired token is rejected", func(t *testing.T) {
		claims := &VerisafeClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				ID:        uuid.NewString(),
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
			},
		}
		signed := signHS256(t, claims, testSecret)

		_, err := ValidateJWT(signed, testSecret)
		assert.Error(t, err)
	})

	t.Run("missing expiry is rejected", func(t *testing.T) {
		claims := &VerisafeClaims{
			RegisteredClaims: jwt.RegisteredClaims{ID: uuid.NewString()},
		}
		signed := signHS256(t, claims, testSecret)

		_, err := ValidateJWT(signed, testSecret)
		assert.ErrorContains(t, err, "missing expiry")
	})

	t.Run("wrong secret is rejected", func(t *testing.T) {
		signed := signHS256(t, validClaims(), testSecret)

		_, err := ValidateJWT(signed, "a-completely-different-secret")
		assert.Error(t, err)
	})

	t.Run("non-HMAC signing method is rejected", func(t *testing.T) {
		rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)

		token := jwt.NewWithClaims(jwt.SigningMethodRS256, validClaims())
		signed, err := token.SignedString(rsaKey)
		require.NoError(t, err)

		_, err = ValidateJWT(signed, testSecret)
		assert.ErrorContains(t, err, "unexpected signing method")
	})
}

func TestHashToken(t *testing.T) {
	t.Run("deterministic", func(t *testing.T) {
		assert.Equal(t, HashToken("same-input"), HashToken("same-input"))
	})

	t.Run("different inputs hash differently", func(t *testing.T) {
		assert.NotEqual(t, HashToken("input-a"), HashToken("input-b"))
	})

	t.Run("not the raw input", func(t *testing.T) {
		assert.NotEqual(t, "raw-token-value", HashToken("raw-token-value"))
	})
}
