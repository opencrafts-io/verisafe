package tokens

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	mockscore "github.com/opencrafts-io/verisafe/internal/core/mocks"
	"github.com/opencrafts-io/verisafe/internal/repository"
	mockQuerier "github.com/opencrafts-io/verisafe/internal/repository/mocks"
)

func TestNewTokenService(t *testing.T) {
	ctrl := gomock.NewController(t)
	repo := mockQuerier.NewMockQuerier(ctrl)
	cacher := mockscore.NewMockCacher(ctrl)
	svc := NewTokenService(repo, cacher, &config.Config{})
	assert.NotNil(
		t,
		svc,
		"New token service should always return a valid token service",
	)
}

func TestIssueTokenPair(t *testing.T) {
	svc := validTokenService(t)
	userID, _ := uuid.NewUUID()
	deviceID, _ := uuid.NewUUID()
	familyID := uuid.New() // ← new param

	tkP, err := svc.IssueTokenPair(context.TODO(), userID, deviceID, familyID)

	assert.NoError(t, err, "Got an error when generating token pair")
	assert.NotNil(t, tkP, "Token pair should not be empty.")
	assert.NotEmpty(
		t,
		tkP.AccessToken,
		"A valid access token should be non empty",
	)
	assert.NotEmpty(
		t,
		tkP.RawRefreshToken,
		"A valid refresh token should be non empty",
	)
}

func TestRotateRefreshToken(t *testing.T) {
	t.Run("valid token is rotated and new pair returned", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo := mockQuerier.NewMockQuerier(ctrl)
		cacher := mockscore.NewMockCacher(ctrl)

		userID := uuid.New()
		deviceID := uuid.New()
		familyID := uuid.New()
		rawToken := "valid-raw-token"

		existing := repository.RefreshToken{
			ID:        uuid.New(),
			UserID:    userID,
			DeviceID:  &deviceID,
			FamilyID:  familyID,
			ExpiresAt: time.Now().Add(time.Hour),
		}

		// ClaimRefreshToken replaces GetRefreshTokenByHash + MarkRefreshTokenUsed
		repo.EXPECT().
			ClaimRefreshToken(gomock.Any(), hashToken(rawToken)).
			Return(existing, nil)

		repo.EXPECT().
			RecordIssuedToken(gomock.Any(), gomock.Any()).
			Return(repository.IssuedToken{}, nil)

		repo.EXPECT().
			RecordIssuedRefreshToken(gomock.Any(), gomock.Any()).
			Return(repository.RefreshToken{}, nil)

		svc := NewTokenService(repo, cacher, &config.Config{})
		pair, err := svc.RotateRefreshToken(context.TODO(), rawToken)

		assert.NoError(t, err)
		assert.NotNil(t, pair)
		assert.NotEmpty(t, pair.AccessToken)
		assert.NotEmpty(t, pair.RawRefreshToken)
	})

	t.Run(
		"reuse detected revokes family and returns error",
		func(t *testing.T) {
			ctrl := gomock.NewController(t)
			repo := mockQuerier.NewMockQuerier(ctrl)
			cacher := mockscore.NewMockCacher(ctrl)

			familyID := uuid.New()
			rawToken := "already-used-token"

			existing := repository.RefreshToken{
				ID:       uuid.New(),
				FamilyID: familyID,
			}

			// ClaimRefreshToken returns ErrNoRows (token already used/expired/revoked)
			repo.EXPECT().
				ClaimRefreshToken(gomock.Any(), hashToken(rawToken)).
				Return(repository.RefreshToken{}, pgx.ErrNoRows)

			// Follow-up fetch to get familyID for revocation
			repo.EXPECT().
				GetRefreshTokenByHash(gomock.Any(), hashToken(rawToken)).
				Return(existing, nil)

			repo.EXPECT().
				RevokeRefreshTokenFamily(gomock.Any(), familyID).
				Return(nil)

			svc := NewTokenService(repo, cacher, &config.Config{})
			pair, err := svc.RotateRefreshToken(context.TODO(), rawToken)

			assert.Nil(t, pair)
			assert.ErrorContains(t, err, "reuse detected")
		},
	)

	t.Run("expired token returns error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo := mockQuerier.NewMockQuerier(ctrl)
		cacher := mockscore.NewMockCacher(ctrl)

		rawToken := "expired-token"

		// Expired token fails the WHERE expires_at > NOW() clause → ErrNoRows
		repo.EXPECT().
			ClaimRefreshToken(gomock.Any(), hashToken(rawToken)).
			Return(repository.RefreshToken{}, pgx.ErrNoRows)

		// Follow-up fetch finds nothing (token is gone/unresolvable)
		repo.EXPECT().
			GetRefreshTokenByHash(gomock.Any(), hashToken(rawToken)).
			Return(repository.RefreshToken{}, errors.New("not found"))

		svc := NewTokenService(repo, cacher, &config.Config{})
		pair, err := svc.RotateRefreshToken(context.TODO(), rawToken)

		assert.Nil(t, pair)
		assert.ErrorContains(t, err, "reuse detected")
	})

	t.Run("revoked token returns error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo := mockQuerier.NewMockQuerier(ctrl)
		cacher := mockscore.NewMockCacher(ctrl)

		rawToken := "revoked-token"

		// Revoked token fails the WHERE revoked_at IS NULL clause → ErrNoRows
		repo.EXPECT().
			ClaimRefreshToken(gomock.Any(), hashToken(rawToken)).
			Return(repository.RefreshToken{}, pgx.ErrNoRows)

		// Follow-up fetch finds nothing
		repo.EXPECT().
			GetRefreshTokenByHash(gomock.Any(), hashToken(rawToken)).
			Return(repository.RefreshToken{}, errors.New("not found"))

		svc := NewTokenService(repo, cacher, &config.Config{})
		pair, err := svc.RotateRefreshToken(context.TODO(), rawToken)

		assert.Nil(t, pair)
		assert.ErrorContains(t, err, "reuse detected")
	})

	t.Run("token not found returns error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo := mockQuerier.NewMockQuerier(ctrl)
		cacher := mockscore.NewMockCacher(ctrl)

		rawToken := "unknown-token"

		// Non-ErrNoRows error (e.g. DB down) → generic error path
		repo.EXPECT().
			ClaimRefreshToken(gomock.Any(), hashToken(rawToken)).
			Return(repository.RefreshToken{}, errors.New("db error"))

		svc := NewTokenService(repo, cacher, &config.Config{})
		pair, err := svc.RotateRefreshToken(context.TODO(), rawToken)

		assert.Nil(t, pair)
		assert.ErrorContains(t, err, "invalid or expired refresh token")
	})
}

func TestRevokeFamily(t *testing.T) {
	ctrl := gomock.NewController(t)
	repo := mockQuerier.NewMockQuerier(ctrl)
	cacher := mockscore.NewMockCacher(ctrl)
	familyID := uuid.New()

	t.Run("success", func(t *testing.T) {
		repo.EXPECT().
			RevokeRefreshTokenFamily(gomock.Any(), familyID).
			Return(nil)

		svc := NewTokenService(repo, cacher, &config.Config{})
		err := svc.RevokeFamily(context.TODO(), familyID)

		assert.NoError(t, err)
	})

	t.Run("repo error is wrapped", func(t *testing.T) {
		repo.EXPECT().
			RevokeRefreshTokenFamily(gomock.Any(), familyID).
			Return(errors.New("db down"))

		svc := NewTokenService(repo, cacher, &config.Config{})
		err := svc.RevokeFamily(context.TODO(), familyID)

		assert.ErrorContains(t, err, "revoke token family")
	})
}

func TestRevokeAccessToken(t *testing.T) {
	ctrl := gomock.NewController(t)
	repo := mockQuerier.NewMockQuerier(ctrl)
	cacher := mockscore.NewMockCacher(ctrl)
	jti := uuid.New()

	cacher.EXPECT().
		Set(gomock.Any(), "blocklist:"+jti.String(), "revoked", time.Hour).
		Return(nil)

	svc := NewTokenService(repo, cacher, &config.Config{})
	err := svc.RevokeAccessToken(context.TODO(), jti, time.Hour)

	assert.NoError(t, err)
}

func TestIsAccessTokenRevoked(t *testing.T) {
	t.Run("revoked", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo := mockQuerier.NewMockQuerier(ctrl)
		cacher := mockscore.NewMockCacher(ctrl)
		jti := uuid.New()

		cacher.EXPECT().
			Get(gomock.Any(), "blocklist:"+jti.String(), gomock.Any()).
			Return(nil)

		svc := NewTokenService(repo, cacher, &config.Config{})
		revoked, err := svc.IsAccessTokenRevoked(context.TODO(), jti)

		assert.NoError(t, err)
		assert.True(t, revoked)
	})

	t.Run("not revoked (cache miss)", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo := mockQuerier.NewMockQuerier(ctrl)
		cacher := mockscore.NewMockCacher(ctrl)
		jti := uuid.New()

		cacher.EXPECT().
			Get(gomock.Any(), "blocklist:"+jti.String(), gomock.Any()).
			Return(core.ErrCacheMiss)

		svc := NewTokenService(repo, cacher, &config.Config{})
		revoked, err := svc.IsAccessTokenRevoked(context.TODO(), jti)

		assert.NoError(t, err)
		assert.False(t, revoked)
	})

	t.Run("cache error propagates", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo := mockQuerier.NewMockQuerier(ctrl)
		cacher := mockscore.NewMockCacher(ctrl)
		jti := uuid.New()

		cacher.EXPECT().
			Get(gomock.Any(), "blocklist:"+jti.String(), gomock.Any()).
			Return(errors.New("redis down"))

		svc := NewTokenService(repo, cacher, &config.Config{})
		_, err := svc.IsAccessTokenRevoked(context.TODO(), jti)

		assert.Error(t, err)
	})
}

func TestRevokeByRawToken(t *testing.T) {
	rawToken := "some-raw-refresh-token"
	tokenHash := hashToken(rawToken)
	familyID := uuid.New()

	t.Run("success", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo := mockQuerier.NewMockQuerier(ctrl)
		cacher := mockscore.NewMockCacher(ctrl)

		repo.EXPECT().
			GetRefreshTokenByHash(gomock.Any(), tokenHash).
			Return(repository.RefreshToken{FamilyID: familyID}, nil)
		repo.EXPECT().
			RevokeRefreshTokenFamily(gomock.Any(), familyID).
			Return(nil)

		svc := NewTokenService(repo, cacher, &config.Config{})
		err := svc.RevokeByRawToken(context.TODO(), rawToken)

		assert.NoError(t, err)
	})

	t.Run("unknown token", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo := mockQuerier.NewMockQuerier(ctrl)
		cacher := mockscore.NewMockCacher(ctrl)

		repo.EXPECT().
			GetRefreshTokenByHash(gomock.Any(), tokenHash).
			Return(repository.RefreshToken{}, pgx.ErrNoRows)

		svc := NewTokenService(repo, cacher, &config.Config{})
		err := svc.RevokeByRawToken(context.TODO(), rawToken)

		assert.ErrorContains(t, err, "lookup refresh token")
	})
}

func TestValidateAccessToken(t *testing.T) {
	cfg := &config.Config{}
	cfg.JWTConfig.ApiSecret = "test-secret"

	t.Run("valid, non-revoked token", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo := mockQuerier.NewMockQuerier(ctrl)
		cacher := mockscore.NewMockCacher(ctrl)
		svc := NewTokenService(repo, cacher, cfg).(tokenService)

		jti := uuid.New()
		signed, err := svc.signJwt(jti, uuid.New(), time.Now().Add(time.Hour))
		require.NoError(t, err)

		cacher.EXPECT().
			Get(gomock.Any(), "blocklist:"+jti.String(), gomock.Any()).
			Return(core.ErrCacheMiss)

		claims, err := svc.ValidateAccessToken(context.TODO(), signed)

		assert.NoError(t, err)
		assert.Equal(t, jti.String(), claims.ID)
	})

	t.Run("revoked token is rejected", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo := mockQuerier.NewMockQuerier(ctrl)
		cacher := mockscore.NewMockCacher(ctrl)
		svc := NewTokenService(repo, cacher, cfg).(tokenService)

		jti := uuid.New()
		signed, err := svc.signJwt(jti, uuid.New(), time.Now().Add(time.Hour))
		require.NoError(t, err)

		cacher.EXPECT().
			Get(gomock.Any(), "blocklist:"+jti.String(), gomock.Any()).
			Return(nil)

		_, err = svc.ValidateAccessToken(context.TODO(), signed)

		assert.ErrorContains(t, err, "revoked")
	})

	t.Run("malformed token is rejected", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo := mockQuerier.NewMockQuerier(ctrl)
		cacher := mockscore.NewMockCacher(ctrl)
		svc := NewTokenService(repo, cacher, cfg)

		_, err := svc.ValidateAccessToken(context.TODO(), "not-a-jwt")

		assert.Error(t, err)
	})
}

func validTokenService(t *testing.T) TokenService {
	ctrl := gomock.NewController(t)
	repo := mockQuerier.NewMockQuerier(ctrl)
	repo.EXPECT().
		RecordIssuedToken(gomock.Any(), gomock.Any()).
		Return(repository.IssuedToken{}, nil)
	repo.EXPECT().
		RecordIssuedRefreshToken(gomock.Any(), gomock.Any()).
		Return(repository.RefreshToken{}, nil).
		Times(1)
	cacher := mockscore.NewMockCacher(ctrl)
	svc := NewTokenService(repo, cacher, &config.Config{})
	return svc
}
