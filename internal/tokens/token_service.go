package tokens

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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
	return nil, nil
}

func (ts tokenService) RevokeFamily(
	ctx context.Context,
	familyID uuid.UUID,
) error {
	return nil
}

func (ts tokenService) RevokeAccessToken(
	ctx context.Context,
	jti uuid.UUID,
	remainingTTL time.Duration,
) error {
	return nil
}

func (ts tokenService) IsAccessTokenRevoked(
	ctx context.Context,
	jti uuid.UUID,
) (bool, error) {
	return false, nil
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
