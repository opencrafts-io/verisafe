package auth

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/markbates/goth/gothic"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/repository"
	"github.com/opencrafts-io/verisafe/internal/utils"
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
		http.Error(w, "Failed to get provider name for callback", http.StatusBadRequest)
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
		http.Error(w, "Failed to establish database connection", http.StatusInternalServerError)
		return
	}

	tx, _ := conn.Begin(r.Context())
	defer tx.Rollback(r.Context())
	repo := repository.New(tx)

	var account repository.Account
	var socialAccount repository.Social

	account, err = repo.GetAccountByEmail(r.Context(), user.Email)
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
		account, err = repo.CreateAccount(r.Context(), userParams)
		if err != nil {
			a.logger.Error("Error creating user", slog.Any("error", err))
			http.Error(w, "Failed to create account", http.StatusInternalServerError)
			return
		}

	}

	// Check whether the the social account exists for the user
	socialAccount, err = repo.GetSocialByExternalUserID(r.Context(), user.UserID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		a.logger.Error("Error while processing request", slog.Any("error", err))
		http.Error(w, "Failed to fetch social connection", http.StatusInternalServerError)
		return
	}

	// If the social account does not exist yet create it
	if errors.Is(err, sql.ErrNoRows) {
		socialAccount, err = repo.CreateSocial(r.Context(), repository.CreateSocialParams{

			UserID:            user.UserID,
			AccountID:         account.ID,
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
		})

		if err != nil {
			a.logger.Error("Error creating social connection", slog.Any("error", err))
			http.Error(w, "Error creating social connection", http.StatusInternalServerError)
			return
		}
		a.logger.Info("New social connection created for user",
			slog.Any("created_user", account), slog.Any("social_account", socialAccount),
		)
	} else {

		a.logger.Debug("Use", slog.Any("user", user))
		//
		// // Update the social account
		_, err := repo.UpdateSocial(r.Context(),
			repository.UpdateSocialParams{
				UserID:            user.UserID,
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
			},
		)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			a.logger.Info("Error while trying to update social auth", slog.Any("error", err))
			http.Error(w, "Error updating social connection", http.StatusInternalServerError)
			return
		}

	}
	userRoles, err := repo.GetAllUserRoles(r.Context(), account.ID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		a.logger.Info("Error while trying to retrieve user role",
			slog.Any("error", err),
			slog.Any("roles", userRoles),
		)

		http.Error(w, "Failed to fetch your authorization details", http.StatusInternalServerError)
		return
	}

	userPerms, err := repo.GetUserPermissions(r.Context(), account.ID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		a.logger.Info("Error while trying to retrieve user role",
			slog.Any("error", err),
			slog.Any("perms", userPerms),
		)

		http.Error(w, "Failed to fetch your authorization details", http.StatusInternalServerError)
		return
	}

	if err = tx.Commit(r.Context()); err != nil {
		a.logger.Error("Error while processing request", slog.Any("error", err))
		http.Error(w, "Error while committing transaction", http.StatusInternalServerError)
		return
	}

	token, err := utils.GenerateJWT(account, userRoles, userPerms, *a.config)
	if err != nil {
		http.Error(w, "Error while generating jwt token", http.StatusInternalServerError)
		return
	}
	w.Header().Add("Authorization", strings.Join([]string{"bearer", token}, " "))
	redirectURI := fmt.Sprintf("academia://callback?token=%s", token)
	http.Redirect(w, r, redirectURI, http.StatusFound)
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
