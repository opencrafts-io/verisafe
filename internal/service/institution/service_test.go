package institution_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/repository"
	mockQuerier "github.com/opencrafts-io/verisafe/internal/repository/mocks"
	"github.com/opencrafts-io/verisafe/internal/service/institution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func newService(t *testing.T) (institution.Service, *mockQuerier.MockQuerier) {
	t.Helper()
	q := mockQuerier.NewMockQuerier(gomock.NewController(t))
	return institution.NewService(q), q
}

func TestCreate_WrapsFailureAsInternal(t *testing.T) {
	svc, q := newService(t)
	in := repository.CreateInstitutionParams{Name: "x"}
	q.EXPECT().CreateInstitution(gomock.Any(), in).
		Return(repository.Institution{}, errors.New("duplicate key"))

	_, err := svc.Create(context.Background(), in)

	assert.ErrorIs(t, err, core.ErrInternal)
}

func TestUpdate_WrapsFailureAsInternal(t *testing.T) {
	svc, q := newService(t)
	in := repository.UpdateInstitutionParams{InstitutionID: 1}
	q.EXPECT().UpdateInstitution(gomock.Any(), in).
		Return(repository.Institution{}, errors.New("connection reset"))

	_, err := svc.Update(context.Background(), in)

	assert.ErrorIs(t, err, core.ErrInternal)
}

// GetByID does not classify the error at all -- the handler treats every
// error from this call as 404, so the service's only job is to wrap it, not
// distinguish a missing row from a driver failure.
func TestGetByID_WrapsFailureAsInternalWithoutClassifying(t *testing.T) {
	svc, q := newService(t)
	q.EXPECT().GetInstitution(gomock.Any(), int32(1)).
		Return(repository.Institution{}, errors.New("connection reset"))

	_, err := svc.GetByID(context.Background(), 1)

	assert.ErrorIs(t, err, core.ErrInternal)
}

func TestGetByID_ReturnsRowUnchanged(t *testing.T) {
	svc, q := newService(t)
	want := repository.Institution{InstitutionID: 1, Name: "Acme University"}
	q.EXPECT().GetInstitution(gomock.Any(), int32(1)).Return(want, nil)

	got, err := svc.GetByID(context.Background(), 1)

	require.NoError(t, err)
	assert.Equal(t, want, got)
}

// sqlc emits items := []Institution{} for an empty result
// (emit_empty_slices), so the endpoint serialises []. Rebuilding the slice
// would turn that into null.
func TestList_EmptyResultStaysAnEmptySliceNotNil(t *testing.T) {
	svc, q := newService(t)
	q.EXPECT().
		ListInstitutions(gomock.Any(), repository.ListInstitutionsParams{
			Limit: 10, Offset: 0,
		}).
		Return([]repository.Institution{}, nil)

	got, err := svc.List(context.Background(), 10, 0)

	require.NoError(t, err)
	assert.NotNil(t, got, "an empty result must marshal to [] and not null")
}

func TestDelete_WrapsFailureAsInternal(t *testing.T) {
	svc, q := newService(t)
	q.EXPECT().DeleteInstitution(gomock.Any(), int32(1)).
		Return(errors.New("connection reset"))

	err := svc.Delete(context.Background(), 1)

	assert.ErrorIs(t, err, core.ErrInternal)
}

func TestSearchByName_PassesArgumentsThroughUnchanged(t *testing.T) {
	svc, q := newService(t)
	q.EXPECT().
		SearchInstitutionsByName(gomock.Any(), repository.SearchInstitutionsByNameParams{
			Name: "acme", Limit: 25, Offset: 50,
		}).
		Return([]repository.Institution{{}}, nil)

	got, err := svc.SearchByName(context.Background(), "acme", 25, 50)

	require.NoError(t, err)
	assert.Len(t, got, 1)
}

func TestAddAccount_WrapsFailureAsInternal(t *testing.T) {
	svc, q := newService(t)
	in := repository.AddAccountInstitutionParams{AccountID: uuid.New(), InstitutionID: 1}
	q.EXPECT().AddAccountInstitution(gomock.Any(), in).
		Return(repository.AddAccountInstitutionRow{}, errors.New("duplicate key"))

	_, err := svc.AddAccount(context.Background(), in)

	assert.ErrorIs(t, err, core.ErrInternal)
}

func TestRemoveAccount_WrapsFailureAsInternal(t *testing.T) {
	svc, q := newService(t)
	in := repository.RemoveAccountInstitutionParams{AccountID: uuid.New(), InstitutionID: 1}
	q.EXPECT().RemoveAccountInstitution(gomock.Any(), in).
		Return(errors.New("connection reset"))

	err := svc.RemoveAccount(context.Background(), in)

	assert.ErrorIs(t, err, core.ErrInternal)
}

func TestListForAccount_EmptyResultStaysAnEmptySlice(t *testing.T) {
	svc, q := newService(t)
	accountID := uuid.New()
	q.EXPECT().
		ListInstitutionsForAccount(gomock.Any(), repository.ListInstitutionsForAccountParams{
			AccountID: accountID, Limit: 10, Offset: 0,
		}).
		Return([]repository.Institution{}, nil)

	got, err := svc.ListForAccount(context.Background(), accountID, 10, 0)

	require.NoError(t, err)
	assert.NotNil(t, got)
}

func TestListAccountsForInstitution_EmptyResultStaysAnEmptySlice(t *testing.T) {
	svc, q := newService(t)
	q.EXPECT().
		ListAccountsForInstitution(gomock.Any(), repository.ListAccountsForInstitutionParams{
			InstitutionID: 1, Limit: 10, Offset: 0,
		}).
		Return([]repository.Account{}, nil)

	got, err := svc.ListAccountsForInstitution(context.Background(), 1, 10, 0)

	require.NoError(t, err)
	assert.NotNil(t, got)
}

func TestListConnectionsBatch_PassesPaginationThroughUnchanged(t *testing.T) {
	svc, q := newService(t)
	q.EXPECT().
		ListInstitutionConnections(gomock.Any(), repository.ListInstitutionConnectionsParams{
			Limit: 500, Offset: 1000,
		}).
		Return([]repository.AccountInstitution{{}}, nil)

	got, err := svc.ListConnectionsBatch(context.Background(), 500, 1000)

	require.NoError(t, err)
	assert.Len(t, got, 1)
}
