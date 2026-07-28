// Package role owns the business rules for RBAC roles: creating them, reading
// them, and attaching or detaching them from accounts.
//
// It sits above the repository.New(tx) boundary and takes a repository.Querier,
// so it can be exercised with a mocked Querier rather than a live database. The
// handler above it is left holding transport concerns only.
package role

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

// Service is the role domain's contract.
//
// Methods return the sqlc row types verbatim rather than a hand-written DTO.
// The response bytes these endpoints emit today are exactly those structs'
// JSON, and maintaining field-for-field parity by hand would be the largest
// source of silent drift available. New endpoints should define their own
// output types; these carry an existing wire contract.
type Service interface {
	Create(
		ctx context.Context,
		in repository.CreateRoleParams,
	) (repository.Role, error)

	GetByID(ctx context.Context, id uuid.UUID) (repository.Role, error)

	List(ctx context.Context, limit, offset int32) ([]repository.Role, error)

	ListForUser(
		ctx context.Context,
		userID uuid.UUID,
	) ([]repository.UserRolesView, error)

	ListPermissions(
		ctx context.Context,
		roleID uuid.UUID,
	) ([]repository.RolePermissionsView, error)

	Update(
		ctx context.Context,
		in repository.UpdateRoleParams,
	) (repository.Role, error)

	Assign(ctx context.Context, userID, roleID uuid.UUID) error

	Revoke(ctx context.Context, userID, roleID uuid.UUID) error
}

type service struct {
	q repository.Querier
}

// NewService returns a Service backed by q. Pass a transaction-scoped Querier
// in production and a mock in tests; the signature is deliberately narrow
// enough to be used as a factory value.
func NewService(q repository.Querier) Service {
	return &service{q: q}
}

func (s *service) Create(
	ctx context.Context,
	in repository.CreateRoleParams,
) (repository.Role, error) {
	role, err := s.q.CreateRole(ctx, in)
	if err != nil {
		return repository.Role{}, fmt.Errorf("%w: create role: %v", core.ErrInternal, err)
	}
	return role, nil
}

func (s *service) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (repository.Role, error) {
	role, err := s.q.GetRoleByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return repository.Role{}, core.ErrNotFound
	}
	if err != nil {
		return repository.Role{}, fmt.Errorf("%w: look up role: %v", core.ErrInternal, err)
	}
	return role, nil
}

// List returns the querier's slice unchanged. sqlc is configured with
// emit_empty_slices, so it returns []Role{} for an empty result and the
// endpoint serialises []. Rebuilding the slice here would turn that into null.
func (s *service) List(
	ctx context.Context,
	limit, offset int32,
) ([]repository.Role, error) {
	roles, err := s.q.GetAllRoles(ctx, repository.GetAllRolesParams{
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: list roles: %v", core.ErrInternal, err)
	}
	return roles, nil
}

func (s *service) ListForUser(
	ctx context.Context,
	userID uuid.UUID,
) ([]repository.UserRolesView, error) {
	roles, err := s.q.GetAllUserRoles(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("%w: list roles for user: %v", core.ErrInternal, err)
	}
	return roles, nil
}

func (s *service) ListPermissions(
	ctx context.Context,
	roleID uuid.UUID,
) ([]repository.RolePermissionsView, error) {
	perms, err := s.q.GetRolePermissions(ctx, roleID)
	if err != nil {
		return nil, fmt.Errorf("%w: list role permissions: %v", core.ErrInternal, err)
	}
	return perms, nil
}

func (s *service) Update(
	ctx context.Context,
	in repository.UpdateRoleParams,
) (repository.Role, error) {
	role, err := s.q.UpdateRole(ctx, in)
	if errors.Is(err, pgx.ErrNoRows) {
		return repository.Role{}, core.ErrNotFound
	}
	if err != nil {
		return repository.Role{}, fmt.Errorf("%w: update role: %v", core.ErrInternal, err)
	}
	return role, nil
}

// Assign is not idempotent: user_roles is keyed on (user_id, role_id), so
// re-assigning a role the account already holds violates the primary key. That
// surfaces as ErrInternal, matching what the endpoint returned before this
// service existed. Making it idempotent is a behaviour change, not a refactor.
func (s *service) Assign(ctx context.Context, userID, roleID uuid.UUID) error {
	_, err := s.q.AssignRole(ctx, repository.AssignRoleParams{
		UserID: userID,
		RoleID: roleID,
	})
	if err != nil {
		return fmt.Errorf("%w: assign role: %v", core.ErrInternal, err)
	}
	return nil
}

func (s *service) Revoke(ctx context.Context, userID, roleID uuid.UUID) error {
	if err := s.q.RevokeRole(ctx, repository.RevokeRoleParams{
		UserID: userID,
		RoleID: roleID,
	}); err != nil {
		return fmt.Errorf("%w: revoke role: %v", core.ErrInternal, err)
	}
	return nil
}
