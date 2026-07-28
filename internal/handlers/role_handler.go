package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

type RoleHandler struct {
	Cacher core.Cacher
	DB     core.IDBProvider
	Cfg    *config.Config
	Logger *slog.Logger
}

// Registers all the necessary routes associated with this handler group
func (rh *RoleHandler) RegisterHandlers(
	router *http.ServeMux,
) {
	router.Handle("POST /roles/create",
		middleware.CreateStack(
			middleware.IsAuthenticated(rh.Cfg, rh.DB, rh.Cacher, rh.Logger),
			middleware.HasPermission([]string{"create:role"}),
		)(http.HandlerFunc(rh.CreateRole)),
	)

	router.Handle("GET /roles",
		middleware.CreateStack(

			middleware.IsAuthenticated(rh.Cfg, rh.DB, rh.Cacher, rh.Logger),
			middleware.HasPermission([]string{"read:role:any"}),
			middleware.PaginationMiddleware(10, 100),
		)(http.HandlerFunc(rh.GetAllRoles)),
	)

	router.Handle("GET /roles/{id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(rh.Cfg, rh.DB, rh.Cacher, rh.Logger),
			middleware.HasPermission([]string{"read:role:any"}),
		)(http.HandlerFunc(rh.GetRoleByID)),
	)

	router.Handle("GET /roles/user/{id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(rh.Cfg, rh.DB, rh.Cacher, rh.Logger),
			middleware.HasPermission([]string{"read:role:any"}),
		)(http.HandlerFunc(rh.GetAllUserRoles)),
	)

	router.Handle("GET /roles/permissions/{id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(rh.Cfg, rh.DB, rh.Cacher, rh.Logger),
			middleware.HasPermission([]string{"read:role:permissions"}),
		)(http.HandlerFunc(rh.GetRolePermissions)),
	)

	router.Handle("PATCH /roles/{id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(rh.Cfg, rh.DB, rh.Cacher, rh.Logger),
			middleware.HasPermission([]string{"update:role:any"}),
		)(http.HandlerFunc(rh.UpdateRole)),
	)

	router.Handle("GET /roles/assign/{user_id}/{role_id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(rh.Cfg, rh.DB, rh.Cacher, rh.Logger),
			middleware.HasPermission([]string{"assign:role:any"}),
		)(http.HandlerFunc(rh.AssignUserRole)),
	)

	router.Handle("DELETE /roles/revoke/{user_id}/{role_id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(rh.Cfg, rh.DB, rh.Cacher, rh.Logger),
			middleware.HasPermission([]string{"revoke:role:any"}),
		)(http.HandlerFunc(rh.RevokeUserRole)),
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
func (rh *RoleHandler) CreateRole(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		rh.Logger.Error(
			"Error while processing request",
			slog.Any("error", err),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	tx, _ := conn.Begin(r.Context())
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	var roleData repository.CreateRoleParams

	if err := json.NewDecoder(r.Body).Decode(&roleData); err != nil {
		rh.Logger.Error("Failed to parse request body", slog.Any("error", err))
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Please check your request body and try again",
		})
		return
	}

	created, err := repo.CreateRole(r.Context(), roleData)
	if err != nil {
		rh.Logger.Error("Failed to create role", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We couldn't complete this request at the moment please try again",
		})
		return
	}

	if err = tx.Commit(r.Context()); err != nil {
		rh.Logger.Error(
			"Error while committing transaction",
			slog.Any("error", err),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(created)
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
func (rh *RoleHandler) GetRoleByID(w http.ResponseWriter, r *http.Request) {
	rawID := r.PathValue("id")
	id, err := uuid.Parse(rawID)
	if err != nil {
		rh.Logger.Error("Failed to parse request body", slog.Any("error", err))
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Please check your request body and try again",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		rh.Logger.Error(
			"Error while processing request",
			slog.Any("error", err),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	tx, _ := conn.Begin(r.Context())
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	role, err := repo.GetRoleByID(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "The role you are requesting does not exist",
		})
		return
	}
	if err != nil {
		rh.Logger.Error(
			"Failed to retrieve role",
			slog.Any("error", err),
			slog.Any("role", id.String()),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We couldn't complete this request at the moment please try again",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(role)
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
func (rh *RoleHandler) GetAllRoles(w http.ResponseWriter, r *http.Request) {
	pagination := middleware.GetPagination(r.Context())

	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		rh.Logger.Error(
			"Error while processing request",
			slog.Any("error", err),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	tx, _ := conn.Begin(r.Context())
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	roles, err := repo.GetAllRoles(r.Context(), repository.GetAllRolesParams{
		Limit:  int32(pagination.Limit),
		Offset: int32(pagination.Offset),
	})
	if err != nil {
		rh.Logger.Error("Failed to retrieve roles", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We couldn't complete this request at the moment please try again later",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(roles)
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
func (rh *RoleHandler) GetAllUserRoles(w http.ResponseWriter, r *http.Request) {
	rawID := r.PathValue("id")
	id, err := uuid.Parse(rawID)
	if err != nil {
		rh.Logger.Error("Failed to parse request body", slog.Any("error", err))
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Please check your request body and try again",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		rh.Logger.Error(
			"Error while processing request",
			slog.Any("error", err),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	tx, _ := conn.Begin(r.Context())
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	roles, err := repo.GetAllUserRoles(r.Context(), id)
	if err != nil {
		rh.Logger.Error("Failed to retrieve roles", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We couldn't complete this request at the moment please try again later",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(roles)
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
func (rh *RoleHandler) UpdateRole(w http.ResponseWriter, r *http.Request) {
	rawID := r.PathValue("id")
	id, err := uuid.Parse(rawID)
	if err != nil {
		rh.Logger.Error("Failed to parse request body", slog.Any("error", err))
		core.WriteError(w, http.StatusBadRequest, "Please check your request body and try again")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		rh.Logger.Error(
			"Error while processing request",
			slog.Any("error", err),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	tx, _ := conn.Begin(r.Context())
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	var roleData repository.UpdateRoleParams

	if err := json.NewDecoder(r.Body).Decode(&roleData); err != nil {
		rh.Logger.Error("Failed to parse request body", slog.Any("error", err))
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Please check your request body and try again",
		})
		return
	}
	// The path segment is the resource identifier; ignore whatever id (if
	// any) was in the request body so callers can't update the wrong role
	// by sending a mismatched body id.
	roleData.ID = id

	created, err := repo.UpdateRole(r.Context(), roleData)
	if err != nil {
		rh.Logger.Error("Failed to update role", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We couldn't complete this request at the moment please try again",
		})
		return
	}

	if err = tx.Commit(r.Context()); err != nil {
		rh.Logger.Error(
			"Error while committing transaction",
			slog.Any("error", err),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(created)
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
) {
	rawID := r.PathValue("id")
	id, err := uuid.Parse(rawID)
	if err != nil {
		rh.Logger.Error("Failed to parse request body", slog.Any("error", err))
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Please check your request body and try again",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		rh.Logger.Error(
			"Error while processing request",
			slog.Any("error", err),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	tx, _ := conn.Begin(r.Context())
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	roles, err := repo.GetRolePermissions(r.Context(), id)
	if err != nil {
		rh.Logger.Error(
			"Failed to retrieve role permissions",
			slog.Any("error", err),
			slog.Any("role", id.String()),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We couldn't complete this request at the moment please try again later",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(roles)
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
func (rh *RoleHandler) AssignUserRole(w http.ResponseWriter, r *http.Request) {
	rawUserID := r.PathValue("user_id")
	userID, err := uuid.Parse(rawUserID)
	if err != nil {
		rh.Logger.Error("Failed to parse request body", slog.Any("error", err))
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Please check your request body and try again",
		})
		return
	}

	rawRoleID := r.PathValue("role_id")
	roleID, err := uuid.Parse(rawRoleID)
	if err != nil {
		rh.Logger.Error("Failed to parse request body", slog.Any("error", err))
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Please check your request body and try again",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		rh.Logger.Error(
			"Error while processing request",
			slog.Any("error", err),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	tx, _ := conn.Begin(r.Context())
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	_, err = repo.AssignRole(r.Context(), repository.AssignRoleParams{
		UserID: userID,
		RoleID: roleID,
	})
	if err != nil {
		rh.Logger.Error("Failed to assign role to user",
			slog.Any("error", err),
			slog.Any("role", roleID.String()),
			slog.Any("user", userID.String()),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We couldn't complete this request at the moment please try again later",
		})
		return
	}

	if err = tx.Commit(r.Context()); err != nil {
		rh.Logger.Error(
			"Error while committing transaction",
			slog.Any("error", err),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).
		Encode(map[string]any{"message": "Role successfully assigned"})
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
func (rh *RoleHandler) RevokeUserRole(w http.ResponseWriter, r *http.Request) {
	rawUserID := r.PathValue("user_id")
	userID, err := uuid.Parse(rawUserID)
	if err != nil {
		rh.Logger.Error("Failed to parse request body", slog.Any("error", err))
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Please check your request body and try again",
		})
		return
	}

	rawRoleID := r.PathValue("role_id")
	roleID, err := uuid.Parse(rawRoleID)
	if err != nil {
		rh.Logger.Error("Failed to parse request body", slog.Any("error", err))
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Please check your request body and try again",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		rh.Logger.Error(
			"Error while processing request",
			slog.Any("error", err),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	tx, _ := conn.Begin(r.Context())
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	err = repo.RevokeRole(r.Context(), repository.RevokeRoleParams{
		UserID: userID,
		RoleID: roleID,
	})
	if err != nil {
		rh.Logger.Error("Failed to revoke role from user",
			slog.Any("error", err),
			slog.Any("role", roleID.String()),
			slog.Any("user", userID.String()),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We couldn't complete this request at the moment please try again later",
		})
		return
	}

	if err = tx.Commit(r.Context()); err != nil {
		rh.Logger.Error(
			"Error while committing transaction",
			slog.Any("error", err),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).
		Encode(map[string]any{"message": "Role successfully revoked"})
}
