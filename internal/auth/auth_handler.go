package auth

import (
	"database/sql"
	"encoding/base64"
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

const authPlatformKey = "auth.platform.key"
const authPlatformWebValue = "auth.platform.value.web"
const authPlatformMobileValue = "auth.platform.value.mobile"
const authRedirectKey = "auth.redirect.key"

func (a *Auth) RegisterRoutes(router *http.ServeMux) {
	router.HandleFunc("GET /auth/{provider}", a.LoginHandler)
	router.HandleFunc("GET /auth/{provider}/callback", a.CallbackHandler)
	router.HandleFunc("GET /auth/{provider}/logout", a.LogoutHandler)

	// Secret management
	// router.Handle("GET /auth/generate/token",
	// 	middleware.CreateStack(
	// 		middleware.IsAuthenticated(a.config, a.logger),
	// 		middleware.HasPermission([]string{"create:service_token:own"}),
	// 	)(http.HandlerFunc(a.CreateServiceToken)),
	// )

}

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

	platform := authPlatformMobileValue
	redirectURI := ""

	if r.URL.Query().Get("platform") == "web" {
		platform = authPlatformWebValue

		redirectURI = r.URL.Query().Get("redirect_uri")
		if redirectURI == "" {
			http.Error(w, "Programming error: missing redirect_uri", http.StatusBadRequest)
			return
		}
	}

	// encode platform + redirect_uri into state
	stateData := fmt.Sprintf("%s|%s", platform, redirectURI)
	state := base64.URLEncoding.EncodeToString([]byte(stateData))

	a.logger.Info("Initiating OAuth login",
		"provider", provider,
		"platform", platform,
		"redirect_uri", redirectURI,
	)

	// Clone request and inject state query param (gothic looks for it here)
	q := r.URL.Query()
	q.Set("state", state)
	r.URL.RawQuery = q.Encode()

	// Get provider auth URL
	url, err := gothic.GetAuthURL(w, r)
	if err != nil {
		a.logger.Error("Failed to get auth URL", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Redirect user to provider login page
	http.Redirect(w, r, url, http.StatusFound)
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

	state := r.URL.Query().Get("state")
	if state == "" {
		http.Error(w, "Missing state", http.StatusBadRequest)
		return
	}

	stateBytes, err := base64.URLEncoding.DecodeString(state)
	if err != nil {
		http.Error(w, "Invalid state", http.StatusBadRequest)
		return
	}

	parts := strings.SplitN(string(stateBytes), "|", 2)
	if len(parts) != 2 {
		http.Error(w, "Malformed state", http.StatusBadRequest)
		return
	}

	platform := parts[0]
	redirectURI := parts[1]

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
			Email:     user.Email,
			Name:      strings.Join([]string{user.FirstName, user.LastName}, " "),
			Type:      repository.AccountTypeHuman,
			AvatarUrl: &user.AvatarURL,
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
				Email:             user.Email,
				Name:              user.Name,
				FirstName:         user.FirstName,
				LastName:          user.LastName,
				NickName:          user.NickName,
				Description:       user.Description,
				AvatarUrl:         user.AvatarURL,
				Location:          user.Location,
				AccessToken:       user.AccessToken,
				AccessTokenSecret: user.AccessTokenSecret,
				RefreshToken:      user.RefreshToken,
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

	if err = tx.Commit(r.Context()); err != nil {
		a.logger.Error("Error while processing request", slog.Any("error", err))
		http.Error(w, "Error while committing transaction", http.StatusInternalServerError)
		return
	}

	token, err := utils.GenerateJWT(account.ID, *a.config)
	if err != nil {
		http.Error(w, "Error while generating jwt token", http.StatusInternalServerError)
		return
	}

	refreshToken, err := utils.GenerateJWT(account.ID, *a.config, utils.UserRefreshToken)
	if err != nil {
		http.Error(w, "Error while generating jwt token", http.StatusInternalServerError)
		return
	}

	// redirectURI := fmt.Sprintf("academia://callback?token=%s&refresh_token=%s", token, refreshToken)
	// http.Redirect(w, r, redirectURI, http.StatusFound)
	if platform == authPlatformWebValue {
		// Web: redirect back to client
		finalURL := fmt.Sprintf("%s?token=%s&refresh_token=%s", redirectURI, token, refreshToken)
		http.Redirect(w, r, finalURL, http.StatusFound)
		return
	}

	if platform == authPlatformMobileValue {
		// Mobile: use deep link
		finalURL := fmt.Sprintf("academia://callback?token=%s&refresh_token=%s", token, refreshToken)
		http.Redirect(w, r, finalURL, http.StatusFound)
		return
	}

	http.Error(w, "Unknown platform", http.StatusBadRequest)
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
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect) // Redirectto to homepage
}

// Creates a service token
// func (a *Auth) CreateServiceToken(w http.ResponseWriter, r *http.Request) {
// 	// claims := r.Context().Value(middleware.AuthUserClaims).(*utils.VerisafeClaims)
// 	//
// 	// if claims.Account.Type != repository.AccountTypeBot {
// 	// 	a.logger.Error(
// 	// 		"Attempting to generate service token on a non bot account",
// 	// 		slog.Any("account", claims.Account),
// 	// 	)
// 	// 	w.WriteHeader(http.StatusUnauthorized)
// 	// 	json.NewEncoder(w).Encode(map[string]any{
// 	// 		"error": "Only bot accounts can generate service tokens",
// 	// 	})
// 	// 	return
// 	// }
//
// 	conn, err := middleware.GetDBConnFromContext(r.Context())
// 	if err != nil {
// 		a.logger.Error("failed to get db conn", slog.String("err", err.Error()))
// 		w.WriteHeader(http.StatusInternalServerError)
// 		json.NewEncoder(w).Encode(map[string]any{"error": "Internal server error"})
// 		return
// 	}
//
// 	tx, err := conn.Begin(r.Context())
// 	if err != nil {
// 		a.logger.Error("failed to begin tx", slog.String("err", err.Error()))
// 		w.WriteHeader(http.StatusInternalServerError)
// 		json.NewEncoder(w).Encode(map[string]any{"error": "Internal server error"})
// 		return
// 	}
// 	defer tx.Rollback(r.Context())
//
// 	repo := repository.New(tx)
//
// 	// Get if the service is a bot account
// 	claims := r.Context().Value(middleware.AuthUserClaims).(*utils.VerisafeClaims)
// 	accountID, err := uuid.Parse(claims.ID)
// 	if err != nil {
// 		w.WriteHeader(http.StatusBadRequest)
// 		json.NewEncoder(w).Encode(map[string]any{
// 			"error": "Failed to decode your token.",
// 		})
// 		return
// 	}
//
// 	botAccount, err := repo.GetAccountByID(r.Context(), accountID)
//
// 	if botAccount.Type != "bot" {
// 		w.WriteHeader(http.StatusForbidden)
// 		json.NewEncoder(w).Encode(map[string]any{
// 			"error": "Only bot accounts can perform this action",
// 		})
// 		return
// 	}
//
// 	var serviceTokenParams repository.CreateServiceTokenParams
//
// 	if err := json.NewDecoder(r.Body).Decode(&serviceTokenParams); err != nil || strings.TrimSpace(serviceTokenParams.Name) == "" {
// 		w.WriteHeader(http.StatusBadRequest)
// 		json.NewEncoder(w).Encode(map[string]any{
// 			"error": "Please check your form details and try that again",
// 		})
// 		return
// 	}
// 	//
// 	jwtToken, err := utils.GenerateJWT(
// 		accountID,
// 		*a.config, utils.ServiceToken)
// 	if err != nil {
// 		a.logger.Error("failed to generate service token token", slog.String("err", err.Error()))
// 		w.WriteHeader(http.StatusInternalServerError)
// 		json.NewEncoder(w).Encode(map[string]any{"error": "Could not generate service token"})
// 		return
// 	}
//
// 	hash := sha256.Sum256([]byte(jwtToken))
// 	hashed := base64.StdEncoding.EncodeToString(hash[:])
//
// 	expiry := time.Now().Add(time.Hour * 24 * 30)
//
// 	_, err = repo.CreateServiceToken(r.Context(), repository.CreateServiceTokenParams{
// 		Name:      serviceTokenParams.Name,
// 		TokenHash: hashed,
// 		ExpiresAt: &expiry,
// 	})
// 	if err != nil {
// 		a.logger.Error("failed to store service token", slog.String("err", err.Error()))
// 		w.WriteHeader(http.StatusInternalServerError)
// 		json.NewEncoder(w).Encode(map[string]any{"error": "Could not save service token"})
// 		return
// 	}
//
// 	if err := tx.Commit(r.Context()); err != nil {
// 		a.logger.Error("failed to commit tx", slog.String("err", err.Error()))
// 		w.WriteHeader(http.StatusInternalServerError)
// 		json.NewEncoder(w).Encode(map[string]any{"error": "Could not save token, try again"})
// 		return
// 	}
// 	//
// 	w.Header().Set("Content-Type", "application/json")
// 	// json.NewEncoder(w).Encode(map[string]any{
// 	// 	"token":   jwtToken,
// 	// 	"message": "Token generated successfully do not loose it",
// 	// })
// }
