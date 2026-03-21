package tokens

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	mockscore "github.com/opencrafts-io/verisafe/internal/core/mocks"
	"github.com/opencrafts-io/verisafe/internal/repository"
	mockQuerier "github.com/opencrafts-io/verisafe/internal/repository/mocks"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
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

	tkP, err := svc.IssueTokenPair(context.TODO(), userID, deviceID)

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
		existingID := uuid.New()
		familyID := uuid.New()
		rawToken := "valid-raw-token"

		existing := repository.RefreshToken{
			ID:       existingID,
			UserID:   userID,
			DeviceID: pgtype.UUID{Bytes: deviceID, Valid: true},
			FamilyID: familyID,
			ExpiresAt: pgtype.Timestamp{
				Time:  time.Now().Add(time.Hour),
				Valid: true,
			},
			UsedAt:    pgtype.Timestamp{Valid: false}, // not used
			RevokedAt: pgtype.Timestamp{Valid: false}, // not revoked
		}

		repo.EXPECT().
			GetRefreshTokenByHash(gomock.Any(), hashToken(rawToken)).
			Return(existing, nil)

		repo.EXPECT().
			MarkRefreshTokenUsed(gomock.Any(), existingID).
			Return(nil)

		// IssueTokenPair is called internally after rotation
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
				UsedAt: pgtype.Timestamp{
					Time:  time.Now().Add(-time.Hour),
					Valid: true,
				}, // already used
			}

			repo.EXPECT().
				GetRefreshTokenByHash(gomock.Any(), hashToken(rawToken)).
				Return(existing, nil)

			// Entire family should be revoked
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

		existing := repository.RefreshToken{
			ID:     uuid.New(),
			UsedAt: pgtype.Timestamp{Valid: false},
			ExpiresAt: pgtype.Timestamp{
				Time:  time.Now().Add(-time.Hour),
				Valid: true,
			}, // expired
		}

		repo.EXPECT().
			GetRefreshTokenByHash(gomock.Any(), hashToken(rawToken)).
			Return(existing, nil)

		svc := NewTokenService(repo, cacher, &config.Config{})
		pair, err := svc.RotateRefreshToken(context.TODO(), rawToken)

		assert.Nil(t, pair)
		assert.ErrorContains(t, err, "expired")
	})

	t.Run("revoked token returns error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo := mockQuerier.NewMockQuerier(ctrl)
		cacher := mockscore.NewMockCacher(ctrl)

		rawToken := "revoked-token"

		existing := repository.RefreshToken{
			ID:     uuid.New(),
			UsedAt: pgtype.Timestamp{Valid: false},
			ExpiresAt: pgtype.Timestamp{
				Time:  time.Now().Add(time.Hour),
				Valid: true,
			},
			RevokedAt: pgtype.Timestamp{
				Time:  time.Now().Add(-time.Hour),
				Valid: true,
			}, // revoked
		}

		repo.EXPECT().
			GetRefreshTokenByHash(gomock.Any(), hashToken(rawToken)).
			Return(existing, nil)

		svc := NewTokenService(repo, cacher, &config.Config{})
		pair, err := svc.RotateRefreshToken(context.TODO(), rawToken)

		assert.Nil(t, pair)
		assert.ErrorContains(t, err, "revoked")
	})

	t.Run("token not found returns error", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo := mockQuerier.NewMockQuerier(ctrl)
		cacher := mockscore.NewMockCacher(ctrl)

		rawToken := "unknown-token"

		repo.EXPECT().
			GetRefreshTokenByHash(gomock.Any(), hashToken(rawToken)).
			Return(repository.RefreshToken{}, errors.New("not found"))

		svc := NewTokenService(repo, cacher, &config.Config{})
		pair, err := svc.RotateRefreshToken(context.TODO(), rawToken)

		assert.Nil(t, pair)
		assert.ErrorContains(t, err, "invalid refresh token")
	})
}

func TestRevokeFamily(t *testing.T) {
	t.Run("successfully revokes token family", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo := mockQuerier.NewMockQuerier(ctrl)
		cacher := mockscore.NewMockCacher(ctrl)
		familyID := uuid.New()

		repo.EXPECT().
			RevokeRefreshTokenFamily(gomock.Any(), familyID).
			Return(nil)

		svc := NewTokenService(repo, cacher, &config.Config{})
		err := svc.RevokeFamily(context.TODO(), familyID)

		assert.NoError(t, err)
	})

	t.Run("repo error is wrapped and returned", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo := mockQuerier.NewMockQuerier(ctrl)
		cacher := mockscore.NewMockCacher(ctrl)
		familyID := uuid.New()

		repo.EXPECT().
			RevokeRefreshTokenFamily(gomock.Any(), familyID).
			Return(errors.New("db error"))

		svc := NewTokenService(repo, cacher, &config.Config{})
		err := svc.RevokeFamily(context.TODO(), familyID)

		assert.ErrorContains(t, err, familyID.String())
	})
}

func TestRevokeAccessToken(t *testing.T) {
	t.Run("sets jti in blocklist with correct key and ttl", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo := mockQuerier.NewMockQuerier(ctrl)
		cacher := mockscore.NewMockCacher(ctrl)

		jti := uuid.New()
		ttl := 10 * time.Minute
		expectedKey := "blocklist:" + jti.String()

		cacher.EXPECT().
			Set(gomock.Any(), expectedKey, "revoked", ttl).
			Return(nil)

		svc := NewTokenService(repo, cacher, &config.Config{})
		err := svc.RevokeAccessToken(context.TODO(), jti, ttl)

		assert.NoError(t, err)
	})

	t.Run("cacher error is returned", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo := mockQuerier.NewMockQuerier(ctrl)
		cacher := mockscore.NewMockCacher(ctrl)

		jti := uuid.New()

		cacher.EXPECT().
			Set(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(errors.New("redis unavailable"))

		svc := NewTokenService(repo, cacher, &config.Config{})
		err := svc.RevokeAccessToken(context.TODO(), jti, time.Minute)

		assert.Error(t, err)
	})
}

func TestIsAccessTokenRevoked(t *testing.T) {
	t.Run("returns true when jti is in blocklist", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo := mockQuerier.NewMockQuerier(ctrl)
		cacher := mockscore.NewMockCacher(ctrl)

		jti := uuid.New()
		expectedKey := "blocklist:" + jti.String()

		cacher.EXPECT().
			Get(gomock.Any(), expectedKey, gomock.Any()).
			Return(nil)

		svc := NewTokenService(repo, cacher, &config.Config{})
		revoked, err := svc.IsAccessTokenRevoked(context.TODO(), jti)

		assert.NoError(t, err)
		assert.True(t, revoked)
	})

	t.Run("returns false on cache miss", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo := mockQuerier.NewMockQuerier(ctrl)
		cacher := mockscore.NewMockCacher(ctrl)

		jti := uuid.New()

		cacher.EXPECT().
			Get(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(core.ErrCacheMiss)

		svc := NewTokenService(repo, cacher, &config.Config{})
		revoked, err := svc.IsAccessTokenRevoked(context.TODO(), jti)

		assert.NoError(t, err)
		assert.False(t, revoked)
	})

	t.Run("returns error on cacher failure", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo := mockQuerier.NewMockQuerier(ctrl)
		cacher := mockscore.NewMockCacher(ctrl)

		jti := uuid.New()

		cacher.EXPECT().
			Get(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(errors.New("redis unavailable"))

		svc := NewTokenService(repo, cacher, &config.Config{})
		revoked, err := svc.IsAccessTokenRevoked(context.TODO(), jti)

		assert.Error(t, err)
		assert.False(t, revoked)
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
