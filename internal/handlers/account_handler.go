package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/repository"
	"github.com/opencrafts-io/verisafe/internal/utils"
)

type AccountHandler struct {
	Logger *slog.Logger
}

func (ah *AccountHandler) RegisterHandlers(cfg *config.Config, router *http.ServeMux) {
	router.Handle("POST /accounts/bot/create",
		middleware.CreateStack(
			middleware.IsAuthenticated(cfg),
			middleware.HasPermission([]string{"create:account:any"}),
		)(http.HandlerFunc(ah.CreateBotAccount)),
	)

	router.Handle("GET /accounts/me",
		middleware.CreateStack(
			middleware.IsAuthenticated(cfg),
			middleware.HasPermission([]string{"read:account:own"}),
		)(http.HandlerFunc(ah.GetPersonalAccount)),
	)

}

// Creates a bot account
func (ah *AccountHandler) CreateBotAccount(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		ah.Logger.Error("Error while processing request", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	tx, _ := conn.Begin(r.Context())
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	var accData repository.CreateAccountParams

	if err := json.NewDecoder(r.Body).Decode(&accData); err != nil || accData.Name == "" {
		ah.Logger.Error("Failed to parse request body", slog.Any("error", err))
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Please check your request body and try again",
		})
		return
	}

	accData.Type = repository.AccountTypeBot

	created, err := repo.CreateAccount(r.Context(), accData)
	if err != nil {
		ah.Logger.Error("Failed to create account",
			slog.Any("error", err),
			slog.Any("account", accData),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We couldn't create this account at the moment please try again later",
		})
		return
	}

	if err = tx.Commit(r.Context()); err != nil {
		ah.Logger.Error("Error while committing transaction", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(created)
}

func (ah *AccountHandler) GetPersonalAccount(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(middleware.AuthUserClaims).(*utils.VerisafeClaims)
	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		ah.Logger.Error("Error while processing request", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	tx, _ := conn.Begin(r.Context())
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	user, err := repo.GetAccountByID(r.Context(), claims.Account.ID)
	if errors.Is(err, sql.ErrNoRows) {
		ah.Logger.Error("Error while processing request", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Account does not exist your token might be from a different flavor",
		})
		return

	}
	if err != nil {
		ah.Logger.Error("Error while processing request", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into an error while trying to fetch your account",
		})
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(user)
}
