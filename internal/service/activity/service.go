// Package activity owns the business rules for the activity catalog: listing
// definitions (all, active-only, inactive-only), a given account's completions
// against them, and creating/updating/deleting a definition.
//
// It sits above the repository.New(tx) boundary and takes a repository.Querier,
// so it can be exercised with a mocked Querier rather than a live database.
// Building the paginated response envelope stays a handler concern, since it
// needs the *http.Request to construct next/previous links.
package activity

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

// Service is the activity domain's contract.
type Service interface {
	List(
		ctx context.Context,
		limit, offset int32,
	) (total int64, rows []repository.Activity, err error)

	ListActive(
		ctx context.Context,
		limit, offset int32,
	) (total int64, rows []repository.Activity, err error)

	ListInactive(
		ctx context.Context,
		limit, offset int32,
	) (total int64, rows []repository.Activity, err error)

	ListCompletionsForUser(
		ctx context.Context,
		accountID uuid.UUID,
		limit, offset int32,
	) (total int64, rows []repository.ActivityCompletion, err error)

	Create(
		ctx context.Context,
		in repository.CreateActivityParams,
	) (repository.Activity, error)

	Update(
		ctx context.Context,
		in repository.UpdateActivityParams,
	) (repository.Activity, error)

	Delete(ctx context.Context, id uuid.UUID) error
}

type service struct {
	q repository.Querier
}

// NewService returns a Service backed by q.
func NewService(q repository.Querier) Service {
	return &service{q: q}
}

// List, ListActive and ListInactive each run the same count-then-page shape as
// the handler methods they replace: the count query first, so the row query is
// never issued if it fails, and the row slice returned unchanged -- sqlc emits
// items := []Activity{} for an empty result (emit_empty_slices), so rebuilding
// it would turn that into null on the wire.

func (s *service) List(
	ctx context.Context,
	limit, offset int32,
) (int64, []repository.Activity, error) {
	total, err := s.q.GetAllActivitiesCount(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf(
			"%w: get activity count: %v", core.ErrInternal, err,
		)
	}

	rows, err := s.q.GetAllActivities(ctx, repository.GetAllActivitiesParams{
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return 0, nil, fmt.Errorf(
			"%w: list activities: %v", core.ErrInternal, err,
		)
	}

	return total, rows, nil
}

func (s *service) ListActive(
	ctx context.Context,
	limit, offset int32,
) (int64, []repository.Activity, error) {
	total, err := s.q.GetAllActiveActivitiesCount(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf(
			"%w: get active activity count: %v", core.ErrInternal, err,
		)
	}

	rows, err := s.q.GetAllActiveActivities(
		ctx,
		repository.GetAllActiveActivitiesParams{Limit: limit, Offset: offset},
	)
	if err != nil {
		return 0, nil, fmt.Errorf(
			"%w: list active activities: %v", core.ErrInternal, err,
		)
	}

	return total, rows, nil
}

func (s *service) ListInactive(
	ctx context.Context,
	limit, offset int32,
) (int64, []repository.Activity, error) {
	total, err := s.q.GetAllInactiveActivitiesCount(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf(
			"%w: get inactive activity count: %v", core.ErrInternal, err,
		)
	}

	rows, err := s.q.GetAllInactiveActivities(
		ctx,
		repository.GetAllInactiveActivitiesParams{Limit: limit, Offset: offset},
	)
	if err != nil {
		return 0, nil, fmt.Errorf(
			"%w: list inactive activities: %v", core.ErrInternal, err,
		)
	}

	return total, rows, nil
}

func (s *service) ListCompletionsForUser(
	ctx context.Context,
	accountID uuid.UUID,
	limit, offset int32,
) (int64, []repository.ActivityCompletion, error) {
	total, err := s.q.GetAllUserActivityCompletionsCount(ctx, accountID)
	if err != nil {
		return 0, nil, fmt.Errorf(
			"%w: get completion count: %v", core.ErrInternal, err,
		)
	}

	rows, err := s.q.GetAllUserActivityCompletions(
		ctx,
		repository.GetAllUserActivityCompletionsParams{
			AccountID: accountID,
			Limit:     limit,
			Offset:    offset,
		},
	)
	if err != nil {
		return 0, nil, fmt.Errorf(
			"%w: list completions: %v", core.ErrInternal, err,
		)
	}

	return total, rows, nil
}

func (s *service) Create(
	ctx context.Context,
	in repository.CreateActivityParams,
) (repository.Activity, error) {
	activity, err := s.q.CreateActivity(ctx, in)
	if err != nil {
		return repository.Activity{}, fmt.Errorf(
			"%w: create activity: %v", core.ErrInternal, err,
		)
	}
	return activity, nil
}

func (s *service) Update(
	ctx context.Context,
	in repository.UpdateActivityParams,
) (repository.Activity, error) {
	activity, err := s.q.UpdateActivity(ctx, in)
	if err != nil {
		return repository.Activity{}, fmt.Errorf(
			"%w: update activity: %v", core.ErrInternal, err,
		)
	}
	return activity, nil
}

func (s *service) Delete(ctx context.Context, id uuid.UUID) error {
	if err := s.q.DeleteActivity(ctx, id); err != nil {
		return fmt.Errorf("%w: delete activity: %v", core.ErrInternal, err)
	}
	return nil
}
