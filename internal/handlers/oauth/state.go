package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/opencrafts-io/verisafe/internal/core"
)

const scopeUpgradeStatePrefix = "oauth_scope_upgrade:"

// scopeUpgradeState is everything the callback needs, held server-side.
//
// The login flow encodes its state as a pipe-delimited, base64'd blob of
// client-supplied values with no escaping and no integrity protection (see
// encodeState in internal/auth) — a documented limitation that also makes it
// replayable. Rather than extend that, this flow stores the state in Redis and
// puts only an opaque handle on the wire.
//
// That fixes the whole class structurally: 256 bits of entropy makes the
// handle unguessable, nothing user-controlled is in the state to be injected
// into, reading deletes it so a captured callback URL cannot be replayed, and
// there is no delimiter to escape.
type scopeUpgradeState struct {
	AccountID       uuid.UUID `json:"account_id"`
	Provider        string    `json:"provider"`
	Capabilities    []string  `json:"capabilities"`
	RequestedScopes []string  `json:"requested_scopes"`
	// ExpectedExternalUserID pins the provider account this upgrade may touch.
	// Empty when the account has no existing grant for the provider.
	ExpectedExternalUserID string    `json:"expected_external_user_id"`
	Platform               string    `json:"platform"`
	RedirectURI            string    `json:"redirect_uri"`
	DeepLink               string    `json:"deep_link"`
	PKCEVerifier           string    `json:"pkce_verifier"`
	CreatedAt              time.Time `json:"created_at"`
}

// errStateNotFound covers a handle that is unknown, already used, or expired.
// The three are deliberately indistinguishable to the caller.
var errStateNotFound = errors.New("state is invalid or has expired")

// newStateHandle mints the opaque value that travels as the state parameter.
func newStateHandle() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate state handle: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// stateKey hashes the handle before it becomes a Redis key, so a dump of the
// keyspace does not yield usable state handles.
func stateKey(handle string) string {
	sum := sha256.Sum256([]byte(handle))
	return scopeUpgradeStatePrefix + base64.RawURLEncoding.EncodeToString(sum[:])
}

// putState stores state under a handle for the configured TTL.
func (h *OAuthScopeHandler) putState(
	ctx context.Context,
	handle string,
	state scopeUpgradeState,
) error {
	return h.Cacher.Set(ctx, stateKey(handle), state, h.Cfg.ScopeUpgradeStateTTL())
}

// takeState reads and immediately consumes a state handle. Single use is what
// makes a captured callback URL worthless on replay.
func (h *OAuthScopeHandler) takeState(
	ctx context.Context,
	handle string,
) (*scopeUpgradeState, error) {
	if handle == "" {
		return nil, errStateNotFound
	}

	key := stateKey(handle)

	var state scopeUpgradeState
	if err := h.Cacher.Get(ctx, key, &state); err != nil {
		if errors.Is(err, core.ErrCacheMiss) {
			return nil, errStateNotFound
		}
		return nil, fmt.Errorf("read scope upgrade state: %w", err)
	}

	if err := h.Cacher.Delete(ctx, key); err != nil {
		// Worth knowing about: a state that survives its use is replayable
		// until the TTL expires.
		h.Logger.Warn("failed to consume scope upgrade state", "error", err)
	}

	return &state, nil
}

// pkcePair generates a PKCE verifier and its S256 challenge, so an
// authorization code intercepted in transit cannot be exchanged without the
// verifier that never left this process.
func pkcePair() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate pkce verifier: %w", err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)

	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}
