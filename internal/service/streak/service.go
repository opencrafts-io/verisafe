// Package streak owns recording activity completions and managing streak
// milestones.
//
// It sits above the repository.New(tx) boundary and takes a repository.Querier,
// so it can be exercised with a mocked Querier rather than a live database.
// Building the paginated response envelope stays a handler concern.
package streak

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

// Service is the streak domain's contract.
type Service interface {
	RecordActivity(
		ctx context.Context,
		in repository.RecordActivityCompletionParams,
	) (repository.RecordActivityCompletionRow, error)

	CreateMilestone(
		ctx context.Context,
		in repository.CreateStreakMilestoneParams,
	) (repository.StreakMilestone, error)

	// ListActiveMilestones returns the total row count and one page of active
	// milestones. The count is fetched first, matching the two queries this
	// replaced: if it fails, the milestone query is never issued.
	ListActiveMilestones(
		ctx context.Context,
		limit, offset int32,
	) (total int64, rows []repository.StreakMilestone, err error)

	DeleteMilestone(ctx context.Context, id uuid.UUID) error
}

type service struct {
	q repository.Querier
}

// NewService returns a Service backed by q.
func NewService(q repository.Querier) Service {
	return &service{q: q}
}

func (s *service) RecordActivity(
	ctx context.Context,
	in repository.RecordActivityCompletionParams,
) (repository.RecordActivityCompletionRow, error) {
	completed, err := s.q.RecordActivityCompletion(ctx, in)
	if err != nil {
		return repository.RecordActivityCompletionRow{}, fmt.Errorf(
			"%w: record activity completion: %v", core.ErrInternal, err,
		)
	}
	return completed, nil
}

func (s *service) CreateMilestone(
	ctx context.Context,
	in repository.CreateStreakMilestoneParams,
) (repository.StreakMilestone, error) {
	milestone, err := s.q.CreateStreakMilestone(ctx, in)
	if err != nil {
		return repository.StreakMilestone{}, fmt.Errorf(
			"%w: create streak milestone: %v", core.ErrInternal, err,
		)
	}
	return milestone, nil
}

func (s *service) ListActiveMilestones(
	ctx context.Context,
	limit, offset int32,
) (int64, []repository.StreakMilestone, error) {
	total, err := s.q.GetAllActiveStreakMilestoneCount(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf(
			"%w: get active streak milestone count: %v", core.ErrInternal, err,
		)
	}

	active := true
	// sqlc emits items := []StreakMilestone{} for an empty result
	// (emit_empty_slices), so rebuilding the slice would turn that into null
	// on the wire.
	rows, err := s.q.GetAllStreaksMilestoneByActive(
		ctx,
		repository.GetAllStreaksMilestoneByActiveParams{
			Limit: limit, Offset: offset, IsActive: &active,
		},
	)
	if err != nil {
		return 0, nil, fmt.Errorf(
			"%w: list active streak milestones: %v", core.ErrInternal, err,
		)
	}

	return total, rows, nil
}

func (s *service) DeleteMilestone(ctx context.Context, id uuid.UUID) error {
	if err := s.q.DeleteStreakMilestoneByID(ctx, id); err != nil {
		return fmt.Errorf(
			"%w: delete streak milestone: %v", core.ErrInternal, err,
		)
	}
	return nil
}
