package tokens

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/opencrafts-io/verisafe/internal/config"
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
			ID:       uuid.New(),
			UserID:   userID,
			DeviceID: pgtype.UUID{Bytes: deviceID, Valid: true},
			FamilyID: familyID,
			ExpiresAt: pgtype.Timestamp{
				Time:  time.Now().Add(time.Hour),
				Valid: true,
			},
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

// ... TestRevokeFamily, TestRevokeAccessToken, TestIsAccessTokenRevoked unchanged ...

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
