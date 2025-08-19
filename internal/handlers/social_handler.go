package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/repository"
	"github.com/opencrafts-io/verisafe/internal/utils"
)

type SocialHandler struct {
	Logger *slog.Logger
}

func (sh *SocialHandler) RegisterRoutes(cfg *config.Config, router *http.ServeMux) {
	router.Handle("GET /socials/me",
		middleware.CreateStack(
			middleware.IsAuthenticated(cfg, sh.Logger),
			middleware.HasPermission([]string{"read:account:own"}),
		)(http.HandlerFunc(sh.GetAllUserSocials)),
	)
	router.Handle("GET /socials/user/{user_id}",
		middleware.CreateStack(
			middleware.IsAuthenticated(cfg, sh.Logger),
			middleware.HasPermission([]string{"read:account:any"}),
		)(http.HandlerFunc(sh.GetUserIDSocials)),
	)

}

// Returns all user socials accounts specified by id
func (sh *SocialHandler) GetUserIDSocials(w http.ResponseWriter, r *http.Request) {
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
		sh.Logger.Error("Error while processing request", slog.Any("error", err))
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
		sh.Logger.Error("Error while processing request", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We couldn't fetch your social login providers at the moment please try again",
		})
		return
	}

	if err = tx.Commit(r.Context()); err != nil {
		sh.Logger.Error("Error while committing transaction", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(socials)

}

// Returns all user social accounts
func (sh *SocialHandler) GetAllUserSocials(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Parse the id from the token
	claims := r.Context().Value(middleware.AuthUserClaims).(*utils.VerisafeClaims)
	id, err := uuid.Parse(claims.Subject)
	if err != nil {
		sh.Logger.Error("Error while processing request", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Please check your request auth token and try again",
		})
		return
	}

	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		sh.Logger.Error("Error while processing request", slog.Any("error", err))
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
		sh.Logger.Error("Error while processing request", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We couldn't fetch your social login providers at the moment please try again",
		})
		return
	}

	if err = tx.Commit(r.Context()); err != nil {
		sh.Logger.Error("Error while committing transaction", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(socials)
}
