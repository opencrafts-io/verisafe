package permission

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
	permsvc "github.com/opencrafts-io/verisafe/internal/service/permission"
)

type PermissionHandler struct {
	Cacher core.Cacher
	DB     core.IDBProvider
	Cfg    *config.Config
	Logger *slog.Logger

	// Service builds a permission service bound to the caller's transaction.
	// Left nil it falls back to the real implementation; see the role handler
	// for why this field is the testing seam.
	Service func(repository.Querier) permsvc.Service
}

func (ph *PermissionHandler) svc(tx pgx.Tx) permsvc.Service {
	if ph.Service != nil {
		return ph.Service(repository.New(tx))
	}
	return permsvc.NewService(repository.New(tx))
}

// Registers all the necessary routes associated with this handler group
func (ph *PermissionHandler) RegisterHandlers(router core.Router) {
	router.Handle("POST /permissions/create",
		middleware.CreateStack(
			middleware.IsAuthenticated(ph.Cfg, ph.DB, ph.Cacher, ph.Logger),
			middleware.HasPermission([]string{"create:permission"}),
		)(core.AppHandler(ph.CreatePermission)),
	)

	router.Handle("GET /permissions",
		middleware.CreateStack(
			middleware.IsAuthenticated(ph.Cfg, ph.DB, ph.Cacher, ph.Logger),
			middleware.HasPermission([]string{"read:permission:any"}),
			middleware.PaginationMiddleware(10, 100),
		)(core.AppHandler(ph.GetAllPermissions)),
	)

	router.Handle("GET /permissions/{id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(ph.Cfg, ph.DB, ph.Cacher, ph.Logger),
			middleware.HasPermission([]string{"read:permission:any"}),
		)(core.AppHandler(ph.GetPermissionByID)),
	)

	router.Handle("GET /permissions/user/{id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(ph.Cfg, ph.DB, ph.Cacher, ph.Logger),
			middleware.HasPermission([]string{"read:permission:user"}),
		)(core.AppHandler(ph.GetAllUserPermissions)),
	)

	router.Handle("PATCH /permissions/{id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(ph.Cfg, ph.DB, ph.Cacher, ph.Logger),
			middleware.HasPermission([]string{"update:permission:any"}),
		)(core.AppHandler(ph.UpdatePermission)),
	)

	router.Handle("GET /permissions/assign/{perm_id}/{role_id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(ph.Cfg, ph.DB, ph.Cacher, ph.Logger),
			middleware.HasPermission([]string{"assign:permission:role"}),
		)(core.AppHandler(ph.AssignRolePermission)),
	)

	router.Handle("DELETE /permissions/revoke/{perm_id}/{role_id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(ph.Cfg, ph.DB, ph.Cacher, ph.Logger),
			middleware.HasPermission([]string{"revoke:permission:role"}),
		)(core.AppHandler(ph.RevokeRolePermission)),
	)
}

// CreatePermission godoc
//
// @Summary      Create a permission
// @Description  Newly created permissions are automatically granted to the "system" and "Administrator" roles by database triggers — see docs/RBAC.md.
// @Tags         permissions
// @Accept       json
// @Produce      json
// @Param        request  body      repository.CreatePermissionParams  true  "Permission to create"
// @Success      201  {object}  repository.Permission
// @Failure      400  {object}  core.APIError  "Invalid request body"
// @Failure      500  {object}  core.APIError  "Failed to create permission"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /permissions/create [post]
func (ph *PermissionHandler) CreatePermission(
	w http.ResponseWriter,
	r *http.Request,
) error {
	var permData repository.CreatePermissionParams
	if err := json.NewDecoder(r.Body).Decode(&permData); err != nil {
		ph.Logger.Error("Failed to parse request body", slog.Any("error", err))
		return core.Public(core.ErrInvalidInput, msgCheckBody)
	}

	created, err := core.InTx(
		r.Context(),
		ph.DB,
		func(tx pgx.Tx) (repository.Permission, error) {
			perm, err := ph.svc(tx).Create(r.Context(), permData)
			if err != nil {
				ph.Logger.Error(
					"Failed to create permission", slog.Any("error", err),
				)
				return repository.Permission{}, core.Public(
					core.ErrInternal, msgCreateFailed,
				)
			}
			return perm, nil
		},
	)
	if err != nil {
		return core.Fallback(err, core.ErrInternal, msgGeneric)
	}

	core.WriteJSON(w, http.StatusCreated, created)
	return nil
}

// GetPermissionByID godoc
//
// @Summary      Get a permission by id
// @Description  Returns a single permission by its UUID. Responds 404 when no
// @Description  permission carries that id.
// @Description  Requires the read:permission:any permission.
// @Tags         permissions
// @Produce      json
// @Param        id  path  string  true  "Permission ID"
// @Success      200  {object}  repository.Permission
// @Failure      400  {object}  core.APIError  "Invalid id"
// @Failure      404  {object}  core.APIError  "Permission not found"
// @Failure      500  {object}  core.APIError  "Failed to fetch permission"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /permissions/{id} [get]
func (ph *PermissionHandler) GetPermissionByID(
	w http.ResponseWriter,
	r *http.Request,
) error {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		ph.Logger.Error("Failed to parse request body", slog.Any("error", err))
		return core.Public(core.ErrInvalidInput, msgCheckBody)
	}

	perm, err := core.InTx(
		r.Context(),
		ph.DB,
		func(tx pgx.Tx) (repository.Permission, error) {
			perm, err := ph.svc(tx).GetByID(r.Context(), id)
			// Checked before the generic branch, mirroring the ordering this
			// handler had before: a missing row is a 404, not a 500.
			if errors.Is(err, core.ErrNotFound) {
				return repository.Permission{}, core.Public(
					core.ErrNotFound, msgPermissionNotFound,
				)
			}
			if err != nil {
				ph.Logger.Error(
					"Failed to retrieve permission",
					slog.Any("error", err),
					slog.Any("permission", id.String()),
				)
				return repository.Permission{}, core.Public(
					core.ErrInternal, msgRetryLater,
				)
			}
			return perm, nil
		},
	)
	if err != nil {
		return core.Fallback(err, core.ErrInternal, msgGeneric)
	}

	core.WriteJSON(w, http.StatusOK, perm)
	return nil
}

// GetAllPermissions godoc
//
// @Summary      List permissions
// @Description  Returns a paginated list of every permission defined in the
// @Description  system. Paginated with limit/offset; the response is a bare
// @Description  array, not an envelope.
// @Description  Requires the read:permission:any permission.
// @Tags         permissions
// @Produce      json
// @Param        limit   query  int  false  "Page size (default 10, max 100)"
// @Param        offset  query  int  false  "Page offset (default 0)"
// @Success      200  {array}   repository.Permission
// @Failure      500  {object}  core.APIError  "Failed to fetch permissions"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /permissions [get]
func (ph *PermissionHandler) GetAllPermissions(
	w http.ResponseWriter,
	r *http.Request,
) error {
	pagination := middleware.GetPagination(r.Context())

	perms, err := core.InTx(
		r.Context(),
		ph.DB,
		func(tx pgx.Tx) ([]repository.Permission, error) {
			perms, err := ph.svc(tx).List(
				r.Context(),
				int32(pagination.Limit),
				int32(pagination.Offset),
			)
			if err != nil {
				ph.Logger.Error(
					"Failed to retrieve permissions", slog.Any("error", err),
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

// GetAllUserPermissions godoc
//
// @Summary      List a given user's effective permissions
// @Description  Returns the permissions the account effectively holds, resolved
// @Description  through every role assigned to it. Permissions are never
// @Description  attached to an account directly, only via a role.
// @Description  Requires the read:permission:user permission.
// @Tags         permissions
// @Produce      json
// @Param        id  path  string  true  "Account ID"
// @Success      200  {array}   repository.UserPermissionsView
// @Failure      400  {object}  core.APIError  "Invalid id"
// @Failure      500  {object}  core.APIError  "Failed to fetch permissions"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /permissions/user/{id} [get]
func (ph *PermissionHandler) GetAllUserPermissions(
	w http.ResponseWriter,
	r *http.Request,
) error {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		ph.Logger.Error("Failed to parse request body", slog.Any("error", err))
		return core.Public(core.ErrInvalidInput, msgCheckBody)
	}

	perms, err := core.InTx(
		r.Context(),
		ph.DB,
		func(tx pgx.Tx) ([]repository.UserPermissionsView, error) {
			perms, err := ph.svc(tx).ListForUser(r.Context(), id)
			if err != nil {
				ph.Logger.Error(
					"Failed to retrieve user permissions",
					slog.Any("error", err),
					slog.Any("user", id.String()),
				)
				return nil, core.Public(core.ErrInternal, msgUserPermsFailed)
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

// UpdatePermission godoc
//
// @Summary      Update a permission
// @Description  Updates a permission in place. Every role holding it, and every
// @Description  account holding those roles, sees the change immediately.
// @Description  Requires the update:permission:any permission.
// @Tags         permissions
// @Accept       json
// @Produce      json
// @Param        request  body      repository.UpdatePermissionParams  true  "Fields to update (id identifies the permission)"
// @Success      200  {object}  repository.Permission
// @Failure      400  {object}  core.APIError  "Invalid request body"
// @Failure      500  {object}  core.APIError  "Failed to update permission"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /permissions/{id} [patch]
func (ph *PermissionHandler) UpdatePermission(
	w http.ResponseWriter,
	r *http.Request,
) error {
	// The permission being updated is identified by the id in the request
	// body, not by the {id} path segment, which this endpoint has always
	// ignored. Changing that would alter which resource a mismatched request
	// updates, so it is left alone here and noted for a follow-up.
	var permData repository.UpdatePermissionParams
	if err := json.NewDecoder(r.Body).Decode(&permData); err != nil {
		ph.Logger.Error("Failed to parse request body", slog.Any("error", err))
		return core.Public(core.ErrInvalidInput, msgCheckBody)
	}

	updated, err := core.InTx(
		r.Context(),
		ph.DB,
		func(tx pgx.Tx) (repository.Permission, error) {
			perm, err := ph.svc(tx).Update(r.Context(), permData)
			if err != nil {
				ph.Logger.Error("Failed to update permission",
					slog.Any("error", err),
					slog.Any("permission", permData),
				)
				return repository.Permission{}, core.Public(
					core.ErrInternal, msgRetry,
				)
			}
			return perm, nil
		},
	)
	if err != nil {
		return core.Fallback(err, core.ErrInternal, msgGeneric)
	}

	core.WriteJSON(w, http.StatusOK, updated)
	return nil
}

// AssignRolePermission godoc
//
// @Summary      Grant a permission to a role
// @Description  Attaches a permission to a role, granting it to every account
// @Description  that holds the role. Not idempotent: role_permissions is keyed
// @Description  on (role_id, permission_id), so re-granting a permission the
// @Description  role already holds fails the primary key and returns 500.
// @Description  Requires the assign:permission:role permission.
// @Tags         permissions
// @Produce      json
// @Param        perm_id  path  string  true  "Permission ID"
// @Param        role_id  path  string  true  "Role ID"
// @Success      200  {object}  map[string]any  "Confirmation message"
// @Failure      400  {object}  core.APIError  "Invalid perm_id or role_id"
// @Failure      500  {object}  core.APIError  "Failed to assign permission"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /permissions/assign/{perm_id}/{role_id} [get]
func (ph *PermissionHandler) AssignRolePermission(
	w http.ResponseWriter,
	r *http.Request,
) error {
	permID, roleID, err := ph.pairFromPath(r)
	if err != nil {
		return err
	}

	if err := core.InTxDo(r.Context(), ph.DB, func(tx pgx.Tx) error {
		if err := ph.svc(tx).AssignToRole(r.Context(), permID, roleID); err != nil {
			ph.Logger.Error("Failed to assign permission to role",
				slog.Any("error", err),
				slog.Any("permission", permID.String()),
				slog.Any("role", roleID.String()),
			)
			return core.Public(core.ErrInternal, msgRetryLater)
		}
		return nil
	}); err != nil {
		return core.Fallback(err, core.ErrInternal, msgGeneric)
	}

	core.WriteJSON(w, http.StatusOK, map[string]any{
		"message": "Permission successfully assigned",
	})
	return nil
}

// RevokeRolePermission godoc
//
// @Summary      Revoke a permission from a role
// @Description  Detaches a permission from a role, withdrawing it from every
// @Description  account that held it only through this role. Accounts holding
// @Description  the same permission via another role keep it.
// @Description  Requires the revoke:permission:role permission.
// @Tags         permissions
// @Produce      json
// @Param        perm_id  path  string  true  "Permission ID"
// @Param        role_id  path  string  true  "Role ID"
// @Success      200  {object}  map[string]any  "Confirmation message"
// @Failure      400  {object}  core.APIError  "Invalid perm_id or role_id"
// @Failure      500  {object}  core.APIError  "Failed to revoke permission"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /permissions/revoke/{perm_id}/{role_id} [delete]
func (ph *PermissionHandler) RevokeRolePermission(
	w http.ResponseWriter,
	r *http.Request,
) error {
	permID, roleID, err := ph.pairFromPath(r)
	if err != nil {
		return err
	}

	if err := core.InTxDo(r.Context(), ph.DB, func(tx pgx.Tx) error {
		if err := ph.svc(tx).RevokeFromRole(r.Context(), permID, roleID); err != nil {
			ph.Logger.Error("Failed to revoke permission from role",
				slog.Any("error", err),
				slog.Any("permission", permID.String()),
				slog.Any("role", roleID.String()),
			)
			return core.Public(core.ErrInternal, msgRetryLater)
		}
		return nil
	}); err != nil {
		return core.Fallback(err, core.ErrInternal, msgGeneric)
	}

	core.WriteJSON(w, http.StatusOK, map[string]any{
		"message": "Permission successfully revoked from role",
	})
	return nil
}

// pairFromPath parses the {perm_id}/{role_id} pair the assign and revoke
// routes share.
//
// role_id is validated first, which is the order both endpoints used before
// this refactor and the opposite of the role handler's. It matters only when
// both segments are malformed, but that is still an observable difference, so
// it is preserved rather than tidied.
func (ph *PermissionHandler) pairFromPath(
	r *http.Request,
) (permID, roleID uuid.UUID, err error) {
	roleID, err = uuid.Parse(r.PathValue("role_id"))
	if err != nil {
		ph.Logger.Error("Failed to parse request body", slog.Any("error", err))
		return uuid.Nil, uuid.Nil, core.Public(core.ErrInvalidInput, msgCheckBody)
	}

	permID, err = uuid.Parse(r.PathValue("perm_id"))
	if err != nil {
		ph.Logger.Error("Failed to parse request body", slog.Any("error", err))
		return uuid.Nil, uuid.Nil, core.Public(core.ErrInvalidInput, msgCheckBody)
	}

	return permID, roleID, nil
}
