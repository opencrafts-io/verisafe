// Package institution owns the business rules for the institution catalog and
// its links to accounts.
//
// It sits above the repository.New(tx) boundary and takes a repository.Querier,
// so it can be exercised with a mocked Querier rather than a live database.
// Ownership checks (does the caller own the account_id in the request body,
// or hold the admin permission) stay a handler concern, since they read
// request context rather than the database. The two backfill endpoints
// (FanoutInstitutions, which needs the raw pool for a worker-per-connection
// fan-out, and the worker-pool choreography inside
// FanoutInstitutionConnections) are out of scope for this service; only
// FanoutInstitutionConnections's single batched list query is exposed here.
package institution

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

// Service is the institution domain's contract.
type Service interface {
	Create(
		ctx context.Context,
		in repository.CreateInstitutionParams,
	) (repository.Institution, error)

	Update(
		ctx context.Context,
		in repository.UpdateInstitutionParams,
	) (repository.Institution, error)

	// GetByID returns the bare driver error unwrapped from any not-found
	// detection: the handler treats every error from this call as 404,
	// matching the endpoint's behaviour before this extraction, so there is
	// nothing for the service to classify.
	GetByID(ctx context.Context, id int32) (repository.Institution, error)

	List(
		ctx context.Context,
		limit, offset int32,
	) ([]repository.Institution, error)

	Delete(ctx context.Context, id int32) error

	SearchByName(
		ctx context.Context,
		name string,
		limit, offset int32,
	) ([]repository.Institution, error)

	AddAccount(
		ctx context.Context,
		in repository.AddAccountInstitutionParams,
	) (repository.AddAccountInstitutionRow, error)

	RemoveAccount(
		ctx context.Context,
		in repository.RemoveAccountInstitutionParams,
	) error

	ListForAccount(
		ctx context.Context,
		accountID uuid.UUID,
		limit, offset int32,
	) ([]repository.Institution, error)

	ListAccountsForInstitution(
		ctx context.Context,
		institutionID int32,
		limit, offset int32,
	) ([]repository.Account, error)

	// ListConnectionsBatch returns one page of account/institution links, used
	// by FanoutInstitutionConnections to page through the whole table without
	// loading it into memory at once. The worker-pool choreography around
	// this call stays in the handler.
	ListConnectionsBatch(
		ctx context.Context,
		limit, offset int32,
	) ([]repository.AccountInstitution, error)
}

type service struct {
	q repository.Querier
}

// NewService returns a Service backed by q.
func NewService(q repository.Querier) Service {
	return &service{q: q}
}

func (s *service) Create(
	ctx context.Context,
	in repository.CreateInstitutionParams,
) (repository.Institution, error) {
	inst, err := s.q.CreateInstitution(ctx, in)
	if err != nil {
		return repository.Institution{}, fmt.Errorf(
			"%w: create institution: %v", core.ErrInternal, err,
		)
	}
	return inst, nil
}

func (s *service) Update(
	ctx context.Context,
	in repository.UpdateInstitutionParams,
) (repository.Institution, error) {
	inst, err := s.q.UpdateInstitution(ctx, in)
	if err != nil {
		return repository.Institution{}, fmt.Errorf(
			"%w: update institution: %v", core.ErrInternal, err,
		)
	}
	return inst, nil
}

func (s *service) GetByID(
	ctx context.Context,
	id int32,
) (repository.Institution, error) {
	inst, err := s.q.GetInstitution(ctx, id)
	if err != nil {
		return repository.Institution{}, fmt.Errorf(
			"%w: get institution: %v", core.ErrInternal, err,
		)
	}
	return inst, nil
}

// List returns the querier's slice unchanged. sqlc emits items := []Institution{}
// for an empty result (emit_empty_slices), so rebuilding the slice would turn
// that into null on the wire.
func (s *service) List(
	ctx context.Context,
	limit, offset int32,
) ([]repository.Institution, error) {
	insts, err := s.q.ListInstitutions(ctx, repository.ListInstitutionsParams{
		Limit: limit, Offset: offset,
	})
	if err != nil {
		return nil, fmt.Errorf(
			"%w: list institutions: %v", core.ErrInternal, err,
		)
	}
	return insts, nil
}

func (s *service) Delete(ctx context.Context, id int32) error {
	if err := s.q.DeleteInstitution(ctx, id); err != nil {
		return fmt.Errorf("%w: delete institution: %v", core.ErrInternal, err)
	}
	return nil
}

func (s *service) SearchByName(
	ctx context.Context,
	name string,
	limit, offset int32,
) ([]repository.Institution, error) {
	insts, err := s.q.SearchInstitutionsByName(
		ctx,
		repository.SearchInstitutionsByNameParams{
			Name: name, Limit: limit, Offset: offset,
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: search institutions: %v", core.ErrInternal, err,
		)
	}
	return insts, nil
}

func (s *service) AddAccount(
	ctx context.Context,
	in repository.AddAccountInstitutionParams,
) (repository.AddAccountInstitutionRow, error) {
	row, err := s.q.AddAccountInstitution(ctx, in)
	if err != nil {
		return repository.AddAccountInstitutionRow{}, fmt.Errorf(
			"%w: add account institution: %v", core.ErrInternal, err,
		)
	}
	return row, nil
}

func (s *service) RemoveAccount(
	ctx context.Context,
	in repository.RemoveAccountInstitutionParams,
) error {
	if err := s.q.RemoveAccountInstitution(ctx, in); err != nil {
		return fmt.Errorf(
			"%w: remove account institution: %v", core.ErrInternal, err,
		)
	}
	return nil
}

func (s *service) ListForAccount(
	ctx context.Context,
	accountID uuid.UUID,
	limit, offset int32,
) ([]repository.Institution, error) {
	insts, err := s.q.ListInstitutionsForAccount(
		ctx,
		repository.ListInstitutionsForAccountParams{
			AccountID: accountID, Limit: limit, Offset: offset,
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: list institutions for account: %v", core.ErrInternal, err,
		)
	}
	return insts, nil
}

func (s *service) ListAccountsForInstitution(
	ctx context.Context,
	institutionID int32,
	limit, offset int32,
) ([]repository.Account, error) {
	accounts, err := s.q.ListAccountsForInstitution(
		ctx,
		repository.ListAccountsForInstitutionParams{
			InstitutionID: institutionID, Limit: limit, Offset: offset,
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: list accounts for institution: %v", core.ErrInternal, err,
		)
	}
	return accounts, nil
}

func (s *service) ListConnectionsBatch(
	ctx context.Context,
	limit, offset int32,
) ([]repository.AccountInstitution, error) {
	conns, err := s.q.ListInstitutionConnections(
		ctx,
		repository.ListInstitutionConnectionsParams{
			Limit: limit, Offset: offset,
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: list institution connections: %v", core.ErrInternal, err,
		)
	}
	return conns, nil
}
