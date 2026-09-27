package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	passwordMinRunes = 12
	passwordMaxRunes = 128
	passwordSaltSize = 16
	passwordHashSize = 32
	passwordMemory   = 64 * 1024
	passwordTime     = 3
	passwordThreads  = 2
)

func hashPassword(password string) (string, error) {
	salt := make([]byte, passwordSaltSize)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}

	hash := argon2.IDKey(
		[]byte(password), salt, passwordTime, passwordMemory,
		passwordThreads, passwordHashSize,
	)
	return "v1$" + base64.RawURLEncoding.EncodeToString(salt) + "$" +
		base64.RawURLEncoding.EncodeToString(hash), nil
}

func verifyPassword(password, encodedHash string) bool {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 3 || parts[0] != "v1" {
		burnPasswordHash(password)
		return false
	}

	salt, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(salt) != passwordSaltSize {
		burnPasswordHash(password)
		return false
	}

	expectedHash, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(expectedHash) != passwordHashSize {
		burnPasswordHash(password)
		return false
	}

	actualHash := argon2.IDKey(
		[]byte(password), salt, passwordTime, passwordMemory,
		passwordThreads, passwordHashSize,
	)
	return subtle.ConstantTimeCompare(actualHash, expectedHash) == 1
}

// burnPasswordHash performs the same expensive operation for an email that has
// no password credential, reducing timing differences that could reveal which
// accounts have enabled password login.
func burnPasswordHash(password string) {
	var salt [passwordSaltSize]byte
	argon2.IDKey(
		[]byte(password), salt[:], passwordTime, passwordMemory,
		passwordThreads, passwordHashSize,
	)
}

func validPasswordLength(password string) bool {
	if !utf8.ValidString(password) {
		return false
	}
	runeCount := utf8.RuneCountInString(password)
	return runeCount >= passwordMinRunes && runeCount <= passwordMaxRunes
}
