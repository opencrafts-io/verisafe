package role

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/repository"
	rolesvc "github.com/opencrafts-io/verisafe/internal/service/role"
)

type RoleHandler struct {
	Cacher core.Cacher
	DB     core.IDBProvider
	Cfg    *config.Config
	Logger *slog.Logger

	// Service builds a role service bound to the caller's transaction. This is
	// the seam that makes the handler testable: a test supplies a factory
	// returning a stub and never needs a Querier, a pgx.Rows, or a database.
	//
	// Left nil it falls back to the real implementation, so a composition root
	// that forgets to wire it still works rather than panicking on first use.
	Service func(repository.Querier) rolesvc.Service
}

// svc builds the service for a transaction, honouring an injected factory.
func (rh *RoleHandler) svc(tx pgx.Tx) rolesvc.Service {
	if rh.Service != nil {
		return rh.Service(repository.New(tx))
	}
	return rolesvc.NewService(repository.New(tx))
}

// Registers all the necessary routes associated with this handler group
func (rh *RoleHandler) RegisterHandlers(router core.Router) {
	router.Handle("POST /roles/create",
		middleware.CreateStack(
			middleware.IsAuthenticated(rh.Cfg, rh.DB, rh.Cacher, rh.Logger),
			middleware.HasPermission([]string{"create:role"}),
		)(core.AppHandler(rh.CreateRole)),
	)

	router.Handle("GET /roles",
		middleware.CreateStack(

			middleware.IsAuthenticated(rh.Cfg, rh.DB, rh.Cacher, rh.Logger),
			middleware.HasPermission([]string{"read:role:any"}),
			middleware.PaginationMiddleware(10, 100),
		)(core.AppHandler(rh.GetAllRoles)),
	)

	router.Handle("GET /roles/{id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(rh.Cfg, rh.DB, rh.Cacher, rh.Logger),
			middleware.HasPermission([]string{"read:role:any"}),
		)(core.AppHandler(rh.GetRoleByID)),
	)

	router.Handle("GET /roles/user/{id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(rh.Cfg, rh.DB, rh.Cacher, rh.Logger),
			middleware.HasPermission([]string{"read:role:any"}),
		)(core.AppHandler(rh.GetAllUserRoles)),
	)

	router.Handle("GET /roles/permissions/{id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(rh.Cfg, rh.DB, rh.Cacher, rh.Logger),
			middleware.HasPermission([]string{"read:role:permissions"}),
		)(core.AppHandler(rh.GetRolePermissions)),
	)

	router.Handle("PATCH /roles/{id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(rh.Cfg, rh.DB, rh.Cacher, rh.Logger),
			middleware.HasPermission([]string{"update:role:any"}),
		)(core.AppHandler(rh.UpdateRole)),
	)

	router.Handle("GET /roles/assign/{user_id}/{role_id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(rh.Cfg, rh.DB, rh.Cacher, rh.Logger),
			middleware.HasPermission([]string{"assign:role:any"}),
		)(core.AppHandler(rh.AssignUserRole)),
	)

	router.Handle("DELETE /roles/revoke/{user_id}/{role_id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(rh.Cfg, rh.DB, rh.Cacher, rh.Logger),
			middleware.HasPermission([]string{"revoke:role:any"}),
		)(core.AppHandler(rh.RevokeUserRole)),
	)
}

// CreateRole godoc
//
// @Summary      Create a role
// @Description  Creates a new role. Roles are the unit RBAC permissions attach
// @Description  to: assigning a role to an account grants that account every
// @Description  permission the role holds.
// @Description  Requires the create:role permission.
// @Tags         roles
// @Accept       json
// @Produce      json
// @Param        request  body      repository.CreateRoleParams  true  "Role to create"
// @Success      201  {object}  repository.Role
// @Failure      400  {object}  core.APIError  "Invalid request body"
// @Failure      500  {object}  core.APIError  "Failed to create role"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /roles/create [post]
func (rh *RoleHandler) CreateRole(w http.ResponseWriter, r *http.Request) error {
	var roleData repository.CreateRoleParams
	if err := json.NewDecoder(r.Body).Decode(&roleData); err != nil {
		rh.Logger.Error("Failed to parse request body", slog.Any("error", err))
		return core.Public(core.ErrInvalidInput, msgCheckBody)
	}

	created, err := core.InTx(
		r.Context(),
		rh.DB,
		func(tx pgx.Tx) (repository.Role, error) {
			role, err := rh.svc(tx).Create(r.Context(), roleData)
			if err != nil {
				rh.Logger.Error("Failed to create role", slog.Any("error", err))
				return repository.Role{}, core.Public(core.ErrInternal, msgRetry)
			}
			return role, nil
		},
	)
	if err != nil {
		return core.Fallback(err, core.ErrInternal, msgGeneric)
	}

	core.WriteJSON(w, http.StatusCreated, created)
	return nil
}

// GetRoleByID godoc
//
// @Summary      Get a role by id
// @Description  Returns a single role by its UUID. Responds 404 when no role
// @Description  carries that id.
// @Description  Requires the read:role:any permission.
// @Tags         roles
// @Produce      json
// @Param        id  path  string  true  "Role ID"
// @Success      200  {object}  repository.Role
// @Failure      400  {object}  core.APIError  "Invalid id"
// @Failure      404  {object}  core.APIError  "Role not found"
// @Failure      500  {object}  core.APIError  "Failed to fetch role"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /roles/{id} [get]
func (rh *RoleHandler) GetRoleByID(w http.ResponseWriter, r *http.Request) error {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		rh.Logger.Error("Failed to parse request body", slog.Any("error", err))
		return core.Public(core.ErrInvalidInput, msgCheckBody)
	}

	role, err := core.InTx(
		r.Context(),
		rh.DB,
		func(tx pgx.Tx) (repository.Role, error) {
			role, err := rh.svc(tx).GetByID(r.Context(), id)
			// Checked before the generic branch, mirroring the ordering this
			// handler had before: a missing row is a 404, not a 500.
			if errors.Is(err, core.ErrNotFound) {
				return repository.Role{}, core.Public(
					core.ErrNotFound, msgRoleNotFound,
				)
			}
			if err != nil {
				rh.Logger.Error(
					"Failed to retrieve role",
					slog.Any("error", err),
					slog.Any("role", id.String()),
				)
				return repository.Role{}, core.Public(core.ErrInternal, msgRetry)
			}
			return role, nil
		},
	)
	if err != nil {
		return core.Fallback(err, core.ErrInternal, msgGeneric)
	}

	core.WriteJSON(w, http.StatusOK, role)
	return nil
}

// GetAllRoles godoc
//
// @Summary      List roles
// @Description  Returns a paginated list of every role defined in the system.
// @Description  Paginated with limit/offset; the response is a bare array, not
// @Description  an envelope.
// @Description  Requires the read:role:any permission.
// @Tags         roles
// @Produce      json
// @Param        limit   query  int  false  "Page size (default 10, max 100)"
// @Param        offset  query  int  false  "Page offset (default 0)"
// @Success      200  {array}   repository.Role
// @Failure      500  {object}  core.APIError  "Failed to fetch roles"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /roles [get]
func (rh *RoleHandler) GetAllRoles(w http.ResponseWriter, r *http.Request) error {
	pagination := middleware.GetPagination(r.Context())

	roles, err := core.InTx(
		r.Context(),
		rh.DB,
		func(tx pgx.Tx) ([]repository.Role, error) {
			roles, err := rh.svc(tx).List(
				r.Context(),
				int32(pagination.Limit),
				int32(pagination.Offset),
			)
			if err != nil {
				rh.Logger.Error(
					"Failed to retrieve roles", slog.Any("error", err),
				)
				return nil, core.Public(core.ErrInternal, msgRetryLater)
			}
			return roles, nil
		},
	)
	if err != nil {
		return core.Fallback(err, core.ErrInternal, msgGeneric)
	}

	core.WriteJSON(w, http.StatusOK, roles)
	return nil
}

// GetAllUserRoles godoc
//
// @Summary      List a given user's roles
// @Description  Returns every role currently assigned to the given account.
// @Description  Requires the read:role:any permission.
// @Tags         roles
// @Produce      json
// @Param        id  path  string  true  "Account ID"
// @Success      200  {array}   repository.UserRolesView
// @Failure      400  {object}  core.APIError  "Invalid id"
// @Failure      500  {object}  core.APIError  "Failed to fetch roles"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /roles/user/{id} [get]
func (rh *RoleHandler) GetAllUserRoles(
	w http.ResponseWriter,
	r *http.Request,
) error {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		rh.Logger.Error("Failed to parse request body", slog.Any("error", err))
		return core.Public(core.ErrInvalidInput, msgCheckBody)
	}

	roles, err := core.InTx(
		r.Context(),
		rh.DB,
		func(tx pgx.Tx) ([]repository.UserRolesView, error) {
			roles, err := rh.svc(tx).ListForUser(r.Context(), id)
			if err != nil {
				rh.Logger.Error(
					"Failed to retrieve roles", slog.Any("error", err),
				)
				return nil, core.Public(core.ErrInternal, msgRetryLater)
			}
			return roles, nil
		},
	)
	if err != nil {
		return core.Fallback(err, core.ErrInternal, msgGeneric)
	}

	core.WriteJSON(w, http.StatusOK, roles)
	return nil
}

// UpdateRole godoc
//
// @Summary      Update a role
// @Description  The path id is the authoritative resource identifier — any id in the request body is overwritten with it.
// @Tags         roles
// @Accept       json
// @Produce      json
// @Param        id       path  string                       true  "Role ID"
// @Param        request  body  repository.UpdateRoleParams  true  "Fields to update"
// @Success      200  {object}  repository.Role
// @Failure      400  {object}  core.APIError  "Invalid id or request body"
// @Failure      500  {object}  core.APIError  "Failed to update role"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /roles/{id} [patch]
func (rh *RoleHandler) UpdateRole(w http.ResponseWriter, r *http.Request) error {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		rh.Logger.Error("Failed to parse request body", slog.Any("error", err))
		return core.Public(core.ErrInvalidInput, msgCheckBody)
	}

	var roleData repository.UpdateRoleParams
	if err := json.NewDecoder(r.Body).Decode(&roleData); err != nil {
		rh.Logger.Error("Failed to parse request body", slog.Any("error", err))
		return core.Public(core.ErrInvalidInput, msgCheckBody)
	}

	// The path segment is the resource identifier; ignore whatever id (if
	// any) was in the request body so callers can't update the wrong role
	// by sending a mismatched body id.
	roleData.ID = id

	updated, err := core.InTx(
		r.Context(),
		rh.DB,
		func(tx pgx.Tx) (repository.Role, error) {
			role, err := rh.svc(tx).Update(r.Context(), roleData)
			if err != nil {
				rh.Logger.Error("Failed to update role", slog.Any("error", err))
				return repository.Role{}, core.Public(core.ErrInternal, msgRetry)
			}
			return role, nil
		},
	)
	if err != nil {
		return core.Fallback(err, core.ErrInternal, msgGeneric)
	}

	core.WriteJSON(w, http.StatusOK, updated)
	return nil
}

// GetRolePermissions godoc
//
// @Summary      List permissions granted to a role
// @Description  Returns every permission attached to the given role. These are
// @Description  the permissions an account inherits when the role is assigned
// @Description  to it.
// @Description  Requires the read:role:permissions permission.
// @Tags         roles
// @Produce      json
// @Param        id  path  string  true  "Role ID"
// @Success      200  {array}   repository.RolePermissionsView
// @Failure      400  {object}  core.APIError  "Invalid id"
// @Failure      500  {object}  core.APIError  "Failed to fetch permissions"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /roles/permissions/{id} [get]
func (rh *RoleHandler) GetRolePermissions(
	w http.ResponseWriter,
	r *http.Request,
) error {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		rh.Logger.Error("Failed to parse request body", slog.Any("error", err))
		return core.Public(core.ErrInvalidInput, msgCheckBody)
	}

	perms, err := core.InTx(
		r.Context(),
		rh.DB,
		func(tx pgx.Tx) ([]repository.RolePermissionsView, error) {
			perms, err := rh.svc(tx).ListPermissions(r.Context(), id)
			if err != nil {
				rh.Logger.Error(
					"Failed to retrieve role permissions",
					slog.Any("error", err),
					slog.Any("role", id.String()),
				)
				return nil, core.Public(core.ErrInternal, msgRetryLater)
			}
			return perms, nil
		},
	)
	if err != nil {
		return core.Fallback(err, core.ErrInternal, msgGeneric)
	}

	core.WriteJSON(w, http.StatusOK, perms)
	return nil
}

// AssignUserRole godoc
//
// @Summary      Assign a role to a user
// @Description  Grants a role to an account, transitively granting the account
// @Description  every permission that role holds. Not idempotent: user_roles is
// @Description  keyed on (user_id, role_id), so re-assigning a role the account
// @Description  already holds fails the primary key and returns 500.
// @Description  Requires the assign:role:any permission.
// @Tags         roles
// @Produce      json
// @Param        user_id  path  string  true  "Account ID"
// @Param        role_id  path  string  true  "Role ID"
// @Success      200  {object}  map[string]any  "Confirmation message"
// @Failure      400  {object}  core.APIError  "Invalid user_id or role_id"
// @Failure      500  {object}  core.APIError  "Failed to assign role"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /roles/assign/{user_id}/{role_id} [get]
func (rh *RoleHandler) AssignUserRole(
	w http.ResponseWriter,
	r *http.Request,
) error {
	userID, roleID, err := rh.pairFromPath(r)
	if err != nil {
		return err
	}

	if err := core.InTxDo(r.Context(), rh.DB, func(tx pgx.Tx) error {
		if err := rh.svc(tx).Assign(r.Context(), userID, roleID); err != nil {
			rh.Logger.Error("Failed to assign role to user",
				slog.Any("error", err),
				slog.Any("role", roleID.String()),
				slog.Any("user", userID.String()),
			)
			return core.Public(core.ErrInternal, msgRetryLater)
		}
		return nil
	}); err != nil {
		return core.Fallback(err, core.ErrInternal, msgGeneric)
	}

	core.WriteJSON(w, http.StatusOK, map[string]any{
		"message": "Role successfully assigned",
	})
	return nil
}

// RevokeUserRole godoc
//
// @Summary      Revoke a role from a user
// @Description  Removes a role from an account, withdrawing every permission
// @Description  the account held only through that role. Permissions the
// @Description  account also holds via another role are unaffected.
// @Description  Requires the revoke:role:any permission.
// @Tags         roles
// @Produce      json
// @Param        user_id  path  string  true  "Account ID"
// @Param        role_id  path  string  true  "Role ID"
// @Success      200  {object}  map[string]any  "Confirmation message"
// @Failure      400  {object}  core.APIError  "Invalid user_id or role_id"
// @Failure      500  {object}  core.APIError  "Failed to revoke role"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /roles/revoke/{user_id}/{role_id} [delete]
func (rh *RoleHandler) RevokeUserRole(
	w http.ResponseWriter,
	r *http.Request,
) error {
	userID, roleID, err := rh.pairFromPath(r)
	if err != nil {
		return err
	}

	if err := core.InTxDo(r.Context(), rh.DB, func(tx pgx.Tx) error {
		if err := rh.svc(tx).Revoke(r.Context(), userID, roleID); err != nil {
			rh.Logger.Error("Failed to revoke role from user",
				slog.Any("error", err),
				slog.Any("role", roleID.String()),
				slog.Any("user", userID.String()),
			)
			return core.Public(core.ErrInternal, msgRetryLater)
		}
		return nil
	}); err != nil {
		return core.Fallback(err, core.ErrInternal, msgGeneric)
	}

	core.WriteJSON(w, http.StatusOK, map[string]any{
		"message": "Role successfully revoked",
	})
	return nil
}

// pairFromPath parses the {user_id}/{role_id} pair the assign and revoke
// routes share. user_id is checked first, matching the order both endpoints
// used before, so a request with both malformed still fails on user_id.
func (rh *RoleHandler) pairFromPath(
	r *http.Request,
) (userID, roleID uuid.UUID, err error) {
	userID, err = uuid.Parse(r.PathValue("user_id"))
	if err != nil {
		rh.Logger.Error("Failed to parse request body", slog.Any("error", err))
		return uuid.Nil, uuid.Nil, core.Public(core.ErrInvalidInput, msgCheckBody)
	}

	roleID, err = uuid.Parse(r.PathValue("role_id"))
	if err != nil {
		rh.Logger.Error("Failed to parse request body", slog.Any("error", err))
		return uuid.Nil, uuid.Nil, core.Public(core.ErrInvalidInput, msgCheckBody)
	}

	return userID, roleID, nil
}
