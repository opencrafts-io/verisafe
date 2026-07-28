package servicetoken_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/repository"
	mockQuerier "github.com/opencrafts-io/verisafe/internal/repository/mocks"
	"github.com/opencrafts-io/verisafe/internal/service/servicetoken"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func newService(t *testing.T) (servicetoken.Service, *mockQuerier.MockQuerier) {
	t.Helper()
	q := mockQuerier.NewMockQuerier(gomock.NewController(t))
	return servicetoken.NewService(q), q
}

func TestVerifyBotAccount(t *testing.T) {
	accountID := uuid.New()

	t.Run("account lookup failure is core.ErrNotFound", func(t *testing.T) {
		svc, q := newService(t)
		q.EXPECT().GetAccountByID(gomock.Any(), accountID).
			Return(repository.Account{}, errors.New("connection reset"))

		_, err := svc.VerifyBotAccount(context.Background(), accountID)

		assert.ErrorIs(t, err, core.ErrNotFound)
	})

	t.Run("a non-bot account is ErrNotBotAccount, distinct from ErrNotFound", func(t *testing.T) {
		svc, q := newService(t)
		q.EXPECT().GetAccountByID(gomock.Any(), accountID).
			Return(repository.Account{Type: repository.AccountTypeHuman}, nil)

		_, err := svc.VerifyBotAccount(context.Background(), accountID)

		assert.ErrorIs(t, err, servicetoken.ErrNotBotAccount)
		assert.ErrorIs(t, err, core.ErrForbidden)
		assert.NotErrorIs(t, err, core.ErrNotFound)
	})

	t.Run("a bot account succeeds", func(t *testing.T) {
		svc, q := newService(t)
		want := repository.Account{ID: accountID, Type: repository.AccountTypeBot}
		q.EXPECT().GetAccountByID(gomock.Any(), accountID).Return(want, nil)

		got, err := svc.VerifyBotAccount(context.Background(), accountID)

		require.NoError(t, err)
		assert.Equal(t, want, got)
	})
}

func TestCreate_WrapsFailureAsInternal(t *testing.T) {
	svc, q := newService(t)
	in := repository.CreateServiceTokenParams{Name: "x"}
	q.EXPECT().CreateServiceToken(gomock.Any(), in).
		Return(repository.ServiceToken{}, errors.New("duplicate key"))

	_, err := svc.Create(context.Background(), in)

	assert.ErrorIs(t, err, core.ErrInternal)
}

// GetByID does not classify the error at all -- the handler treats every
// error from this call as "token not found", so the service's only job is to
// wrap it, not distinguish a missing row from a driver failure.
func TestGetByID_WrapsFailureAsInternalWithoutClassifying(t *testing.T) {
	svc, q := newService(t)
	id := uuid.New()
	q.EXPECT().GetServiceTokenByID(gomock.Any(), id).
		Return(repository.ServiceToken{}, errors.New("connection reset"))

	_, err := svc.GetByID(context.Background(), id)

	assert.ErrorIs(t, err, core.ErrInternal)
}

// sqlc emits items := []ServiceToken{} for an empty result
// (emit_empty_slices), so the endpoint serialises []. Rebuilding the slice
// would turn that into null.
func TestList_EmptyResultStaysAnEmptySliceNotNil(t *testing.T) {
	svc, q := newService(t)
	accountID := uuid.New()
	q.EXPECT().ListServiceTokensByAccount(gomock.Any(), accountID).
		Return([]repository.ServiceToken{}, nil)

	got, err := svc.List(context.Background(), accountID)

	require.NoError(t, err)
	assert.NotNil(t, got, "an empty result must marshal to [] and not null")
}

func TestListAllActive_EmptyResultStaysAnEmptySlice(t *testing.T) {
	svc, q := newService(t)
	q.EXPECT().ListActiveServiceTokens(gomock.Any()).
		Return([]repository.ActiveServiceToken{}, nil)

	got, err := svc.ListAllActive(context.Background())

	require.NoError(t, err)
	assert.NotNil(t, got)
}

func TestUpdate_WrapsFailureAsInternal(t *testing.T) {
	svc, q := newService(t)
	in := repository.UpdateServiceTokenParams{ID: uuid.New(), Name: "x"}
	q.EXPECT().UpdateServiceToken(gomock.Any(), in).
		Return(errors.New("connection reset"))

	err := svc.Update(context.Background(), in)

	assert.ErrorIs(t, err, core.ErrInternal)
}

func TestRotate_WrapsFailureAsInternal(t *testing.T) {
	svc, q := newService(t)
	in := repository.RotateServiceTokenParams{ID: uuid.New()}
	q.EXPECT().RotateServiceToken(gomock.Any(), in).
		Return(errors.New("connection reset"))

	err := svc.Rotate(context.Background(), in)

	assert.ErrorIs(t, err, core.ErrInternal)
}

func TestRevoke_WrapsFailureAsInternal(t *testing.T) {
	svc, q := newService(t)
	id := uuid.New()
	q.EXPECT().RevokeServiceToken(gomock.Any(), id).
		Return(errors.New("connection reset"))

	err := svc.Revoke(context.Background(), id)

	assert.ErrorIs(t, err, core.ErrInternal)
}

func TestStats_ReturnsRowUnchanged(t *testing.T) {
	svc, q := newService(t)
	accountID := uuid.New()
	want := repository.GetServiceTokenUsageStatsRow{TotalTokens: 3}
	q.EXPECT().GetServiceTokenUsageStats(gomock.Any(), accountID).Return(want, nil)

	got, err := svc.Stats(context.Background(), accountID)

	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestCleanupExpired_WrapsFailureAsInternal(t *testing.T) {
	svc, q := newService(t)
	q.EXPECT().CleanupExpiredServiceTokens(gomock.Any()).
		Return(errors.New("connection reset"))

	err := svc.CleanupExpired(context.Background())

	assert.ErrorIs(t, err, core.ErrInternal)
}
