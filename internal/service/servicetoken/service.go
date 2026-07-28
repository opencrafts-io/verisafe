// Package servicetoken owns the business rules for service (machine-to-machine)
// tokens: verifying an account is eligible to hold one, and their full CRUD
// plus rotation and admin cleanup.
//
// It sits above the repository.New(tx) boundary and takes a repository.Querier,
// so it can be exercised with a mocked Querier rather than a live database.
// Token generation (crypto/rand) and request-shape validation (scope syntax,
// IP format, user-agent regex) stay handler concerns: they touch no I/O and
// were already pure functions before this extraction.
package servicetoken

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

// ErrNotBotAccount is returned by VerifyBotAccount when the account exists but
// is not a bot account. Wrapping core.ErrForbidden lets core.HandleError map
// it to 403 without the handler needing to know this package's types.
var ErrNotBotAccount = fmt.Errorf(
	"%w: only bot accounts can hold service tokens", core.ErrForbidden,
)

// Service is the service-token domain's contract.
type Service interface {
	// VerifyBotAccount looks up the account and confirms it is a bot account.
	// Returns core.ErrNotFound if the account does not exist, or
	// ErrNotBotAccount if it exists but is not a bot account.
	VerifyBotAccount(
		ctx context.Context,
		accountID uuid.UUID,
	) (repository.Account, error)

	Create(
		ctx context.Context,
		in repository.CreateServiceTokenParams,
	) (repository.ServiceToken, error)

	// GetByID returns the bare driver error unwrapped from any not-found
	// detection: the handler treats every error from this call as
	// "token not found", matching the endpoint's behaviour before this
	// extraction, so there is nothing for the service to classify.
	GetByID(ctx context.Context, id uuid.UUID) (repository.ServiceToken, error)

	List(
		ctx context.Context,
		accountID uuid.UUID,
	) ([]repository.ServiceToken, error)

	ListAllActive(ctx context.Context) ([]repository.ActiveServiceToken, error)

	Update(ctx context.Context, in repository.UpdateServiceTokenParams) error

	Rotate(ctx context.Context, in repository.RotateServiceTokenParams) error

	Revoke(ctx context.Context, id uuid.UUID) error

	Stats(
		ctx context.Context,
		accountID uuid.UUID,
	) (repository.GetServiceTokenUsageStatsRow, error)

	CleanupExpired(ctx context.Context) error
}

type service struct {
	q repository.Querier
}

// NewService returns a Service backed by q.
func NewService(q repository.Querier) Service {
	return &service{q: q}
}

func (s *service) VerifyBotAccount(
	ctx context.Context,
	accountID uuid.UUID,
) (repository.Account, error) {
	account, err := s.q.GetAccountByID(ctx, accountID)
	if err != nil {
		return repository.Account{}, fmt.Errorf(
			"%w: get account: %v", core.ErrNotFound, err,
		)
	}
	if account.Type != repository.AccountTypeBot {
		return repository.Account{}, ErrNotBotAccount
	}
	return account, nil
}

func (s *service) Create(
	ctx context.Context,
	in repository.CreateServiceTokenParams,
) (repository.ServiceToken, error) {
	token, err := s.q.CreateServiceToken(ctx, in)
	if err != nil {
		return repository.ServiceToken{}, fmt.Errorf(
			"%w: create service token: %v", core.ErrInternal, err,
		)
	}
	return token, nil
}

func (s *service) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (repository.ServiceToken, error) {
	token, err := s.q.GetServiceTokenByID(ctx, id)
	if err != nil {
		return repository.ServiceToken{}, fmt.Errorf(
			"%w: get service token: %v", core.ErrInternal, err,
		)
	}
	return token, nil
}

// List returns the querier's slice unchanged. sqlc emits
// items := []ServiceToken{} for an empty result (emit_empty_slices), so
// rebuilding the slice would turn that into null on the wire.
func (s *service) List(
	ctx context.Context,
	accountID uuid.UUID,
) ([]repository.ServiceToken, error) {
	tokens, err := s.q.ListServiceTokensByAccount(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: list service tokens: %v", core.ErrInternal, err,
		)
	}
	return tokens, nil
}

func (s *service) ListAllActive(
	ctx context.Context,
) ([]repository.ActiveServiceToken, error) {
	tokens, err := s.q.ListActiveServiceTokens(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: list active service tokens: %v", core.ErrInternal, err,
		)
	}
	return tokens, nil
}

func (s *service) Update(
	ctx context.Context,
	in repository.UpdateServiceTokenParams,
) error {
	if err := s.q.UpdateServiceToken(ctx, in); err != nil {
		return fmt.Errorf("%w: update service token: %v", core.ErrInternal, err)
	}
	return nil
}

func (s *service) Rotate(
	ctx context.Context,
	in repository.RotateServiceTokenParams,
) error {
	if err := s.q.RotateServiceToken(ctx, in); err != nil {
		return fmt.Errorf("%w: rotate service token: %v", core.ErrInternal, err)
	}
	return nil
}

func (s *service) Revoke(ctx context.Context, id uuid.UUID) error {
	if err := s.q.RevokeServiceToken(ctx, id); err != nil {
		return fmt.Errorf("%w: revoke service token: %v", core.ErrInternal, err)
	}
	return nil
}

func (s *service) Stats(
	ctx context.Context,
	accountID uuid.UUID,
) (repository.GetServiceTokenUsageStatsRow, error) {
	stats, err := s.q.GetServiceTokenUsageStats(ctx, accountID)
	if err != nil {
		return repository.GetServiceTokenUsageStatsRow{}, fmt.Errorf(
			"%w: get service token stats: %v", core.ErrInternal, err,
		)
	}
	return stats, nil
}

func (s *service) CleanupExpired(ctx context.Context) error {
	if err := s.q.CleanupExpiredServiceTokens(ctx); err != nil {
		return fmt.Errorf(
			"%w: cleanup expired service tokens: %v", core.ErrInternal, err,
		)
	}
	return nil
}
