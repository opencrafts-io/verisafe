package permission_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/repository"
	mockQuerier "github.com/opencrafts-io/verisafe/internal/repository/mocks"
	"github.com/opencrafts-io/verisafe/internal/service/permission"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func newService(t *testing.T) (permission.Service, *mockQuerier.MockQuerier) {
	t.Helper()
	q := mockQuerier.NewMockQuerier(gomock.NewController(t))
	return permission.NewService(q), q
}

func TestGetByID(t *testing.T) {
	id := uuid.New()

	t.Run("a missing row is ErrNotFound, not an internal error", func(t *testing.T) {
		svc, q := newService(t)
		q.EXPECT().GetPermissionByID(gomock.Any(), id).
			Return(repository.Permission{}, pgx.ErrNoRows)

		_, err := svc.GetByID(context.Background(), id)

		assert.ErrorIs(t, err, core.ErrNotFound)
		assert.NotErrorIs(t, err, core.ErrInternal)
	})

	t.Run("any other driver error is internal", func(t *testing.T) {
		svc, q := newService(t)
		q.EXPECT().GetPermissionByID(gomock.Any(), id).
			Return(repository.Permission{}, errors.New("connection reset"))

		_, err := svc.GetByID(context.Background(), id)

		assert.ErrorIs(t, err, core.ErrInternal)
		assert.NotErrorIs(t, err, core.ErrNotFound)
	})

	t.Run("the row is returned unchanged", func(t *testing.T) {
		svc, q := newService(t)
		want := repository.Permission{ID: id, Name: "read:role:any"}
		q.EXPECT().GetPermissionByID(gomock.Any(), id).Return(want, nil)

		got, err := svc.GetByID(context.Background(), id)

		require.NoError(t, err)
		assert.Equal(t, want, got)
	})
}

// See the role service for why this matters: rebuilding the slice would turn
// an empty result from [] into null on the wire.
func TestList_EmptyResultStaysAnEmptySliceNotNil(t *testing.T) {
	svc, q := newService(t)
	q.EXPECT().
		GetAllPermissions(gomock.Any(), repository.GetAllPermissionsParams{
			Limit: 10, Offset: 0,
		}).
		Return([]repository.Permission{}, nil)

	got, err := svc.List(context.Background(), 10, 0)

	require.NoError(t, err)
	assert.NotNil(t, got, "an empty result must marshal to [] and not null")
	assert.Empty(t, got)
}

func TestList_PassesPaginationThroughUnchanged(t *testing.T) {
	svc, q := newService(t)
	q.EXPECT().
		GetAllPermissions(gomock.Any(), repository.GetAllPermissionsParams{
			Limit: 25, Offset: 50,
		}).
		Return([]repository.Permission{{Name: "a"}}, nil)

	got, err := svc.List(context.Background(), 25, 50)

	require.NoError(t, err)
	assert.Len(t, got, 1)
}

func TestListForUser_EmptyResultStaysAnEmptySlice(t *testing.T) {
	svc, q := newService(t)
	userID := uuid.New()
	q.EXPECT().GetUserPermissions(gomock.Any(), userID).
		Return([]repository.UserPermissionsView{}, nil)

	got, err := svc.ListForUser(context.Background(), userID)

	require.NoError(t, err)
	assert.NotNil(t, got)
}

func TestCreate_WrapsFailureAsInternal(t *testing.T) {
	svc, q := newService(t)
	in := repository.CreatePermissionParams{Name: "read:thing"}
	q.EXPECT().CreatePermission(gomock.Any(), in).
		Return(repository.Permission{}, errors.New("duplicate key"))

	_, err := svc.Create(context.Background(), in)

	assert.ErrorIs(t, err, core.ErrInternal)
}

func TestUpdate_MissingRowIsNotFound(t *testing.T) {
	svc, q := newService(t)
	in := repository.UpdatePermissionParams{ID: uuid.New(), Name: "renamed"}
	q.EXPECT().UpdatePermission(gomock.Any(), in).
		Return(repository.Permission{}, pgx.ErrNoRows)

	_, err := svc.Update(context.Background(), in)

	assert.ErrorIs(t, err, core.ErrNotFound)
}

// The service takes (permissionID, roleID) but the sqlc params struct orders
// them the other way round. Both are uuid.UUID, so the compiler cannot catch a
// transposition; these assert the mapping explicitly.
func TestAssignAndRevoke_MapArgumentsOntoTheRightParams(t *testing.T) {
	permID, roleID := uuid.New(), uuid.New()

	t.Run("assign", func(t *testing.T) {
		svc, q := newService(t)
		q.EXPECT().
			AssignRolePermission(gomock.Any(), repository.AssignRolePermissionParams{
				RoleID: roleID, PermissionID: permID,
			}).
			Return(repository.RolePermission{}, nil)

		assert.NoError(t, svc.AssignToRole(context.Background(), permID, roleID))
	})

	t.Run("revoke", func(t *testing.T) {
		svc, q := newService(t)
		q.EXPECT().
			RevokeRolePermission(gomock.Any(), repository.RevokeRolePermissionParams{
				RoleID: roleID, PermissionID: permID,
			}).
			Return(nil)

		assert.NoError(t, svc.RevokeFromRole(context.Background(), permID, roleID))
	})
}

// role_permissions is keyed on (role_id, permission_id), so a repeat grant
// violates the primary key. The endpoint returned 500 for that before the
// extraction and still does.
func TestAssignToRole_RepeatGrantSurfacesAsInternal(t *testing.T) {
	svc, q := newService(t)
	permID, roleID := uuid.New(), uuid.New()

	q.EXPECT().
		AssignRolePermission(gomock.Any(), gomock.Any()).
		Return(repository.RolePermission{}, errors.New(
			`ERROR: duplicate key value violates unique constraint "role_permissions_pkey"`,
		))

	err := svc.AssignToRole(context.Background(), permID, roleID)

	assert.ErrorIs(t, err, core.ErrInternal)
}
