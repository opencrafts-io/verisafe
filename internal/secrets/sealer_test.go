package secrets

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func key(b byte) string {
	raw := make([]byte, keyLength)
	for i := range raw {
		raw[i] = b
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func singleKeySpec() string { return "1:" + key(0xAA) }

func twoKeySpec() string { return "1:" + key(0xAA) + ",2:" + key(0xBB) }

func TestParseKeySpec_Rejects(t *testing.T) {
	cases := []struct {
		name    string
		spec    string
		wantSub string
	}{
		{"empty", "", "empty"},
		{"missing colon", key(0x01), "<version>:<base64-key>"},
		{"non numeric version", "v1:" + key(0x01), "non-numeric version"},
		{"zero version", "0:" + key(0x01), "non-positive version"},
		{"negative version", "-1:" + key(0x01), "non-positive version"},
		{"duplicate version", "1:" + key(0x01) + ",1:" + key(0x02), "more than once"},
		{"bad base64", "1:not-base64!!", "not valid base64"},
		{
			"wrong length",
			"1:" + base64.StdEncoding.EncodeToString([]byte("short")),
			"bytes, want 32",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseKeySpec(tc.spec)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantSub)
		})
	}
}

func TestParseKeySpec_Accepts(t *testing.T) {
	keys, err := ParseKeySpec(twoKeySpec())
	require.NoError(t, err)
	assert.Len(t, keys, 2)
	assert.Len(t, keys[1], keyLength)
	assert.Len(t, keys[2], keyLength)
}

func TestNewSealer_ActiveVersionMustExist(t *testing.T) {
	_, err := NewSealer(singleKeySpec(), 2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "active key version 2 is not present")

	_, err = NewSealer(singleKeySpec(), 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be positive")
}

func TestSealOpen_RoundTrip(t *testing.T) {
	s, err := NewSealer(singleKeySpec(), 1)
	require.NoError(t, err)

	aad := GrantAAD(uuid.New(), "google", "refresh_token")
	sealed, version, err := s.Seal("1//0gK9-secret-refresh-token", aad)
	require.NoError(t, err)
	assert.Equal(t, int16(1), version)

	got, err := s.Open(sealed, version, aad)
	require.NoError(t, err)
	assert.Equal(t, "1//0gK9-secret-refresh-token", got)
}

func TestSeal_NonceIsFreshPerCall(t *testing.T) {
	s, err := NewSealer(singleKeySpec(), 1)
	require.NoError(t, err)

	aad := GrantAAD(uuid.New(), "google", "refresh_token")
	first, _, err := s.Seal("same plaintext", aad)
	require.NoError(t, err)
	second, _, err := s.Seal("same plaintext", aad)
	require.NoError(t, err)

	assert.NotEqual(
		t, first, second,
		"sealing identical plaintext twice must not produce identical ciphertext",
	)
}

func TestOpen_WrongAADFails(t *testing.T) {
	s, err := NewSealer(singleKeySpec(), 1)
	require.NoError(t, err)

	accountA, accountB := uuid.New(), uuid.New()
	sealed, version, err := s.Seal("token", GrantAAD(accountA, "google", "refresh_token"))
	require.NoError(t, err)

	// The transplant attempt: same key, same ciphertext, different row.
	_, err = s.Open(sealed, version, GrantAAD(accountB, "google", "refresh_token"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authenticated decryption failed")

	// Same account, different column.
	_, err = s.Open(sealed, version, GrantAAD(accountA, "google", "access_token"))
	require.Error(t, err)

	// Same account, different provider.
	_, err = s.Open(sealed, version, GrantAAD(accountA, "spotify", "refresh_token"))
	require.Error(t, err)
}

func TestOpen_TamperedCiphertextFails(t *testing.T) {
	s, err := NewSealer(singleKeySpec(), 1)
	require.NoError(t, err)

	aad := GrantAAD(uuid.New(), "google", "refresh_token")
	sealed, version, err := s.Seal("token", aad)
	require.NoError(t, err)

	tampered := make([]byte, len(sealed))
	copy(tampered, sealed)
	tampered[len(tampered)-1] ^= 0x01

	_, err = s.Open(tampered, version, aad)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authenticated decryption failed")
}

func TestOpen_UnknownVersionIsDiagnosable(t *testing.T) {
	s, err := NewSealer(singleKeySpec(), 1)
	require.NoError(t, err)

	aad := GrantAAD(uuid.New(), "google", "refresh_token")
	sealed, _, err := s.Seal("token", aad)
	require.NoError(t, err)

	_, err = s.Open(sealed, 7, aad)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no key for version 7")
	assert.Contains(t, err.Error(), "retired too early")
}

func TestOpen_EmptyCiphertext(t *testing.T) {
	s, err := NewSealer(singleKeySpec(), 1)
	require.NoError(t, err)

	_, err = s.Open(nil, 1, nil)
	assert.ErrorIs(t, err, ErrNoCiphertext)
}

func TestOpen_ShortCiphertext(t *testing.T) {
	s, err := NewSealer(singleKeySpec(), 1)
	require.NoError(t, err)

	_, err = s.Open([]byte{0x01, 0x02}, 1, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shorter than the nonce")
}

// Key rotation is the reason versions exist: a value sealed under v1 must stay
// readable after the active version moves to v2, and new seals must use v2.
func TestKeyRotation_OldCiphertextStaysReadable(t *testing.T) {
	aad := GrantAAD(uuid.New(), "google", "refresh_token")

	v1Only, err := NewSealer(singleKeySpec(), 1)
	require.NoError(t, err)
	sealedUnderV1, version, err := v1Only.Seal("legacy token", aad)
	require.NoError(t, err)
	require.Equal(t, int16(1), version)

	rotated, err := NewSealer(twoKeySpec(), 2)
	require.NoError(t, err)
	assert.Equal(t, int16(2), rotated.ActiveVersion())

	got, err := rotated.Open(sealedUnderV1, 1, aad)
	require.NoError(t, err)
	assert.Equal(t, "legacy token", got)

	_, newVersion, err := rotated.Seal("fresh token", aad)
	require.NoError(t, err)
	assert.Equal(
		t, int16(2), newVersion,
		"new seals must use the active version so rotation actually progresses",
	)
}

func TestGrantAAD_IsStableAndCaseInsensitiveOnProvider(t *testing.T) {
	id := uuid.New()
	assert.Equal(
		t,
		GrantAAD(id, "google", "refresh_token"),
		GrantAAD(id, "GOOGLE", "refresh_token"),
	)
	assert.True(t, strings.HasPrefix(
		string(GrantAAD(id, "google", "refresh_token")),
		"oauth_grant|"+id.String()+"|google|",
	))
}
