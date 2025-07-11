package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

type PermissionHandler struct {
	Logger *slog.Logger
}

// Creates a permission
func (ph *PermissionHandler) CreatePermission(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		ph.Logger.Error("Error while processing request", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	tx, _ := conn.Begin(r.Context())
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	var permData repository.CreatePermissionParams

	if err := json.NewDecoder(r.Body).Decode(&permData); err != nil {
		ph.Logger.Error("Failed to parse request body", slog.Any("error", err))
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Please check your request body and try again",
		})
		return
	}

	created, err := repo.CreatePermission(r.Context(), permData)
	if err != nil {
		ph.Logger.Error("Failed to create permission",
			slog.Any("error", err),
			slog.Any("permission", permData),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We couldn't create this permission at the moment please try again later",
		})
		return
	}

	if err = tx.Commit(r.Context()); err != nil {
		ph.Logger.Error("Error while committing transaction", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(created)
}

// Retrieves a permission by it's ID
func (ph *PermissionHandler) GetPermissionByID(w http.ResponseWriter, r *http.Request) {
	rawID := r.PathValue("id")
	id, err := uuid.Parse(rawID)
	if err != nil {
		ph.Logger.Error("Failed to parse request body", slog.Any("error", err))
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Please check your request body and try again",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		ph.Logger.Error("Error while processing request", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	tx, _ := conn.Begin(r.Context())
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	role, err := repo.GetPermissionByID(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "The permission you are requesting does not exist",
		})
		return
	}
	if err != nil {
		ph.Logger.Error("Failed to retrieve permission",
			slog.Any("error", err), slog.Any("role", id.String()),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We couldn't complete this request at the moment please try again later",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(role)

}

// Retrieves all permissions in the system
func (ph *PermissionHandler) GetAllPermissions(w http.ResponseWriter, r *http.Request) {

	pagination := middleware.GetPagination(r.Context())

	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		ph.Logger.Error("Error while processing request", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	tx, _ := conn.Begin(r.Context())
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	roles, err := repo.GetAllPermissions(r.Context(), repository.GetAllPermissionsParams{
		Limit:  int32(pagination.Limit),
		Offset: int32(pagination.Offset),
	})
	if err != nil {
		ph.Logger.Error("Failed to retrieve permissions", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We couldn't complete this request at the moment please try again later",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(roles)

}

// Retrieves all permissions associated
func (ph *PermissionHandler) GetAllUserPermissions(w http.ResponseWriter, r *http.Request) {
	rawID := r.PathValue("id")
	id, err := uuid.Parse(rawID)
	if err != nil {
		ph.Logger.Error("Failed to parse request body", slog.Any("error", err))
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Please check your request body and try again",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		ph.Logger.Error("Error while processing request", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	tx, _ := conn.Begin(r.Context())
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	roles, err := repo.GetUserPermissions(r.Context(), id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		ph.Logger.Error("Failed to retrieve permissions", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We ran into an issue while retrieving this user's permissions try again later",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(roles)

}

// Updates a permission
func (ph *PermissionHandler) UpdatePermission(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		ph.Logger.Error("Error while processing request", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	tx, _ := conn.Begin(r.Context())
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	var permData repository.UpdatePermissionParams

	if err := json.NewDecoder(r.Body).Decode(&permData); err != nil {
		ph.Logger.Error("Failed to parse request body", slog.Any("error", err))
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Please check your request body and try again",
		})
		return
	}

	created, err := repo.UpdatePermission(r.Context(), permData)
	if err != nil {
		ph.Logger.Error("Failed to update permission",
			slog.Any("error", err),
			slog.Any("permission", permData),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We couldn't complete this request at the moment please try again",
		})
		return
	}

	if err = tx.Commit(r.Context()); err != nil {
		ph.Logger.Error("Error while committing transaction", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(created)
}

// Some work might be needed to check for both the assign and revoke permission
// better error handling
func (ph *PermissionHandler) AssignRolePermission(w http.ResponseWriter, r *http.Request) {
	rawRoleID := r.PathValue("role_id")
	roleID, err := uuid.Parse(rawRoleID)
	if err != nil {
		ph.Logger.Error("Failed to parse request body", slog.Any("error", err))
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Please check your request body and try again",
		})
		return
	}

	rawPermID := r.PathValue("perm_id")
	permID, err := uuid.Parse(rawPermID)
	if err != nil {
		ph.Logger.Error("Failed to parse request body", slog.Any("error", err))
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Please check your request body and try again",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		ph.Logger.Error("Error while processing request", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	tx, _ := conn.Begin(r.Context())
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	_, err = repo.AssignRolePermission(r.Context(), repository.AssignRolePermissionParams{
		PermissionID: permID,
		RoleID:       roleID,
	})
	if err != nil {
		ph.Logger.Error("Failed to assign permission to role",
			slog.Any("error", err),
			slog.Any("role", roleID.String()),
			slog.Any("permission", permID.String()),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We couldn't complete this request at the moment please try again later",
		})
		return
	}

	if err = tx.Commit(r.Context()); err != nil {
		ph.Logger.Error("Error while committing transaction", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{"message": "Permission successfully assigned"})

}

func (ph *PermissionHandler) RevokeRolePermission(w http.ResponseWriter, r *http.Request) {
	rawRoleID := r.PathValue("role_id")
	roleID, err := uuid.Parse(rawRoleID)
	if err != nil {
		ph.Logger.Error("Failed to parse request body", slog.Any("error", err))
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Please check your request body and try again",
		})
		return
	}

	rawPermID := r.PathValue("perm_id")
	permID, err := uuid.Parse(rawPermID)
	if err != nil {
		ph.Logger.Error("Failed to parse request body", slog.Any("error", err))
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Please check your request body and try again",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		ph.Logger.Error("Error while processing request", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	tx, _ := conn.Begin(r.Context())
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	err = repo.RevokeRolePermission(r.Context(), repository.RevokeRolePermissionParams{
		PermissionID: permID,
		RoleID:       roleID,
	})
	if err != nil {
		ph.Logger.Error("Failed to revoke permission from role",
			slog.Any("error", err),
			slog.Any("role", roleID.String()),
			slog.Any("permission", permID.String()),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We couldn't complete this request at the moment please try again later",
		})
		return
	}

	if err = tx.Commit(r.Context()); err != nil {
		ph.Logger.Error("Error while committing transaction", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{"message": "Permission successfully revoked from role"})

}
