package leaderboard_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/repository"
	mockQuerier "github.com/opencrafts-io/verisafe/internal/repository/mocks"
	"github.com/opencrafts-io/verisafe/internal/service/leaderboard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func newService(t *testing.T) (leaderboard.Service, *mockQuerier.MockQuerier) {
	t.Helper()
	q := mockQuerier.NewMockQuerier(gomock.NewController(t))
	return leaderboard.NewService(q), q
}

func TestRankForUser_DriverErrorIsInternal(t *testing.T) {
	svc, q := newService(t)
	userID := uuid.New()
	q.EXPECT().GetLeaderBoardRankForUser(gomock.Any(), userID).
		Return(repository.AccountVibepointRank{}, errors.New("connection reset"))

	_, err := svc.RankForUser(context.Background(), userID)

	assert.ErrorIs(t, err, core.ErrInternal)
}

func TestRankForUser_ReturnsRowUnchanged(t *testing.T) {
	svc, q := newService(t)
	userID := uuid.New()
	want := repository.AccountVibepointRank{ID: userID}
	q.EXPECT().GetLeaderBoardRankForUser(gomock.Any(), userID).Return(want, nil)

	got, err := svc.RankForUser(context.Background(), userID)

	require.NoError(t, err)
	assert.Equal(t, want, got)
}

// If the count query fails, the leaderboard query must never be issued -- the
// same short-circuit the two hand-written queries had before this service
// existed. gomock's strict-by-default expectations enforce this: an
// unexpected GetLeaderboard call fails the test.
func TestGlobal_CountFailureShortCircuitsBeforeTheLeaderboardQuery(t *testing.T) {
	svc, q := newService(t)
	q.EXPECT().GetGlobalLeaderBoardCount(gomock.Any()).
		Return(int64(0), errors.New("connection reset"))

	_, _, err := svc.Global(context.Background(), 10, 0)

	assert.ErrorIs(t, err, core.ErrInternal)
}

// sqlc emits items := []AccountVibepointRank{} for an empty result
// (emit_empty_slices), so the endpoint serialises []. Rebuilding the slice
// would turn that into null.
func TestGlobal_EmptyResultStaysAnEmptySliceNotNil(t *testing.T) {
	svc, q := newService(t)
	q.EXPECT().GetGlobalLeaderBoardCount(gomock.Any()).Return(int64(0), nil)
	q.EXPECT().
		GetLeaderboard(gomock.Any(), repository.GetLeaderboardParams{
			Limit: 10, Offset: 0,
		}).
		Return([]repository.AccountVibepointRank{}, nil)

	total, rows, err := svc.Global(context.Background(), 10, 0)

	require.NoError(t, err)
	assert.Equal(t, int64(0), total)
	assert.NotNil(t, rows, "an empty result must marshal to [] and not null")
}

func TestGlobal_PassesPaginationThroughUnchanged(t *testing.T) {
	svc, q := newService(t)
	q.EXPECT().GetGlobalLeaderBoardCount(gomock.Any()).Return(int64(42), nil)
	q.EXPECT().
		GetLeaderboard(gomock.Any(), repository.GetLeaderboardParams{
			Limit: 25, Offset: 50,
		}).
		Return([]repository.AccountVibepointRank{{}}, nil)

	total, rows, err := svc.Global(context.Background(), 25, 50)

	require.NoError(t, err)
	assert.Equal(t, int64(42), total)
	assert.Len(t, rows, 1)
}

func TestGlobal_LeaderboardQueryFailureIsInternal(t *testing.T) {
	svc, q := newService(t)
	q.EXPECT().GetGlobalLeaderBoardCount(gomock.Any()).Return(int64(1), nil)
	q.EXPECT().GetLeaderboard(gomock.Any(), gomock.Any()).
		Return(nil, errors.New("connection reset"))

	_, _, err := svc.Global(context.Background(), 10, 0)

	assert.ErrorIs(t, err, core.ErrInternal)
}
