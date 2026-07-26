// Package secrets provides authenticated encryption for credentials Verisafe
// must store in a recoverable form — principally third-party OAuth refresh
// tokens, which unlike Verisafe's own refresh tokens cannot be hashed because
// they have to be replayed to the provider.
//
// # Key versioning
//
// Keys are supplied as a versioned set ("1:<base64>,2:<base64>") with one
// version marked active. Ciphertext records the version it was sealed under,
// so old keys stay readable while new writes use the active one. Rotation is
// therefore lazy: add a key, bump the active version, and rows migrate as they
// are rewritten — no backfill job.
//
// # Binding
//
// Seal and Open take additional authenticated data (AAD). Callers pass a value
// derived from the row's identity (see GrantAAD), which means ciphertext lifted
// out of one row and pasted into another fails to open even though the key is
// correct. This is what stops an attacker with database write access from
// transplanting one user's sealed refresh token onto their own account.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// keyLength is the AES-256 key size. Anything else is rejected outright rather
// than silently stretched or truncated.
const keyLength = 32

// ErrNoCiphertext is returned by Open when handed an empty value — callers
// distinguish "no token stored" from "stored but undecryptable".
var ErrNoCiphertext = errors.New("secrets: no ciphertext to open")

// Sealer seals and opens values using AES-256-GCM under a versioned key set.
type Sealer struct {
	aeads     map[int16]cipher.AEAD
	activeVer int16
}

// ParseKeySpec decodes a "version:base64key" comma-separated key set.
// Exported so configuration can fail fast on a malformed value at startup
// rather than at the first token write.
func ParseKeySpec(spec string) (map[int16][]byte, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, errors.New("key spec is empty")
	}

	keys := make(map[int16][]byte)
	for _, entry := range strings.Split(spec, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		version, encoded, ok := strings.Cut(entry, ":")
		if !ok {
			return nil, fmt.Errorf(
				"entry %q is not in the form <version>:<base64-key>",
				entry,
			)
		}

		v, err := strconv.ParseInt(strings.TrimSpace(version), 10, 16)
		if err != nil {
			return nil, fmt.Errorf("entry %q has a non-numeric version", entry)
		}
		if v <= 0 {
			return nil, fmt.Errorf("entry %q has a non-positive version", entry)
		}
		if _, exists := keys[int16(v)]; exists {
			return nil, fmt.Errorf("version %d is defined more than once", v)
		}

		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
		if err != nil {
			return nil, fmt.Errorf(
				"key for version %d is not valid base64: %w",
				v,
				err,
			)
		}
		if len(raw) != keyLength {
			return nil, fmt.Errorf(
				"key for version %d is %d bytes, want %d",
				v,
				len(raw),
				keyLength,
			)
		}

		keys[int16(v)] = raw
	}

	if len(keys) == 0 {
		return nil, errors.New("key spec contains no usable keys")
	}
	return keys, nil
}

// NewSealer builds a Sealer from a key spec and the version new writes should
// use. Every supplied key is retained for decryption.
func NewSealer(spec string, activeVersion int) (*Sealer, error) {
	keys, err := ParseKeySpec(spec)
	if err != nil {
		return nil, err
	}

	if activeVersion <= 0 {
		return nil, errors.New("active key version must be positive")
	}
	if _, ok := keys[int16(activeVersion)]; !ok {
		return nil, fmt.Errorf(
			"active key version %d is not present in the key spec (have %v)",
			activeVersion,
			sortedVersions(keys),
		)
	}

	aeads := make(map[int16]cipher.AEAD, len(keys))
	for version, key := range keys {
		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, fmt.Errorf("version %d: %w", version, err)
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, fmt.Errorf("version %d: %w", version, err)
		}
		aeads[version] = aead
	}

	return &Sealer{aeads: aeads, activeVer: int16(activeVersion)}, nil
}

// ActiveVersion reports the key version new seals use.
func (s *Sealer) ActiveVersion() int16 { return s.activeVer }

// Seal encrypts plaintext under the active key, returning
// nonce || ciphertext || tag along with the version used. Sealing the same
// plaintext twice yields different ciphertext — the nonce is fresh per call.
func (s *Sealer) Seal(plaintext string, aad []byte) ([]byte, int16, error) {
	aead, ok := s.aeads[s.activeVer]
	if !ok {
		return nil, 0, fmt.Errorf(
			"no key for active version %d",
			s.activeVer,
		)
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, 0, fmt.Errorf("generate nonce: %w", err)
	}

	sealed := aead.Seal(nonce, nonce, []byte(plaintext), aad)
	return sealed, s.activeVer, nil
}

// Open decrypts a value produced by Seal. The aad must match byte-for-byte
// what was passed at seal time or authentication fails.
func (s *Sealer) Open(sealed []byte, version int16, aad []byte) (string, error) {
	if len(sealed) == 0 {
		return "", ErrNoCiphertext
	}

	aead, ok := s.aeads[version]
	if !ok {
		return "", fmt.Errorf(
			"no key for version %d (have %v) — was a key retired too early?",
			version,
			sortedVersions(s.aeads),
		)
	}

	if len(sealed) < aead.NonceSize() {
		return "", errors.New("ciphertext is shorter than the nonce")
	}

	nonce, body := sealed[:aead.NonceSize()], sealed[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, body, aad)
	if err != nil {
		return "", fmt.Errorf("authenticated decryption failed: %w", err)
	}
	return string(plaintext), nil
}

// GrantAAD binds an oauth_grants ciphertext to the row and column it belongs
// to. Changing any component makes existing ciphertext unopenable, which is
// the point: it is a transplant guard, not a secret.
func GrantAAD(accountID uuid.UUID, provider, field string) []byte {
	return []byte(fmt.Sprintf(
		"oauth_grant|%s|%s|%s",
		accountID.String(),
		strings.ToLower(provider),
		field,
	))
}

func sortedVersions[T any](m map[int16]T) []int16 {
	out := make([]int16, 0, len(m))
	for v := range m {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
