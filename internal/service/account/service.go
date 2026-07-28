// Package account owns the business rules for accounts: creating a bot
// account (which also assigns its role and issues its service token, spanning
// three tables in one transaction), personal account lookups and edits,
// search, soft-deletion, and the admin fanout backfill's data access.
//
// It sits above the repository.New(tx) boundary and takes a repository.Querier,
// so it can be exercised with a mocked Querier rather than a live database.
// Token generation (crypto/rand) stays a handler concern, as it does in the
// service-token package, since it touches no I/O; the raw token is passed in
// already generated, and this package is responsible for hashing it before
// storage.
package account

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/repository"
	"github.com/opencrafts-io/verisafe/internal/tokens"
)

// Service is the account domain's contract.
type Service interface {
	Create(
		ctx context.Context,
		in repository.CreateAccountParams,
	) (repository.Account, error)

	// GetBotRole looks up the "bot" role, used when creating a bot account.
	GetBotRole(ctx context.Context) (repository.Role, error)

	AssignRole(ctx context.Context, userID, roleID uuid.UUID) error

	// CreateServiceToken hashes rawToken before persisting it. Callers must
	// not pre-hash it themselves.
	CreateServiceToken(
		ctx context.Context,
		rawToken string,
		in repository.CreateServiceTokenParams,
	) (repository.ServiceToken, error)

	// GetByID returns core.ErrNotFound specifically when the row does not
	// exist (a real errors.Is check, unlike several other handlers' "any
	// error is not-found" quirk), so the handler can pick between its two
	// distinct messages for that case and for a genuine driver failure.
	GetByID(ctx context.Context, id uuid.UUID) (repository.Account, error)

	Update(
		ctx context.Context,
		in repository.UpdateAccountDetailsParams,
	) error

	UpdatePhone(
		ctx context.Context,
		in repository.UpdateAccountPhoneNumberParams,
	) error

	SearchByEmail(
		ctx context.Context,
		email string,
		limit, offset int32,
	) ([]repository.Account, error)

	SearchByName(
		ctx context.Context,
		name string,
		limit, offset int32,
	) ([]repository.Account, error)

	SearchByUsername(
		ctx context.Context,
		username string,
		limit, offset int32,
	) ([]repository.Account, error)

	List(
		ctx context.Context,
		limit, offset int32,
	) ([]repository.Account, error)

	MarkForDeletion(ctx context.Context, id uuid.UUID) error

	MarkForRecovery(ctx context.Context, id uuid.UUID) error

	// Count and ListBatch back FanoutAccouts's batched worker-pool read.
	Count(ctx context.Context) (int64, error)

	ListBatch(
		ctx context.Context,
		limit, offset int32,
	) ([]repository.Account, error)
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
	in repository.CreateAccountParams,
) (repository.Account, error) {
	created, err := s.q.CreateAccount(ctx, in)
	if err != nil {
		return repository.Account{}, fmt.Errorf(
			"%w: create account: %v", core.ErrInternal, err,
		)
	}
	return created, nil
}

func (s *service) GetBotRole(ctx context.Context) (repository.Role, error) {
	role, err := s.q.GetRoleByName(ctx, "bot")
	if err != nil {
		return repository.Role{}, fmt.Errorf(
			"%w: get bot role: %v", core.ErrInternal, err,
		)
	}
	return role, nil
}

func (s *service) AssignRole(
	ctx context.Context,
	userID, roleID uuid.UUID,
) error {
	if _, err := s.q.AssignRole(ctx, repository.AssignRoleParams{
		UserID: userID, RoleID: roleID,
	}); err != nil {
		return fmt.Errorf("%w: assign role: %v", core.ErrInternal, err)
	}
	return nil
}

func (s *service) CreateServiceToken(
	ctx context.Context,
	rawToken string,
	in repository.CreateServiceTokenParams,
) (repository.ServiceToken, error) {
	in.TokenHash = tokens.HashToken(rawToken)
	created, err := s.q.CreateServiceToken(ctx, in)
	if err != nil {
		return repository.ServiceToken{}, fmt.Errorf(
			"%w: create service token: %v", core.ErrInternal, err,
		)
	}
	return created, nil
}

func (s *service) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (repository.Account, error) {
	account, err := s.q.GetAccountByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return repository.Account{}, core.ErrNotFound
	}
	if err != nil {
		return repository.Account{}, fmt.Errorf(
			"%w: get account: %v", core.ErrInternal, err,
		)
	}
	return account, nil
}

func (s *service) Update(
	ctx context.Context,
	in repository.UpdateAccountDetailsParams,
) error {
	if err := s.q.UpdateAccountDetails(ctx, in); err != nil {
		return fmt.Errorf("%w: update account: %v", core.ErrInternal, err)
	}
	return nil
}

func (s *service) UpdatePhone(
	ctx context.Context,
	in repository.UpdateAccountPhoneNumberParams,
) error {
	if err := s.q.UpdateAccountPhoneNumber(ctx, in); err != nil {
		return fmt.Errorf("%w: update account phone: %v", core.ErrInternal, err)
	}
	return nil
}

// SearchByEmail, SearchByName and SearchByUsername return the querier's slice
// unchanged. sqlc emits items := []Account{} for an empty result
// (emit_empty_slices), so rebuilding the slice would turn that into null on
// the wire.

func (s *service) SearchByEmail(
	ctx context.Context,
	email string,
	limit, offset int32,
) ([]repository.Account, error) {
	accounts, err := s.q.SearchAccountByEmail(ctx, repository.SearchAccountByEmailParams{
		Email: email, Limit: limit, Offset: offset,
	})
	if err != nil {
		return nil, fmt.Errorf(
			"%w: search accounts by email: %v", core.ErrInternal, err,
		)
	}
	return accounts, nil
}

func (s *service) SearchByName(
	ctx context.Context,
	name string,
	limit, offset int32,
) ([]repository.Account, error) {
	accounts, err := s.q.SearchAccountByName(ctx, repository.SearchAccountByNameParams{
		Name: name, Limit: limit, Offset: offset,
	})
	if err != nil {
		return nil, fmt.Errorf(
			"%w: search accounts by name: %v", core.ErrInternal, err,
		)
	}
	return accounts, nil
}

func (s *service) SearchByUsername(
	ctx context.Context,
	username string,
	limit, offset int32,
) ([]repository.Account, error) {
	accounts, err := s.q.SearchAccountByUsername(ctx, repository.SearchAccountByUsernameParams{
		Username: username, Limit: limit, Offset: offset,
	})
	if err != nil {
		return nil, fmt.Errorf(
			"%w: search accounts by username: %v", core.ErrInternal, err,
		)
	}
	return accounts, nil
}

func (s *service) List(
	ctx context.Context,
	limit, offset int32,
) ([]repository.Account, error) {
	accounts, err := s.q.GetAllAccounts(ctx, repository.GetAllAccountsParams{
		Limit: limit, Offset: offset,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: list accounts: %v", core.ErrInternal, err)
	}
	return accounts, nil
}

func (s *service) MarkForDeletion(ctx context.Context, id uuid.UUID) error {
	if err := s.q.MarkAccountForDeletion(ctx, id); err != nil {
		return fmt.Errorf(
			"%w: mark account for deletion: %v", core.ErrInternal, err,
		)
	}
	return nil
}

func (s *service) MarkForRecovery(ctx context.Context, id uuid.UUID) error {
	if err := s.q.MarkAccountForRecovery(ctx, id); err != nil {
		return fmt.Errorf(
			"%w: mark account for recovery: %v", core.ErrInternal, err,
		)
	}
	return nil
}

func (s *service) Count(ctx context.Context) (int64, error) {
	count, err := s.q.GetAccountsCount(ctx)
	if err != nil {
		return 0, fmt.Errorf("%w: get accounts count: %v", core.ErrInternal, err)
	}
	return count, nil
}

func (s *service) ListBatch(
	ctx context.Context,
	limit, offset int32,
) ([]repository.Account, error) {
	accounts, err := s.q.GetAllAccounts(ctx, repository.GetAllAccountsParams{
		Limit: limit, Offset: offset,
	})
	if err != nil {
		return nil, fmt.Errorf(
			"%w: list accounts batch: %v", core.ErrInternal, err,
		)
	}
	return accounts, nil
}
