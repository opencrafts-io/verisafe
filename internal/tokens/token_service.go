package tokens

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

type tokenService struct {
	repo   repository.Querier
	cacher core.Cacher
	config *config.Config
}

func NewTokenService(
	repo repository.Querier,
	cacher core.Cacher,
	config *config.Config,
) TokenService {
	return tokenService{
		repo:   repo,
		cacher: cacher,
		config: config,
	}
}

func (ts tokenService) IssueTokenPair(
	ctx context.Context,
	userID, deviceID uuid.UUID,
) (*TokenPair, error) {
	jti, err := uuid.NewV6()
	if err != nil {
		return nil, err
	}

	accessExpiry := time.Now().
		Add(time.Duration(ts.config.JWTConfig.ExpireDelta))

	refreshExpiry := time.Now().Add(
		time.Duration(ts.config.JWTConfig.RefreshExpireDelta),
	)

	accessToken, err := ts.signJwt(jti, userID, accessExpiry)

	tokenParams := repository.RecordIssuedTokenParams{
		Jti:      jti,
		UserID:   userID,
		DeviceID: pgtype.UUID{Bytes: deviceID, Valid: false},
		ExpiresAt: pgtype.Timestamp{
			Time: accessExpiry, Valid: true,
		},
	}

	_, err = ts.repo.RecordIssuedToken(ctx, tokenParams)
	if err != nil {
		return nil, fmt.Errorf("record issued token %w", err)
	}

	rawRefreshToken, err := generateOpaqueToken()
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}

	tokenHash := hashToken(rawRefreshToken)
	familyID := uuid.New()

	_, err = ts.repo.RecordIssuedRefreshToken(
		ctx,
		repository.RecordIssuedRefreshTokenParams{
			TokenHash: tokenHash,
			UserID:    userID,
			DeviceID:  pgtype.UUID{Bytes: deviceID, Valid: true},
			JwtJti:    pgtype.UUID{Bytes: jti, Valid: true},
			IssuedAt:  pgtype.Timestamp{Time: refreshExpiry, Valid: true},
			FamilyID:  familyID,
		},
	)

	return &TokenPair{
		AccessToken:      accessToken,
		RawRefreshToken:  rawRefreshToken,
		AccessExpiresAt:  accessExpiry,
		RefreshExpiresAt: refreshExpiry,
	}, nil
}

func (ts tokenService) RotateRefreshToken(
	ctx context.Context,
	rawRefreshToken string,
) (*TokenPair, error) {
	tokenHash := hashToken(rawRefreshToken)

	existing, err := ts.repo.GetRefreshTokenByHash(ctx, tokenHash)
	if err != nil {
		return nil, errors.New("invalid refresh token")
	}

	// Reuse detection — token was already used
	if existing.UsedAt.Valid {
		// Revoke entire family — someone is replaying tokens
		_ = ts.RevokeFamily(ctx, existing.FamilyID)
		return nil, errors.New("refresh token reuse detected: please re-login")
	}

	// Check expiry
	if time.Now().After(existing.ExpiresAt.Time) {
		return nil, errors.New("refresh token expired")
	}

	// Check explicitly revoked
	if existing.RevokedAt.Valid {
		return nil, errors.New("refresh token has been revoked")
	}

	// Mark current token as used
	err = ts.repo.MarkRefreshTokenUsed(ctx, existing.ID)
	if err != nil {
		return nil, fmt.Errorf("mark token used: %w", err)
	}

	// Issue new pair — carry forward same device
	return ts.IssueTokenPair(
		ctx,
		existing.UserID,
		existing.DeviceID.Bytes,
	)
}

func (ts tokenService) RevokeFamily(
	ctx context.Context,
	familyID uuid.UUID,
) error {
	err := ts.repo.RevokeRefreshTokenFamily(ctx, familyID)
	if err != nil {
		return fmt.Errorf("revoke token family %s: %w", familyID, err)
	}
	return nil
}

func (ts tokenService) RevokeAccessToken(
	ctx context.Context,
	jti uuid.UUID,
	remainingTTL time.Duration,
) error {
	key := fmt.Sprintf("blocklist:%s", jti.String())
	return ts.cacher.Set(ctx, key, "revoked", remainingTTL)
}

func (ts tokenService) IsAccessTokenRevoked(
	ctx context.Context,
	jti uuid.UUID,
) (bool, error) {
	key := fmt.Sprintf("blocklist:%s", jti.String())
	var val string
	err := ts.cacher.Get(ctx, key, &val)
	if errors.Is(err, core.ErrCacheMiss) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (ts *tokenService) signJwt(
	jti uuid.UUID,
	userID uuid.UUID,
	expiry time.Time,
) (string, error) {
	claims := jwt.MapClaims{
		"jti": jti.String(),
		"sub": userID.String(),
		"iss": "https://verisafe.opencrafts.io/",
		"aud": []string{"https://academia.opencrafts.io/"},
		"iat": time.Now().Unix(),
		"exp": expiry.Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(ts.config.JWTConfig.ApiSecret))
}

func generateOpaqueToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

func hashToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}
