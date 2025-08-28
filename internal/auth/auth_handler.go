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

// StateData represents the encoded state information passed during OAuth flow
type StateData struct {
	Platform    string
	RedirectURI string
}

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

	// Parse state data
	stateData, err := a.parseStateData(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Complete OAuth authentication
	user, err := a.completeOAuthAuth(w, r)
	if err != nil {
		a.logger.Error("OAuth authentication failed", slog.Any("error", err))
		http.Error(w, "Authentication flow failed", http.StatusInternalServerError)
		return
	}

	// Get database connection and start transaction
	conn, tx, repo, err := a.getDBConnectionAndRepo(r)
	if err != nil {
		a.logger.Error("Database connection failed", slog.Any("error", err))
		http.Error(w, "Failed to establish database connection", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())

	// Handle account creation or retrieval
	account, err := a.handleAccountManagement(r, repo, user)
	if err != nil {
		a.logger.Error("Account management failed", slog.Any("error", err))
		http.Error(w, "Failed to manage account", http.StatusInternalServerError)
		return
	}

	// Handle social account management
	err = a.handleSocialAccountManagement(r, repo, user, account, provider)
	if err != nil {
		a.logger.Error("Social account management failed", slog.Any("error", err))
		http.Error(w, "Failed to manage social account", http.StatusInternalServerError)
		return
	}

	// Commit transaction
	if err = tx.Commit(r.Context()); err != nil {
		a.logger.Error("Transaction commit failed", slog.Any("error", err))
		http.Error(w, "Error while committing transaction", http.StatusInternalServerError)
		return
	}

	// Generate tokens and redirect
	err = a.generateTokensAndRedirect(w, r, account, stateData)
	if err != nil {
		a.logger.Error("Token generation and redirect failed", slog.Any("error", err))
		http.Error(w, "Failed to generate tokens", http.StatusInternalServerError)
		return
	}
}

// parseStateData extracts and validates the state parameter from the request
func (a *Auth) parseStateData(r *http.Request) (*StateData, error) {
	state := r.URL.Query().Get("state")
	if state == "" {
		return nil, errors.New("missing state")
	}

	stateBytes, err := base64.URLEncoding.DecodeString(state)
	if err != nil {
		return nil, errors.New("invalid state")
	}

	parts := strings.SplitN(string(stateBytes), "|", 2)
	if len(parts) != 2 {
		return nil, errors.New("malformed state")
	}

	return &StateData{
		Platform:    parts[0],
		RedirectURI: parts[1],
	}, nil
}

// completeOAuthAuth completes the OAuth authentication flow using Goth
func (a *Auth) completeOAuthAuth(w http.ResponseWriter, r *http.Request) (*gothic.User, error) {
	user, err := gothic.CompleteUserAuth(w, r)
	if err != nil {
		return nil, fmt.Errorf("failed to complete OAuth auth: %w", err)
	}
	return user, nil
}

// getDBConnectionAndRepo establishes database connection and creates repository
func (a *Auth) getDBConnectionAndRepo(r *http.Request) (*sql.DB, *sql.Tx, *repository.Repository, error) {
	conn, err := middleware.GetDBConnFromContext(r.Context())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get DB connection: %w", err)
	}

	tx, err := conn.Begin(r.Context())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	repo := repository.New(tx)
	return conn, tx, repo, nil
}

// handleAccountManagement creates or retrieves the user account
func (a *Auth) handleAccountManagement(r *http.Request, repo *repository.Repository, user *gothic.User) (repository.Account, error) {
	account, err := repo.GetAccountByEmail(r.Context(), user.Email)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return repository.Account{}, fmt.Errorf("failed to check user existence: %w", err)
	}

	// Create user if they don't exist
	if errors.Is(err, sql.ErrNoRows) {
		userParams := repository.CreateAccountParams{
			Email:     user.Email,
			Name:      strings.Join([]string{user.FirstName, user.LastName}, " "),
			Type:      repository.AccountTypeHuman,
			AvatarUrl: &user.AvatarURL,
		}

		account, err = repo.CreateAccount(r.Context(), userParams)
		if err != nil {
			return repository.Account{}, fmt.Errorf("failed to create account: %w", err)
		}
	}

	return account, nil
}

// handleSocialAccountManagement creates or updates the social account connection
func (a *Auth) handleSocialAccountManagement(r *http.Request, repo *repository.Repository, user *gothic.User, account repository.Account, provider string) error {
	socialAccount, err := repo.GetSocialByExternalUserID(r.Context(), user.UserID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("failed to fetch social connection: %w", err)
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
			return fmt.Errorf("failed to create social connection: %w", err)
		}
		a.logger.Info("New social connection created for user",
			slog.Any("created_user", account), slog.Any("social_account", socialAccount),
		)
	} else {
		// Update the social account
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
			return fmt.Errorf("failed to update social connection: %w", err)
		}
	}

	return nil
}

// generateTokensAndRedirect generates JWT tokens and redirects based on platform
func (a *Auth) generateTokensAndRedirect(w http.ResponseWriter, r *http.Request, account repository.Account, stateData *StateData) error {
	token, err := utils.GenerateJWT(account.ID, *a.config)
	if err != nil {
		return fmt.Errorf("failed to generate JWT token: %w", err)
	}

	refreshToken, err := utils.GenerateJWT(account.ID, *a.config, utils.UserRefreshToken)
	if err != nil {
		return fmt.Errorf("failed to generate refresh token: %w", err)
	}

	// Redirect based on platform
	if stateData.Platform == authPlatformWebValue {
		// Web: redirect back to client
		finalURL := fmt.Sprintf("%s?token=%s&refresh_token=%s", stateData.RedirectURI, token, refreshToken)
		http.Redirect(w, r, finalURL, http.StatusFound)
		return nil
	}

	if stateData.Platform == authPlatformMobileValue {
		// Mobile: use deep link
		finalURL := fmt.Sprintf("academia://callback?token=%s&refresh_token=%s", token, refreshToken)
		http.Redirect(w, r, finalURL, http.StatusFound)
		return nil
	}

	return errors.New("unknown platform")
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
