// Package tokens provides the token issuance, rotation, and revocation logic
// for the Verisafe authentication service.
//
// # Overview
//
// Verisafe uses a two-token strategy:
//
//   - Access Token: A short-lived signed JWT (30 minutes) carrying a unique jti
//     (JWT ID) claim. Stateless by design — validated by signature alone. Revocation
//     is handled via a Redis blocklist keyed on the jti, with a TTL matching the
//     token's remaining lifetime.
//
//   - Refresh Token: A long-lived (90 days) cryptographically random opaque token.
//     Never stored in plain text — only its SHA-256 hash is persisted in the database.
//     Fully rotated on every use: each call to RotateRefreshToken invalidates the
//     current token and issues a new pair.
//
// # Refresh Token Families
//
// Refresh tokens are grouped by a family_id. If a token that has already been used
// is presented again (replay attack), the entire family is immediately revoked and
// the user is forced to re-authenticate. This detects token theft scenarios where
// an attacker replays a stolen refresh token after the legitimate client has
// already rotated it.
//
// # Device Association
//
// Both access and refresh tokens are tied to a specific device (user_devices record).
// This allows per-device revocation, audit trails, and push notification targeting.
//
// # Typical Flow
//
//  1. OAuth callback completes → call IssueTokenPair(userID, deviceID)
//  2. Return TokenPair to client (access token + raw refresh token)
//  3. Client stores refresh token securely (iOS Keychain / Android Keystore)
//  4. On 401 → client calls /auth/token/refresh → server calls RotateRefreshToken
//  5. On logout → server calls RevokeAccessToken (blocklist jti) + revokes refresh token
package tokens

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// TokenPair holds the result of a successful token issuance or rotation.
// The RawRefreshToken must be sent to the client exactly once and never stored.
// Only its SHA-256 hash is persisted in the database.
type TokenPair struct {
	// AccessToken is a signed JWT valid for 30 minutes.
	// Contains claims: jti, sub (userID), iss, aud, iat, exp.
	AccessToken string

	// RawRefreshToken is a cryptographically random opaque token valid for 90 days.
	// Send this to the client — never store it. Store only its hash.
	RawRefreshToken string

	// AccessExpiresAt is the absolute expiry time of the access token.
	AccessExpiresAt time.Time

	// RefreshExpiresAt is the absolute expiry time of the refresh token.
	RefreshExpiresAt time.Time
}

// TokenService manages the full lifecycle of access and refresh tokens.
// Implementations must be safe for concurrent use.
type TokenService interface {
	// IssueTokenPair generates a new JWT access token and opaque refresh token
	// for the given user and device. The refresh token is stored (hashed) in the
	// database and associated with a new family_id for reuse detection.
	//
	// Call this after a successful OAuth callback or initial login.
	IssueTokenPair(
		ctx context.Context,
		userID, deviceID uuid.UUID,
	) (*TokenPair, error)

	// RotateRefreshToken validates the incoming raw refresh token, marks it as used,
	// and issues a new TokenPair. Full rotation happens on every call.
	//
	// If the token has already been used (replay detected), the entire token family
	// is revoked and an error is returned — forcing the user to re-authenticate.
	//
	// Returns an error if the token is expired, revoked, or not found.
	RotateRefreshToken(
		ctx context.Context,
		rawRefreshToken string,
	) (*TokenPair, error)

	// RevokeFamily revokes all refresh tokens belonging to the given family.
	// This is called automatically by RotateRefreshToken on reuse detection,
	// but can also be called explicitly (e.g. on suspicious activity or logout
	// from all devices).
	RevokeFamily(ctx context.Context, familyID uuid.UUID) error

	// RevokeAccessToken adds the given jti to the Redis blocklist with the
	// provided TTL. The TTL should match the token's remaining lifetime to
	// avoid storing stale entries beyond the token's natural expiry.
	//
	// Call this on logout or forced session termination.
	RevokeAccessToken(
		ctx context.Context,
		jti uuid.UUID,
		remainingTTL time.Duration,
	) error

	// IsAccessTokenRevoked checks whether the given jti has been blocklisted
	// in Redis. Returns true if revoked, false if not found or still valid.
	//
	// Call this in the authentication middleware on every request.
	IsAccessTokenRevoked(ctx context.Context, jti uuid.UUID) (bool, error)
}
