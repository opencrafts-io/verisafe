package activity_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/repository"
	mockQuerier "github.com/opencrafts-io/verisafe/internal/repository/mocks"
	"github.com/opencrafts-io/verisafe/internal/service/activity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func newService(t *testing.T) (activity.Service, *mockQuerier.MockQuerier) {
	t.Helper()
	q := mockQuerier.NewMockQuerier(gomock.NewController(t))
	return activity.NewService(q), q
}

// Each List* method must issue the count query first and skip the row query
// entirely if it fails -- the same short-circuit the two hand-written queries
// had before this service existed. gomock's strict expectations enforce this:
// an unexpected second call fails the test.

func TestList_CountFailureShortCircuits(t *testing.T) {
	svc, q := newService(t)
	q.EXPECT().GetAllActivitiesCount(gomock.Any()).
		Return(int64(0), errors.New("connection reset"))

	_, _, err := svc.List(context.Background(), 10, 0)

	assert.ErrorIs(t, err, core.ErrInternal)
}

func TestList_EmptyResultStaysAnEmptySliceNotNil(t *testing.T) {
	svc, q := newService(t)
	q.EXPECT().GetAllActivitiesCount(gomock.Any()).Return(int64(0), nil)
	q.EXPECT().
		GetAllActivities(gomock.Any(), repository.GetAllActivitiesParams{
			Limit: 10, Offset: 0,
		}).
		Return([]repository.Activity{}, nil)

	total, rows, err := svc.List(context.Background(), 10, 0)

	require.NoError(t, err)
	assert.Equal(t, int64(0), total)
	assert.NotNil(t, rows, "an empty result must marshal to [] and not null")
}

func TestListActive_CountFailureShortCircuits(t *testing.T) {
	svc, q := newService(t)
	q.EXPECT().GetAllActiveActivitiesCount(gomock.Any()).
		Return(int64(0), errors.New("connection reset"))

	_, _, err := svc.ListActive(context.Background(), 10, 0)

	assert.ErrorIs(t, err, core.ErrInternal)
}

func TestListActive_PassesPaginationThroughUnchanged(t *testing.T) {
	svc, q := newService(t)
	q.EXPECT().GetAllActiveActivitiesCount(gomock.Any()).Return(int64(3), nil)
	q.EXPECT().
		GetAllActiveActivities(gomock.Any(), repository.GetAllActiveActivitiesParams{
			Limit: 25, Offset: 50,
		}).
		Return([]repository.Activity{{}}, nil)

	total, rows, err := svc.ListActive(context.Background(), 25, 50)

	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	assert.Len(t, rows, 1)
}

func TestListInactive_CountFailureShortCircuits(t *testing.T) {
	svc, q := newService(t)
	q.EXPECT().GetAllInactiveActivitiesCount(gomock.Any()).
		Return(int64(0), errors.New("connection reset"))

	_, _, err := svc.ListInactive(context.Background(), 10, 0)

	assert.ErrorIs(t, err, core.ErrInternal)
}

func TestListInactive_EmptyResultStaysAnEmptySlice(t *testing.T) {
	svc, q := newService(t)
	q.EXPECT().GetAllInactiveActivitiesCount(gomock.Any()).Return(int64(0), nil)
	q.EXPECT().
		GetAllInactiveActivities(gomock.Any(), repository.GetAllInactiveActivitiesParams{
			Limit: 10, Offset: 0,
		}).
		Return([]repository.Activity{}, nil)

	_, rows, err := svc.ListInactive(context.Background(), 10, 0)

	require.NoError(t, err)
	assert.NotNil(t, rows)
}

func TestListCompletionsForUser_CountFailureShortCircuits(t *testing.T) {
	svc, q := newService(t)
	accountID := uuid.New()
	q.EXPECT().GetAllUserActivityCompletionsCount(gomock.Any(), accountID).
		Return(int64(0), errors.New("connection reset"))

	_, _, err := svc.ListCompletionsForUser(context.Background(), accountID, 10, 0)

	assert.ErrorIs(t, err, core.ErrInternal)
}

func TestListCompletionsForUser_PassesArgumentsThroughUnchanged(t *testing.T) {
	svc, q := newService(t)
	accountID := uuid.New()
	q.EXPECT().GetAllUserActivityCompletionsCount(gomock.Any(), accountID).
		Return(int64(7), nil)
	q.EXPECT().
		GetAllUserActivityCompletions(gomock.Any(), repository.GetAllUserActivityCompletionsParams{
			AccountID: accountID, Limit: 20, Offset: 40,
		}).
		Return([]repository.ActivityCompletion{{}}, nil)

	total, rows, err := svc.ListCompletionsForUser(
		context.Background(), accountID, 20, 40,
	)

	require.NoError(t, err)
	assert.Equal(t, int64(7), total)
	assert.Len(t, rows, 1)
}

func TestCreate_WrapsFailureAsInternal(t *testing.T) {
	svc, q := newService(t)
	in := repository.CreateActivityParams{Name: "x"}
	q.EXPECT().CreateActivity(gomock.Any(), in).
		Return(repository.Activity{}, errors.New("duplicate key"))

	_, err := svc.Create(context.Background(), in)

	assert.ErrorIs(t, err, core.ErrInternal)
}

func TestUpdate_WrapsFailureAsInternal(t *testing.T) {
	svc, q := newService(t)
	in := repository.UpdateActivityParams{ID: uuid.New()}
	q.EXPECT().UpdateActivity(gomock.Any(), in).
		Return(repository.Activity{}, errors.New("connection reset"))

	_, err := svc.Update(context.Background(), in)

	assert.ErrorIs(t, err, core.ErrInternal)
}

func TestDelete_WrapsFailureAsInternal(t *testing.T) {
	svc, q := newService(t)
	id := uuid.New()
	q.EXPECT().DeleteActivity(gomock.Any(), id).
		Return(errors.New("connection reset"))

	err := svc.Delete(context.Background(), id)

	assert.ErrorIs(t, err, core.ErrInternal)
}
