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
	"github.com/jackc/pgx/v5"
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
	familyID uuid.UUID,
) (*TokenPair, error) {
	jti, err := uuid.NewV6()
	if err != nil {
		return nil, err
	}

	accessExpiry := time.Now().
		Add(time.Duration(ts.config.JWTConfig.ExpireDelta) * time.Minute)

	refreshExpiry := time.Now().
		AddDate(0, 0, int(ts.config.JWTConfig.RefreshExpireDelta))

	accessToken, err := ts.signJwt(jti, userID, accessExpiry)
	if err != nil {
		return nil, fmt.Errorf("sign access token: %w", err)
	}

	tokenParams := repository.RecordIssuedTokenParams{
		Jti:      jti,
		UserID:   userID,
		DeviceID: pgtype.UUID{Bytes: deviceID, Valid: true},
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

	_, err = ts.repo.RecordIssuedRefreshToken(
		ctx,
		repository.RecordIssuedRefreshTokenParams{
			TokenHash: tokenHash,
			UserID:    userID,
			DeviceID:  pgtype.UUID{Bytes: deviceID, Valid: true},
			JwtJti:    pgtype.UUID{Bytes: jti, Valid: true},
			IssuedAt:  pgtype.Timestamp{Time: time.Now(), Valid: true},
			ExpiresAt: pgtype.Timestamp{Time: refreshExpiry, Valid: true},
			FamilyID:  familyID,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("record issued refresh token: %w", err)
	}

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

	existing, err := ts.repo.ClaimRefreshToken(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Could be expired, revoked, or a replay — fetch to check family
			token, err := ts.repo.GetRefreshTokenByHash(ctx, tokenHash)
			if err == nil {
				_ = ts.RevokeFamily(ctx, token.FamilyID)
			}
			return nil, errors.New(
				"refresh token reuse detected: please re-login",
			)
		}
		return nil, errors.New("invalid or expired refresh token")
	}

	return ts.IssueTokenPair(
		ctx,
		existing.UserID,
		existing.DeviceID.Bytes,
		existing.FamilyID,
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

func (ts tokenService) RevokeByRawToken(
	ctx context.Context,
	rawToken string,
) error {
	tokenHash := hashToken(rawToken)

	existing, err := ts.repo.GetRefreshTokenByHash(ctx, tokenHash)
	if err != nil {
		return fmt.Errorf("lookup refresh token: %w", err)
	}

	return ts.RevokeFamily(ctx, existing.FamilyID)
}

func (ts tokenService) ValidateAccessToken(
	ctx context.Context,
	rawToken string,
) (*VerisafeClaims, error) {
	claims, err := ValidateJWT(rawToken, ts.config.JWTConfig.ApiSecret)
	if err != nil {
		return nil, err
	}

	jti, err := claims.JTI()
	if err != nil {
		return nil, fmt.Errorf("invalid jti: %w", err)
	}

	revoked, err := ts.IsAccessTokenRevoked(ctx, jti)
	if err != nil {
		return nil, fmt.Errorf("blocklist check failed: %w", err)
	}
	if revoked {
		return nil, fmt.Errorf("token has been revoked")
	}

	return claims, nil
}

func (ts *tokenService) signJwt(
	jti uuid.UUID,
	userID uuid.UUID,
	expiry time.Time,
) (string, error) {
	claims := jwt.RegisteredClaims{
		ID:        jti.String(),
		Subject:   userID.String(),
		Issuer:    "https://verisafe.opencrafts.io/",
		Audience:  []string{"https://academia.opencrafts.io/"},
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(expiry),
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
