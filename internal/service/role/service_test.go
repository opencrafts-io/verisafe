package role_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/repository"
	mockQuerier "github.com/opencrafts-io/verisafe/internal/repository/mocks"
	"github.com/opencrafts-io/verisafe/internal/service/role"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// The service takes a repository.Querier rather than a transaction, so these
// run against the generated mock with no database at all. That seam is the
// point of the extraction: before it, none of this was reachable without a
// live Postgres.

func newService(t *testing.T) (role.Service, *mockQuerier.MockQuerier) {
	t.Helper()
	q := mockQuerier.NewMockQuerier(gomock.NewController(t))
	return role.NewService(q), q
}

func TestGetByID(t *testing.T) {
	id := uuid.New()

	t.Run("a missing row is ErrNotFound, not an internal error", func(t *testing.T) {
		svc, q := newService(t)
		q.EXPECT().GetRoleByID(gomock.Any(), id).
			Return(repository.Role{}, pgx.ErrNoRows)

		_, err := svc.GetByID(context.Background(), id)

		// The distinction matters: core.HandleError maps these to 404 and 500
		// respectively, so collapsing them would turn a missing role into a
		// server error.
		assert.ErrorIs(t, err, core.ErrNotFound)
		assert.NotErrorIs(t, err, core.ErrInternal)
	})

	t.Run("any other driver error is internal", func(t *testing.T) {
		svc, q := newService(t)
		q.EXPECT().GetRoleByID(gomock.Any(), id).
			Return(repository.Role{}, errors.New("connection reset"))

		_, err := svc.GetByID(context.Background(), id)

		assert.ErrorIs(t, err, core.ErrInternal)
		assert.NotErrorIs(t, err, core.ErrNotFound)
	})

	t.Run("the row is returned unchanged", func(t *testing.T) {
		svc, q := newService(t)
		want := repository.Role{ID: id, Name: "admin"}
		q.EXPECT().GetRoleByID(gomock.Any(), id).Return(want, nil)

		got, err := svc.GetByID(context.Background(), id)

		require.NoError(t, err)
		assert.Equal(t, want, got)
	})
}

// sqlc is configured with emit_empty_slices, so an empty result is []Role{}
// and the endpoint serialises []. If the service rebuilt the slice with
// `var out []T; append(...)` an empty result would marshal to null instead --
// a silent wire change for every client iterating the response.
func TestList_EmptyResultStaysAnEmptySliceNotNil(t *testing.T) {
	svc, q := newService(t)
	q.EXPECT().
		GetAllRoles(gomock.Any(), repository.GetAllRolesParams{Limit: 10, Offset: 0}).
		Return([]repository.Role{}, nil)

	got, err := svc.List(context.Background(), 10, 0)

	require.NoError(t, err)
	assert.NotNil(t, got, "an empty result must marshal to [] and not null")
	assert.Empty(t, got)
}

func TestList_PassesPaginationThroughUnchanged(t *testing.T) {
	svc, q := newService(t)
	q.EXPECT().
		GetAllRoles(gomock.Any(), repository.GetAllRolesParams{Limit: 25, Offset: 50}).
		Return([]repository.Role{{Name: "a"}}, nil)

	got, err := svc.List(context.Background(), 25, 50)

	require.NoError(t, err)
	assert.Len(t, got, 1)
}

func TestListForUser_EmptyResultStaysAnEmptySlice(t *testing.T) {
	svc, q := newService(t)
	userID := uuid.New()
	q.EXPECT().GetAllUserRoles(gomock.Any(), userID).
		Return([]repository.UserRolesView{}, nil)

	got, err := svc.ListForUser(context.Background(), userID)

	require.NoError(t, err)
	assert.NotNil(t, got)
}

func TestListPermissions_EmptyResultStaysAnEmptySlice(t *testing.T) {
	svc, q := newService(t)
	roleID := uuid.New()
	q.EXPECT().GetRolePermissions(gomock.Any(), roleID).
		Return([]repository.RolePermissionsView{}, nil)

	got, err := svc.ListPermissions(context.Background(), roleID)

	require.NoError(t, err)
	assert.NotNil(t, got)
}

func TestCreate_WrapsFailureAsInternal(t *testing.T) {
	svc, q := newService(t)
	in := repository.CreateRoleParams{Name: "admin"}
	q.EXPECT().CreateRole(gomock.Any(), in).
		Return(repository.Role{}, errors.New("duplicate key"))

	_, err := svc.Create(context.Background(), in)

	assert.ErrorIs(t, err, core.ErrInternal)
}

func TestUpdate_MissingRowIsNotFound(t *testing.T) {
	svc, q := newService(t)
	in := repository.UpdateRoleParams{ID: uuid.New(), Name: "renamed"}
	q.EXPECT().UpdateRole(gomock.Any(), in).
		Return(repository.Role{}, pgx.ErrNoRows)

	_, err := svc.Update(context.Background(), in)

	assert.ErrorIs(t, err, core.ErrNotFound)
}

// Assign is deliberately not idempotent: user_roles is keyed on
// (user_id, role_id), so a repeat assignment violates the primary key. The
// endpoint returned 500 for that before the extraction and still does; making
// it idempotent would be a behaviour change, not a refactor.
func TestAssign_RepeatAssignmentSurfacesAsInternal(t *testing.T) {
	svc, q := newService(t)
	userID, roleID := uuid.New(), uuid.New()

	q.EXPECT().
		AssignRole(gomock.Any(), repository.AssignRoleParams{
			UserID: userID, RoleID: roleID,
		}).
		Return(repository.UserRole{}, errors.New(
			`ERROR: duplicate key value violates unique constraint "user_roles_pkey"`,
		))

	err := svc.Assign(context.Background(), userID, roleID)

	assert.ErrorIs(t, err, core.ErrInternal)
}

func TestAssignAndRevoke_MapArgumentsOntoTheRightParams(t *testing.T) {
	userID, roleID := uuid.New(), uuid.New()

	t.Run("assign", func(t *testing.T) {
		svc, q := newService(t)
		q.EXPECT().
			AssignRole(gomock.Any(), repository.AssignRoleParams{
				UserID: userID, RoleID: roleID,
			}).
			Return(repository.UserRole{}, nil)

		assert.NoError(t, svc.Assign(context.Background(), userID, roleID))
	})

	// Guards against the two uuid arguments being transposed, which the
	// compiler cannot catch because both are uuid.UUID.
	t.Run("revoke", func(t *testing.T) {
		svc, q := newService(t)
		q.EXPECT().
			RevokeRole(gomock.Any(), repository.RevokeRoleParams{
				UserID: userID, RoleID: roleID,
			}).
			Return(nil)

		assert.NoError(t, svc.Revoke(context.Background(), userID, roleID))
	})
}
