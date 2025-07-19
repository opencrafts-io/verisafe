package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/repository"
	"github.com/opencrafts-io/verisafe/internal/utils"
)

type AccountHandler struct {
	Logger *slog.Logger
	Cfg    *config.Config
}

func (ah *AccountHandler) RegisterHandlers(router *http.ServeMux) {
	router.Handle("POST /accounts/bot/create",
		middleware.CreateStack(
			middleware.IsAuthenticated(ah.Cfg, ah.Logger),
			middleware.HasPermission([]string{"create:account:any"}),
		)(http.HandlerFunc(ah.CreateBotAccount)),
	)

	router.Handle("GET /accounts/me",
		middleware.CreateStack(
			middleware.IsAuthenticated(ah.Cfg, ah.Logger),
			middleware.HasPermission([]string{"read:account:own"}),
		)(http.HandlerFunc(ah.GetPersonalAccount)),
	)

	router.Handle("PATCH /accounts/me",
		middleware.CreateStack(
			middleware.IsAuthenticated(ah.Cfg, ah.Logger),
			middleware.HasPermission([]string{"update:account:own"}),
		)(http.HandlerFunc(ah.UpdatePersonalAccount)),
	)

}

// Creates a bot account and an initial access token
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

	role, err := repo.GetRoleByName(r.Context(), "bot")
	if err != nil {
		ah.Logger.Error("Failed to retrieve bot role",
			slog.Any("error", err),
			slog.Any("account", accData),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We couldn't found a suitable role to assign to your bot",
		})
		return
	}

	if _, err := repo.AssignRole(r.Context(), repository.AssignRoleParams{
		UserID: created.ID, RoleID: role.ID,
	}); err != nil {
		ah.Logger.Error("Failed to assign role",
			slog.Any("error", err),
			slog.Any("account", accData),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We couldn't assign default bot role",
		})
		return
	}

	// Generate an access key for the bot account
	token, err := utils.GenerateServiceToken(created, ah.Cfg)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		ah.Logger.Info("Error while trying to retrieve user perms",
			slog.Any("error", err),
		)
		http.Error(w, "Failed to fetch your authorization details", http.StatusInternalServerError)
		return
	}

	expiry := time.Now().Add(time.Hour * 24 * 30)

	// store the token hash
	cachedToken, err := repo.CreateServiceToken(r.Context(), repository.CreateServiceTokenParams{
		AccountID: created.ID,
		Name:      fmt.Sprintf("Default token for %s", created.Name),
		TokenHash: utils.HashToken(token),
		ExpiresAt: &expiry,
	})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		ah.Logger.Info("Error while trying to store token",
			slog.Any("error", err),
		)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "Failed to cache service token",
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
	json.NewEncoder(w).Encode(map[string]any{
		"x-api-key": token,
		"expiry":    cachedToken.ExpiresAt,
		"account":   created,
	})
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

func (ah *AccountHandler) UpdatePersonalAccount(w http.ResponseWriter, r *http.Request) {
	var accData repository.UpdateAccountDetailsParams
	if err := json.NewDecoder(r.Body).Decode(&accData); err != nil || accData.Name == "" {
		ah.Logger.Error("Failed to parse request body", slog.Any("error", err))
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Please check your request body and try again",
		})
		return
	}
	claims := r.Context().Value(middleware.AuthUserClaims).(*utils.VerisafeClaims)

	// Check if the user is indeed the owner of the account
	if accData.ID.String() != claims.Subject {
		ah.Logger.Error("Attempting to update wrong account")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "You dont have permissions to update this account",
		})
		return
	}
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

	err = repo.UpdateAccountDetails(r.Context(), accData)
	if err != nil {
		ah.Logger.Error("Error while processing request", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into an error while trying to update your account",
		})
		return
	}
	updated, err := repo.GetAccountByID(r.Context(), accData.ID)
	if err != nil {
		ah.Logger.Error("Error while processing request", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into an error while trying to fetch your account",
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

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(updated)
}


// TODO: implement verifying mechanisms
// Use a provider such as AT or One Signal etc
func (ah* AccountHandler)VerifyPhone(w http.ResponseWriter, r* http.Request)  {
	
}
