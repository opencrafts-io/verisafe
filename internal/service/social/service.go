// Package social owns lookups of an account's linked third-party social
// connections.
//
// It sits above the repository.New(tx) boundary and takes a repository.Querier,
// so it can be exercised with a mocked Querier rather than a live database.
// Credential sanitisation (stripping access/refresh tokens before they cross
// the wire) is deliberately not done here: it is a transport concern, applied
// by the handler to whatever the service returns, and stays with
// social.socialResponse.
package social

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

// Service is the social domain's contract. Methods return repository.Social
// verbatim; the handler is responsible for sanitising it before responding.
type Service interface {
	ListForAccount(
		ctx context.Context,
		accountID uuid.UUID,
	) ([]repository.Social, error)
}

type service struct {
	q repository.Querier
}

// NewService returns a Service backed by q.
func NewService(q repository.Querier) Service {
	return &service{q: q}
}

// ListForAccount returns the querier's slice unchanged. sqlc emits
// items := []Social{} for an empty result (emit_empty_slices), so rebuilding
// the slice here would turn that into null on the wire.
func (s *service) ListForAccount(
	ctx context.Context,
	accountID uuid.UUID,
) ([]repository.Social, error) {
	socials, err := s.q.GetAllAccountSocials(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: list social connections: %v", core.ErrInternal, err,
		)
	}
	return socials, nil
}
