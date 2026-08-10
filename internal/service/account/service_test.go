package account_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/repository"
	mockQuerier "github.com/opencrafts-io/verisafe/internal/repository/mocks"
	"github.com/opencrafts-io/verisafe/internal/service/account"
	"github.com/opencrafts-io/verisafe/internal/tokens"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func newService(t *testing.T) (account.Service, *mockQuerier.MockQuerier) {
	t.Helper()
	q := mockQuerier.NewMockQuerier(gomock.NewController(t))
	return account.NewService(q), q
}

func TestCreate_WrapsFailureAsInternal(t *testing.T) {
	svc, q := newService(t)
	in := repository.CreateAccountParams{Email: "a@b.com", Name: "x"}
	q.EXPECT().CreateAccount(gomock.Any(), in).
		Return(repository.Account{}, errors.New("duplicate key"))

	_, err := svc.Create(context.Background(), in)

	assert.ErrorIs(t, err, core.ErrInternal)
}

func TestGetBotRole_WrapsFailureAsInternal(t *testing.T) {
	svc, q := newService(t)
	q.EXPECT().GetRoleByName(gomock.Any(), "bot").
		Return(repository.Role{}, errors.New("connection reset"))

	_, err := svc.GetBotRole(context.Background())

	assert.ErrorIs(t, err, core.ErrInternal)
}

func TestAssignRole_WrapsFailureAsInternal(t *testing.T) {
	svc, q := newService(t)
	userID, roleID := uuid.New(), uuid.New()
	q.EXPECT().
		AssignRole(gomock.Any(), repository.AssignRoleParams{UserID: userID, RoleID: roleID}).
		Return(repository.UserRole{}, errors.New("duplicate key"))

	err := svc.AssignRole(context.Background(), userID, roleID)

	assert.ErrorIs(t, err, core.ErrInternal)
}

// This is the critical property the CreateBotAccount fix depends on: the
// service hashes the raw token itself, so the caller cannot accidentally
// store a raw token by forgetting to hash it -- which is exactly what
// account_handler.go did before this extraction. tokens.HashToken is
// deterministic (sha256 + base64), so asserting the exact expected hash
// value is a real assertion, not a tautology.
func TestCreateServiceToken_HashesTheRawTokenBeforeStoring(t *testing.T) {
	svc, q := newService(t)
	rawToken := "vst_the-raw-token-value"
	wantHash := tokens.HashToken(rawToken)

	var captured repository.CreateServiceTokenParams
	q.EXPECT().
		CreateServiceToken(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, arg repository.CreateServiceTokenParams) (repository.ServiceToken, error) {
			captured = arg
			return repository.ServiceToken{}, nil
		})

	_, err := svc.CreateServiceToken(context.Background(), rawToken, repository.CreateServiceTokenParams{
		Name: "x",
	})

	require.NoError(t, err)
	assert.Equal(t, wantHash, captured.TokenHash)
	assert.NotEqual(t, rawToken, captured.TokenHash, "the raw token must never be stored as the hash")
}

func TestCreateServiceToken_WrapsFailureAsInternal(t *testing.T) {
	svc, q := newService(t)
	q.EXPECT().CreateServiceToken(gomock.Any(), gomock.Any()).
		Return(repository.ServiceToken{}, errors.New("duplicate key"))

	_, err := svc.CreateServiceToken(context.Background(), "raw", repository.CreateServiceTokenParams{})

	assert.ErrorIs(t, err, core.ErrInternal)
}

func TestGetByID(t *testing.T) {
	id := uuid.New()

	t.Run("a missing row is core.ErrNotFound via a real errors.Is check", func(t *testing.T) {
		svc, q := newService(t)
		q.EXPECT().GetAccountByID(gomock.Any(), id).
			Return(repository.Account{}, pgx.ErrNoRows)

		_, err := svc.GetByID(context.Background(), id)

		assert.ErrorIs(t, err, core.ErrNotFound)
		assert.NotErrorIs(t, err, core.ErrInternal)
	})

	t.Run("any other driver error is internal", func(t *testing.T) {
		svc, q := newService(t)
		q.EXPECT().GetAccountByID(gomock.Any(), id).
			Return(repository.Account{}, errors.New("connection reset"))

		_, err := svc.GetByID(context.Background(), id)

		assert.ErrorIs(t, err, core.ErrInternal)
		assert.NotErrorIs(t, err, core.ErrNotFound)
	})
}

func TestUpdate_WrapsFailureAsInternal(t *testing.T) {
	svc, q := newService(t)
	in := repository.UpdateAccountDetailsParams{ID: uuid.New(), Name: "x"}
	q.EXPECT().UpdateAccountDetails(gomock.Any(), in).Return(errors.New("connection reset"))

	err := svc.Update(context.Background(), in)

	assert.ErrorIs(t, err, core.ErrInternal)
}

func TestUpdatePhone_WrapsFailureAsInternal(t *testing.T) {
	svc, q := newService(t)
	in := repository.UpdateAccountPhoneNumberParams{ID: uuid.New(), Phone: "12345"}
	q.EXPECT().UpdateAccountPhoneNumber(gomock.Any(), in).Return(errors.New("connection reset"))

	err := svc.UpdatePhone(context.Background(), in)

	assert.ErrorIs(t, err, core.ErrInternal)
}

// sqlc emits items := []Account{} for an empty result (emit_empty_slices),
// so the endpoint serialises []. Rebuilding the slice would turn that into
// null.
func TestSearchByEmail_EmptyResultStaysAnEmptySliceNotNil(t *testing.T) {
	svc, q := newService(t)
	q.EXPECT().
		SearchAccountByEmail(gomock.Any(), repository.SearchAccountByEmailParams{
			Email: "a@b.com", Limit: 10, Offset: 0,
		}).
		Return([]repository.Account{}, nil)

	got, err := svc.SearchByEmail(context.Background(), "a@b.com", 10, 0)

	require.NoError(t, err)
	assert.NotNil(t, got, "an empty result must marshal to [] and not null")
}

func TestSearchByName_PassesArgumentsThroughUnchanged(t *testing.T) {
	svc, q := newService(t)
	q.EXPECT().
		SearchAccountByName(gomock.Any(), repository.SearchAccountByNameParams{
			Name: "ada", Limit: 25, Offset: 50,
		}).
		Return([]repository.Account{{}}, nil)

	got, err := svc.SearchByName(context.Background(), "ada", 25, 50)

	require.NoError(t, err)
	assert.Len(t, got, 1)
}

func TestSearchByUsername_EmptyResultStaysAnEmptySlice(t *testing.T) {
	svc, q := newService(t)
	q.EXPECT().
		SearchAccountByUsername(gomock.Any(), repository.SearchAccountByUsernameParams{
			Username: "ada", Limit: 10, Offset: 0,
		}).
		Return([]repository.Account{}, nil)

	got, err := svc.SearchByUsername(context.Background(), "ada", 10, 0)

	require.NoError(t, err)
	assert.NotNil(t, got)
}

func TestList_EmptyResultStaysAnEmptySlice(t *testing.T) {
	svc, q := newService(t)
	q.EXPECT().
		GetAllAccounts(gomock.Any(), repository.GetAllAccountsParams{Limit: 10, Offset: 0}).
		Return([]repository.Account{}, nil)

	got, err := svc.List(context.Background(), 10, 0)

	require.NoError(t, err)
	assert.NotNil(t, got)
}

func TestMarkForDeletion_WrapsFailureAsInternal(t *testing.T) {
	svc, q := newService(t)
	id := uuid.New()
	q.EXPECT().MarkAccountForDeletion(gomock.Any(), id).Return(errors.New("connection reset"))

	err := svc.MarkForDeletion(context.Background(), id)

	assert.ErrorIs(t, err, core.ErrInternal)
}

func TestMarkForRecovery_WrapsFailureAsInternal(t *testing.T) {
	svc, q := newService(t)
	id := uuid.New()
	q.EXPECT().MarkAccountForRecovery(gomock.Any(), id).Return(errors.New("connection reset"))

	err := svc.MarkForRecovery(context.Background(), id)

	assert.ErrorIs(t, err, core.ErrInternal)
}

func TestCount_WrapsFailureAsInternal(t *testing.T) {
	svc, q := newService(t)
	q.EXPECT().GetAccountsCount(gomock.Any()).Return(int64(0), errors.New("connection reset"))

	_, err := svc.Count(context.Background())

	assert.ErrorIs(t, err, core.ErrInternal)
}

func TestListBatch_PassesPaginationThroughUnchanged(t *testing.T) {
	svc, q := newService(t)
	q.EXPECT().
		GetAllAccounts(gomock.Any(), repository.GetAllAccountsParams{Limit: 1000, Offset: 2000}).
		Return([]repository.Account{{}, {}}, nil)

	got, err := svc.ListBatch(context.Background(), 1000, 2000)

	require.NoError(t, err)
	assert.Len(t, got, 2)
}
