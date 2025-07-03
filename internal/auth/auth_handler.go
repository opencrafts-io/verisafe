package auth

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/markbates/goth/gothic"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/repository"
)

// LoginHandler initiates the OAuth2 authentication flow.
// It redirects the user to the selected OAuth provider's login page.
// This handler should be mapped to a route like `/auth/{provider}` or `/login`
// where the provider is passed as a query parameter (e.g., `/login?provider=google`).
func (a *Auth) LoginHandler(w http.ResponseWriter, r *http.Request) {
	provider, err := GetProviderName(r)
	if err != nil {
		a.logger.Warn("Failed to get provider name for login", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	a.logger.Info("Initiating OAuth login", "provider", provider)
	gothic.BeginAuthHandler(w, r)
}

// CallbackHandler processes the OAuth2 callback from the provider.
// After the user authenticates with the provider, they are redirected back to this handler.
// This handler should be mapped to the `callback` URL configured with the OAuth provider,
// e.g., `/auth/{provider}/callback`.
func (a *Auth) CallbackHandler(w http.ResponseWriter, r *http.Request) {
	provider, err := GetProviderName(r)
	if err != nil {
		a.logger.Warn("Failed to get provider name for callback", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	user, err := gothic.CompleteUserAuth(w, r)
	if err != nil {
		a.logger.Error("Ran into an error when perfoming auth flow", slog.Any("error", err))
		http.Error(w, "Authentication flow failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		a.logger.Error("Error while processing request", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}
	tx, _ := conn.Begin(r.Context())
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	// Check whether the the social account exists for the user
	socialAccount, err := repo.GetSocialByExternalUserID(r.Context(), user.UserID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		a.logger.Error("Error while processing request", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "We ran into a problem while servicing your request please try again later",
		})
		return
	}

	// If the social account does not exist yet check if the user already exists
	if errors.Is(err, sql.ErrNoRows) {
		_, err := repo.GetAccountByEmail(r.Context(), user.Email)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			a.logger.Error("Error checking user existence", slog.Any("error", err))
			http.Error(w, "Error checking user existence", http.StatusInternalServerError)
			return
		}

		// Create user if they don't exist
		if errors.Is(err, sql.ErrNoRows) {
			userParams := repository.CreateAccountParams{
				Email: user.Email,
				Name:  strings.Join([]string{user.FirstName, user.LastName}, " "),
			}

			// Create the user in the repository
			createdUser, err := repo.CreateAccount(r.Context(), userParams)
			if err != nil {
				a.logger.Error("Error creating user", slog.Any("error", err))
				http.Error(w, "Error creating user", http.StatusInternalServerError)
				return
			}

			// Create the social connection
			socialParams := repository.CreateSocialParams{
				UserID:            user.UserID,
				AccountID:         createdUser.ID,
				Provider:          provider,
				Email:             &user.Email,
				Name:              &user.Name,
				FirstName:         &user.FirstName,
				LastName:          &user.LastName,
				NickName:          &user.NickName,
				Description:       &user.Description,
				AvatarUrl:         &user.AvatarURL,
				Location:          &user.Location,
				AccessToken:       &user.AccessToken,
				AccessTokenSecret: &user.AccessTokenSecret,
				RefreshToken:      &user.RefreshToken,
				ExpiresAt:         pgtype.Timestamp{Time: user.ExpiresAt},
			}

			// Create the new social account for the user
			socialAccount, err = repo.CreateSocial(r.Context(), socialParams)
			if err != nil {
				a.logger.Error("Error creating social connection", slog.Any("error", err))
				http.Error(w, "Error creating social connection", http.StatusInternalServerError)
				return
			}
			a.logger.Info("New social connection created for user",
				slog.Any("created_user", createdUser), slog.Any("social_account", socialAccount),
			)
		}
	} else {
		// If the social account already exists, check if it's from the same provider
		if socialAccount.Provider != provider {
			// User is trying to link a new provider to their existing account.
			// Depending on your business logic, you could either:
			// 1. Reject the request, or
			// 2. Allow it and link the new provider.
			// Option 1: Reject the request if the provider doesn't match
			a.logger.Warn("User is trying to link a different provider to the same account", slog.Any("error", err))
			http.Error(w, "This account is already linked to another provider", http.StatusBadRequest)
			return
		}

		// Option 2: If same provider, you could update the social connection here if needed.
		// For example, you could update tokens, or refresh token if expired.
		// TODO (erick) (Handle updating of social connection)
		a.logger.Info("User already connected with this provider")
	}

	if err = tx.Commit(r.Context()); err != nil {
		a.logger.Error("Error while processing request", slog.Any("error", err))
		http.Error(w, "Error while committing transaction", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(socialAccount)
}

// LogoutHandler logs the user out from the OAuth provider and clears Goth's session data.
// It assumes the provider name is passed as a query parameter (e.g., `/logout?provider=google`).
// You would also typically clear your application's session/JWT here.
func (a *Auth) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	provider, err := GetProviderName(r)
	if err != nil {
		a.logger.Warn("Failed to get provider name for logout", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := gothic.Logout(w, r); err != nil {
		a.logger.Error("Error logging out from OAuth provider", "provider", provider, "error", err)
		http.Error(w, fmt.Sprintf("Error logging out from %s: %v", provider, err), http.StatusInternalServerError)
		return
	}

	// Optionally, clear your application's session/JWT here as well
	// e.g., if using gorilla/sessions: sessions.Default(r).Clear() or sessions.Default(r).Options.MaxAge = -1

	a.logger.Info("Successfully logged out", "provider", provider)
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect) // Redirect to homepage
}
