// Package leaderboard owns lookups against the global vibepoint ranking.
//
// It sits above the repository.New(tx) boundary and takes a repository.Querier,
// so it can be exercised with a mocked Querier rather than a live database.
// Building the paginated response envelope stays a handler concern, since it
// needs the *http.Request to construct next/previous links.
package leaderboard

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

// Service is the leaderboard domain's contract.
type Service interface {
	RankForUser(
		ctx context.Context,
		userID uuid.UUID,
	) (repository.AccountVibepointRank, error)

	// Global returns the total row count and one page of the ranking. The
	// count is fetched first, matching the two queries this replaced: if it
	// fails, the ranking query is never issued.
	Global(
		ctx context.Context,
		limit, offset int32,
	) (total int64, rows []repository.AccountVibepointRank, err error)
}

type service struct {
	q repository.Querier
}

// NewService returns a Service backed by q.
func NewService(q repository.Querier) Service {
	return &service{q: q}
}

func (s *service) RankForUser(
	ctx context.Context,
	userID uuid.UUID,
) (repository.AccountVibepointRank, error) {
	rank, err := s.q.GetLeaderBoardRankForUser(ctx, userID)
	if err != nil {
		return repository.AccountVibepointRank{}, fmt.Errorf(
			"%w: get leaderboard rank: %v", core.ErrInternal, err,
		)
	}
	return rank, nil
}

func (s *service) Global(
	ctx context.Context,
	limit, offset int32,
) (int64, []repository.AccountVibepointRank, error) {
	total, err := s.q.GetGlobalLeaderBoardCount(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf(
			"%w: get leaderboard count: %v", core.ErrInternal, err,
		)
	}

	// sqlc emits items := []AccountVibepointRank{} for an empty result
	// (emit_empty_slices), so rebuilding the slice would turn that into null
	// on the wire.
	rows, err := s.q.GetLeaderboard(ctx, repository.GetLeaderboardParams{
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return 0, nil, fmt.Errorf(
			"%w: get leaderboard: %v", core.ErrInternal, err,
		)
	}

	return total, rows, nil
}
