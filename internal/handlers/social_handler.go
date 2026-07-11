package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

type SocialHandler struct {
	Cacher core.Cacher
	DB     core.IDBProvider
	Cfg    *config.Config
	Logger *slog.Logger
}

func (sh *SocialHandler) RegisterHandlers(
	router *http.ServeMux,
) {
	router.Handle("GET /socials/me",
		middleware.CreateStack(
			middleware.IsAuthenticated(sh.Cfg, sh.DB, sh.Cacher, sh.Logger),
			middleware.HasPermission([]string{"read:account:own"}),
		)(http.HandlerFunc(sh.GetAllUserSocials)),
	)
	router.Handle("GET /socials/user/{user_id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(sh.Cfg, sh.DB, sh.Cacher, sh.Logger),
			middleware.HasPermission([]string{"read:account:any"}),
		)(http.HandlerFunc(sh.GetUserIDSocials)),
	)
}

// GetUserIDSocials godoc
//
// @Summary      List a given user's linked social accounts
// @Tags         socials
// @Produce      json
// @Param        user_id  path  string  true  "Account ID"
// @Success      200  {array}   repository.Social
// @Failure      400  {object}  core.APIError  "Invalid user_id"
// @Failure      500  {object}  core.APIError  "Failed to fetch social connections"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /socials/user/{user_id} [get]
func (sh *SocialHandler) GetUserIDSocials(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set("Content-Type", "application/json")
	rawID := r.PathValue("user_id")
	id, err := uuid.Parse(rawID)
	if err != nil {
		sh.Logger.Error("Failed to parse request body", slog.Any("error", err))
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Please check your request body and try again",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		sh.Logger.Error(
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

	socials, err := repo.GetAllAccountSocials(r.Context(), id)
	if err != nil {
		sh.Logger.Error(
			"Error while processing request",
			slog.Any("error", err),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We couldn't fetch your social login providers at the moment please try again",
		})
		return
	}

	if err = tx.Commit(r.Context()); err != nil {
		sh.Logger.Error(
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
	json.NewEncoder(w).Encode(socials)
}

// GetAllUserSocials godoc
//
// @Summary      List the authenticated user's own linked social accounts
// @Tags         socials
// @Produce      json
// @Success      200  {array}   repository.Social
// @Failure      401  {object}  core.APIError  "Missing or invalid claims"
// @Failure      500  {object}  core.APIError  "Failed to fetch social connections"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /socials/me [get]
func (sh *SocialHandler) GetAllUserSocials(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set("Content-Type", "application/json")

	// Parse the id from the token
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "authentication required"})
		return
	}
	id, err := uuid.Parse(claims.Subject)
	if err != nil {
		sh.Logger.Error(
			"Error while processing request",
			slog.Any("error", err),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Please check your request auth token and try again",
		})
		return
	}

	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		sh.Logger.Error(
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

	socials, err := repo.GetAllAccountSocials(r.Context(), id)
	if err != nil {
		sh.Logger.Error(
			"Error while processing request",
			slog.Any("error", err),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We couldn't fetch your social login providers at the moment please try again",
		})
		return
	}

	if err = tx.Commit(r.Context()); err != nil {
		sh.Logger.Error(
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
	json.NewEncoder(w).Encode(socials)
}
