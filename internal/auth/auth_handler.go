package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"
	"github.com/opencrafts-io/verisafe/internal/eventbus"
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
	router.HandleFunc("POST /auth/token/refresh", a.RefreshTokenHandler)

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
	_, tx, repo, err := a.getDBConnectionAndRepo(r)
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
func (a *Auth) completeOAuthAuth(w http.ResponseWriter, r *http.Request) (goth.User, error) {
	user, err := gothic.CompleteUserAuth(w, r)
	if err != nil {
		return goth.User{}, fmt.Errorf("failed to complete OAuth auth: %w", err)
	}
	return user, nil
}

// getDBConnectionAndRepo establishes database connection and creates repository
func (a *Auth) getDBConnectionAndRepo(r *http.Request) (*pgxpool.Conn, pgx.Tx, *repository.Queries, error) {
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
func (a *Auth) handleAccountManagement(r *http.Request, repo *repository.Queries, user goth.User) (repository.Account, error) {
	account, err := repo.GetAccountByEmail(r.Context(), user.Email)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return repository.Account{}, fmt.Errorf("failed to check user existence: %w", err)
	}

	// Create user if they don't exist
	if errors.Is(err, pgx.ErrNoRows) {
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

		// Publish user created event
		if a.eventBus != nil {
			requestID := eventbus.GenerateRequestID()
			if err := a.eventBus.PublishUserCreated(r.Context(), account, requestID); err != nil {
				a.logger.Error("Failed to publish user created event",
					slog.String("error", err.Error()),
					slog.String("user_id", account.ID.String()),
					slog.String("request_id", requestID),
				)
				// Don't fail the entire operation if event publishing fails
			}
		}
	}

	return account, nil
}

// handleSocialAccountManagement creates or updates the social account connection
func (a *Auth) handleSocialAccountManagement(r *http.Request, repo *repository.Queries, user goth.User, account repository.Account, provider string) error {
	socialAccount, err := repo.GetSocialByExternalUserID(r.Context(), user.UserID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("failed to fetch social connection: %w", err)
	}

	// If the social account does not exist yet create it
	if errors.Is(err, pgx.ErrNoRows) {
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

		// Publish user updated event for existing users
		if a.eventBus != nil {
			requestID := eventbus.GenerateRequestID()
			if err := a.eventBus.PublishUserUpdated(r.Context(), account, requestID); err != nil {
				a.logger.Error("Failed to publish user updated event",
					slog.String("error", err.Error()),
					slog.String("user_id", account.ID.String()),
					slog.String("request_id", requestID),
				)
				// Don't fail the entire operation if event publishing fails
			}
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

// RefreshTokenHandler  refreshes the user token and provides a new set of tokens to be used
func (a *Auth) RefreshTokenHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// The refresh_token request payload
	type RefreshTokenRequestData struct {
		RefreshToken string `json:"refresh_token"`
	}

	var refreshTokenData RefreshTokenRequestData

	if err := json.NewDecoder(r.Body).Decode(&refreshTokenData); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "Please check your request body and try again",
		})
		return
	}

	// Validate the token
	claims, err := utils.ValidateRefreshToken(refreshTokenData.RefreshToken, a.config.JWTConfig.ApiSecret)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		a.logger.Error("Failed to validate refresh token", slog.Any("token", refreshTokenData.RefreshToken))
		json.NewEncoder(w).Encode(map[string]any{"error": "We couldn't validate your refresh token at the moment"})
		return
	}

	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		a.logger.Error("Failed to parse user id from refresh token",
			slog.Any("raw", claims.ID),
		)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We failed to parse user id from access token",
		})
		return

	}

	// Generate jwt and refresh token
	token, err := utils.GenerateJWT(userID, *a.config)
	if err != nil {
		a.logger.Error("Failed to generate user access token",
			slog.Any("raw", userID.String()),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We ran into an issue generating a new acces refresh token pair.",
		})
		return
	}

	refreshToken, err := utils.GenerateJWT(userID, *a.config, utils.UserRefreshToken)
	if err != nil {
		a.logger.Error("Failed to generate user refresh token",
			slog.Any("raw", userID.String()),
		)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "We ran into an issue generating a new acces refresh token pair.",
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]any{
		"access_token":  token,
		"refresh_token": refreshToken,
	})
}
