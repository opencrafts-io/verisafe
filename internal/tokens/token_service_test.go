package tokens

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/opencrafts-io/verisafe/internal/config"
	mockscore "github.com/opencrafts-io/verisafe/internal/core/mocks"
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

	tkP, err := svc.IssueTokenPair(context.TODO(),
		userID,
		deviceID,
	)

	assert.NoError(
		t,
		err, "Got an error when generating token pair",
	)

	assert.NotNil(
		t,
		tkP,
		"Token pair should not be empty.",
	)

	accessToken := tkP.AccessToken
	refreshToken := tkP.RawRefreshToken
	t.Log(accessToken)
	t.Log(refreshToken)

	assert.NotEmpty(t, accessToken, "A valid access token should be non empty")
	assert.NotEmpty(
		t,
		refreshToken,
		"A valid refreshToken token should be non empty",
	)
}

func TestRotateRefreshToken(t *testing.T) {
}

func TestRevokeFamily(t *testing.T) {
}

func TestRevokeAccessToken(t *testing.T) {
}

func IsAccessTokenRevoked(t *testing.T) {
}

func validTokenService(t *testing.T) TokenService {
	ctrl := gomock.NewController(t)

	repo := mockQuerier.NewMockQuerier(ctrl)
	repo.
		EXPECT().
		RecordIssuedToken(gomock.Any(), gomock.Any())
	repo.EXPECT().RecordIssuedRefreshToken(gomock.Any(), gomock.Any()).Times(1)
	cacher := mockscore.NewMockCacher(ctrl)

	svc := NewTokenService(repo, cacher, &config.Config{})
	return svc
}
