package streak_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/repository"
	mockQuerier "github.com/opencrafts-io/verisafe/internal/repository/mocks"
	"github.com/opencrafts-io/verisafe/internal/service/streak"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func newService(t *testing.T) (streak.Service, *mockQuerier.MockQuerier) {
	t.Helper()
	q := mockQuerier.NewMockQuerier(gomock.NewController(t))
	return streak.NewService(q), q
}

func TestRecordActivity_WrapsFailureAsInternal(t *testing.T) {
	svc, q := newService(t)
	in := repository.RecordActivityCompletionParams{AccountID: uuid.New()}
	q.EXPECT().RecordActivityCompletion(gomock.Any(), in).
		Return(repository.RecordActivityCompletionRow{}, errors.New("connection reset"))

	_, err := svc.RecordActivity(context.Background(), in)

	assert.ErrorIs(t, err, core.ErrInternal)
}

func TestRecordActivity_ReturnsRowUnchanged(t *testing.T) {
	svc, q := newService(t)
	in := repository.RecordActivityCompletionParams{AccountID: uuid.New()}
	want := repository.RecordActivityCompletionRow{PointsEarned: 10}
	q.EXPECT().RecordActivityCompletion(gomock.Any(), in).Return(want, nil)

	got, err := svc.RecordActivity(context.Background(), in)

	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestCreateMilestone_WrapsFailureAsInternal(t *testing.T) {
	svc, q := newService(t)
	in := repository.CreateStreakMilestoneParams{Title: "week-1"}
	q.EXPECT().CreateStreakMilestone(gomock.Any(), in).
		Return(repository.StreakMilestone{}, errors.New("duplicate key"))

	_, err := svc.CreateMilestone(context.Background(), in)

	assert.ErrorIs(t, err, core.ErrInternal)
}

// The count query must be issued first, and the milestone query must never
// run if it fails -- the same short-circuit the two hand-written queries had
// before this service existed. gomock's strict expectations enforce this: an
// unexpected second call fails the test.
func TestListActiveMilestones_CountFailureShortCircuits(t *testing.T) {
	svc, q := newService(t)
	q.EXPECT().GetAllActiveStreakMilestoneCount(gomock.Any()).
		Return(int64(0), errors.New("connection reset"))

	_, _, err := svc.ListActiveMilestones(context.Background(), 10, 0)

	assert.ErrorIs(t, err, core.ErrInternal)
}

// sqlc emits items := []StreakMilestone{} for an empty result
// (emit_empty_slices), so the endpoint serialises []. Rebuilding the slice
// would turn that into null.
func TestListActiveMilestones_EmptyResultStaysAnEmptySliceNotNil(t *testing.T) {
	svc, q := newService(t)
	q.EXPECT().GetAllActiveStreakMilestoneCount(gomock.Any()).Return(int64(0), nil)
	active := true
	q.EXPECT().
		GetAllStreaksMilestoneByActive(gomock.Any(), repository.GetAllStreaksMilestoneByActiveParams{
			Limit: 10, Offset: 0, IsActive: &active,
		}).
		Return([]repository.StreakMilestone{}, nil)

	total, rows, err := svc.ListActiveMilestones(context.Background(), 10, 0)

	require.NoError(t, err)
	assert.Equal(t, int64(0), total)
	assert.NotNil(t, rows, "an empty result must marshal to [] and not null")
}

func TestListActiveMilestones_PassesPaginationThroughUnchanged(t *testing.T) {
	svc, q := newService(t)
	q.EXPECT().GetAllActiveStreakMilestoneCount(gomock.Any()).Return(int64(5), nil)
	active := true
	q.EXPECT().
		GetAllStreaksMilestoneByActive(gomock.Any(), repository.GetAllStreaksMilestoneByActiveParams{
			Limit: 25, Offset: 50, IsActive: &active,
		}).
		Return([]repository.StreakMilestone{{}}, nil)

	total, rows, err := svc.ListActiveMilestones(context.Background(), 25, 50)

	require.NoError(t, err)
	assert.Equal(t, int64(5), total)
	assert.Len(t, rows, 1)
}

func TestDeleteMilestone_WrapsFailureAsInternal(t *testing.T) {
	svc, q := newService(t)
	id := uuid.New()
	q.EXPECT().DeleteStreakMilestoneByID(gomock.Any(), id).
		Return(errors.New("connection reset"))

	err := svc.DeleteMilestone(context.Background(), id)

	assert.ErrorIs(t, err, core.ErrInternal)
}
