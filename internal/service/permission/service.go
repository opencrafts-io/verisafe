// Package permission owns the business rules for RBAC permissions: creating
// them, reading them, and attaching or detaching them from roles.
//
// It sits above the repository.New(tx) boundary and takes a repository.Querier,
// so it can be exercised with a mocked Querier rather than a live database.
package permission

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

// Service is the permission domain's contract.
//
// As with the role service, methods return the sqlc row types verbatim because
// those structs' JSON is the existing wire contract.
type Service interface {
	Create(
		ctx context.Context,
		in repository.CreatePermissionParams,
	) (repository.Permission, error)

	GetByID(ctx context.Context, id uuid.UUID) (repository.Permission, error)

	List(
		ctx context.Context,
		limit, offset int32,
	) ([]repository.Permission, error)

	// ListForUser returns the permissions an account effectively holds,
	// resolved through every role assigned to it. Permissions are never
	// attached to an account directly.
	ListForUser(
		ctx context.Context,
		userID uuid.UUID,
	) ([]repository.UserPermissionsView, error)

	Update(
		ctx context.Context,
		in repository.UpdatePermissionParams,
	) (repository.Permission, error)

	AssignToRole(ctx context.Context, permissionID, roleID uuid.UUID) error

	RevokeFromRole(ctx context.Context, permissionID, roleID uuid.UUID) error
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
	in repository.CreatePermissionParams,
) (repository.Permission, error) {
	perm, err := s.q.CreatePermission(ctx, in)
	if err != nil {
		return repository.Permission{}, fmt.Errorf(
			"%w: create permission: %v", core.ErrInternal, err,
		)
	}
	return perm, nil
}

func (s *service) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (repository.Permission, error) {
	perm, err := s.q.GetPermissionByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return repository.Permission{}, core.ErrNotFound
	}
	if err != nil {
		return repository.Permission{}, fmt.Errorf(
			"%w: look up permission: %v", core.ErrInternal, err,
		)
	}
	return perm, nil
}

// List returns the querier's slice unchanged; see the role service for why
// rebuilding it would turn an empty result from [] into null.
func (s *service) List(
	ctx context.Context,
	limit, offset int32,
) ([]repository.Permission, error) {
	perms, err := s.q.GetAllPermissions(ctx, repository.GetAllPermissionsParams{
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: list permissions: %v", core.ErrInternal, err)
	}
	return perms, nil
}

func (s *service) ListForUser(
	ctx context.Context,
	userID uuid.UUID,
) ([]repository.UserPermissionsView, error) {
	perms, err := s.q.GetUserPermissions(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: list permissions for user: %v", core.ErrInternal, err,
		)
	}
	return perms, nil
}

func (s *service) Update(
	ctx context.Context,
	in repository.UpdatePermissionParams,
) (repository.Permission, error) {
	perm, err := s.q.UpdatePermission(ctx, in)
	if errors.Is(err, pgx.ErrNoRows) {
		return repository.Permission{}, core.ErrNotFound
	}
	if err != nil {
		return repository.Permission{}, fmt.Errorf(
			"%w: update permission: %v", core.ErrInternal, err,
		)
	}
	return perm, nil
}

// AssignToRole is not idempotent: role_permissions is keyed on
// (role_id, permission_id), so re-granting a permission the role already holds
// violates the primary key and surfaces as ErrInternal, matching what the
// endpoint returned before this service existed.
func (s *service) AssignToRole(
	ctx context.Context,
	permissionID, roleID uuid.UUID,
) error {
	_, err := s.q.AssignRolePermission(
		ctx,
		repository.AssignRolePermissionParams{
			RoleID:       roleID,
			PermissionID: permissionID,
		},
	)
	if err != nil {
		return fmt.Errorf("%w: assign permission to role: %v", core.ErrInternal, err)
	}
	return nil
}

func (s *service) RevokeFromRole(
	ctx context.Context,
	permissionID, roleID uuid.UUID,
) error {
	if err := s.q.RevokeRolePermission(
		ctx,
		repository.RevokeRolePermissionParams{
			RoleID:       roleID,
			PermissionID: permissionID,
		},
	); err != nil {
		return fmt.Errorf(
			"%w: revoke permission from role: %v", core.ErrInternal, err,
		)
	}
	return nil
}
