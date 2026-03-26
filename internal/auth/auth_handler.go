package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/eventbus"
	"github.com/opencrafts-io/verisafe/internal/geo"
	"github.com/opencrafts-io/verisafe/internal/handlers"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/repository"
	"github.com/opencrafts-io/verisafe/internal/service"
	"github.com/opencrafts-io/verisafe/internal/tokens"
)

const (
	authPlatformWebValue    = "auth.platform.value.web"
	authPlatformMobileValue = "auth.platform.value.mobile"
	authCodeTTL             = 60 * time.Second
	authCodePrefix          = "auth_code:"
)

type StateData struct {
	Platform    string
	RedirectURI string
	DeepLink    string
	DeviceName  string
	DeviceToken string
}

type appleUserJSON struct {
	Name struct {
		FirstName string `json:"firstName"`
		LastName  string `json:"lastName"`
	} `json:"name"`
	Email string `json:"email"`
}

type refreshTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type revokeTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type tokenResponse struct {
	AccessToken      string    `json:"access_token"`
	RefreshToken     string    `json:"refresh_token"`
	AccessExpiresAt  time.Time `json:"access_expires_at"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
}

type AuthHandler struct {
	geoLocator *geo.GeoIPLocater
	auth       *Auth
	db         core.IDBProvider
	cacher     core.Cacher
	eventBus   *eventbus.UserEventBus
	logger     *slog.Logger
}

func NewAuthHandler(
	auth *Auth,
	db core.IDBProvider,
	cacher core.Cacher,
	eventBus *eventbus.UserEventBus,
	logger *slog.Logger,
	geoLocator *geo.GeoIPLocater,
) *AuthHandler {
	return &AuthHandler{
		auth:       auth,
		db:         db,
		cacher:     cacher,
		eventBus:   eventBus,
		logger:     logger,
		geoLocator: geoLocator,
	}
}

func (h *AuthHandler) RegisterHandlers(router *http.ServeMux) {
	router.HandleFunc("GET /auth/{provider}", h.LoginHandler)
	router.HandleFunc("/auth/{provider}/callback", h.CallbackHandler)
	router.Handle(
		"POST /auth/token/exchange",
		handlers.AppHandler(h.ExchangeAuthCodeHandler),
	)
	router.Handle(
		"POST /auth/token/refresh",
		handlers.AppHandler(h.RefreshTokenHandler),
	)
	router.Handle(
		"POST /auth/token/revoke",
		middleware.CreateStack(
			middleware.IsAuthenticated(h.auth.config, h.db, h.cacher, h.logger),
		)(handlers.AppHandler(h.RevokeTokenHandler)),
	)
	router.Handle(
		"GET /auth/{provider}/logout",
		middleware.CreateStack(
			middleware.IsAuthenticated(h.auth.config, h.db, h.cacher, h.logger),
		)(http.HandlerFunc(h.LogoutHandler)),
	)
}

// isRedirectAllowed checks the redirectURI against a server-side allowlist.
// Never trust caller-supplied redirect URIs without this check.
func isRedirectAllowed(redirectURI string, allowed []string) bool {
	for _, a := range allowed {
		if strings.EqualFold(a, redirectURI) {
			return true
		}
	}
	return false
}

type authCodeExchangeRequest struct {
	Code string `json:"code"`
}

func (h *AuthHandler) storeAuthCode(
	ctx context.Context,
	code string,
	pair *tokens.TokenPair,
) error {
	val, err := json.Marshal(tokenResponse{
		AccessToken:      pair.AccessToken,
		RefreshToken:     pair.RawRefreshToken,
		AccessExpiresAt:  pair.AccessExpiresAt,
		RefreshExpiresAt: pair.RefreshExpiresAt,
	})
	if err != nil {
		return err
	}
	return h.cacher.Set(ctx, authCodePrefix+code, string(val), authCodeTTL)
}

func (h *AuthHandler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	provider, err := GetProviderName(r)
	if err != nil {
		h.logger.Warn("missing provider in login request", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	platform := authPlatformMobileValue
	redirectURI := ""

	if r.URL.Query().Get("platform") == "web" {
		platform = authPlatformWebValue
		redirectURI = r.URL.Query().Get("redirect_uri")
		if redirectURI == "" {
			http.Error(
				w,
				"missing redirect_uri for web platform",
				http.StatusBadRequest,
			)
			return
		}

		if !isRedirectAllowed(
			redirectURI,
			h.auth.config.JWTConfig.AllowedRedirectURIs,
		) {
			http.Error(w, "redirect_uri not allowed", http.StatusBadRequest)
			return
		}
	}

	state := encodeState(StateData{
		Platform:    platform,
		RedirectURI: redirectURI,
		DeepLink:    r.URL.Query().Get("deep_link"),
		DeviceName:  r.URL.Query().Get("device_name"),
		DeviceToken: r.URL.Query().Get("device_token"),
	})

	h.logger.Info("initiating OAuth login",
		slog.String("provider", provider),
		slog.String("platform", platform),
	)

	q := r.URL.Query()
	q.Set("state", state)
	r.URL.RawQuery = q.Encode()

	url, err := gothic.GetAuthURL(w, r)
	if err != nil {
		h.logger.Error("failed to get auth URL from provider", "error", err)
		http.Error(
			w,
			"failed to initiate login",
			http.StatusInternalServerError,
		)
		return
	}

	http.Redirect(w, r, url, http.StatusFound)
}

func (h *AuthHandler) CallbackHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			h.logger.Error("failed to parse Apple callback form", "error", err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
	}

	provider, err := GetProviderName(r)
	if err != nil {
		h.logger.Warn("missing provider in callback", "error", err)
		http.Error(w, "missing provider", http.StatusBadRequest)
		return
	}

	stateData, err := decodeState(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	gothUser, err := gothic.CompleteUserAuth(w, r)
	if err != nil {
		h.logger.Error("OAuth flow failed", slog.Any("error", err))
		http.Error(w, "authentication failed", http.StatusInternalServerError)
		return
	}

	if provider == "apple" {
		gothUser = patchAppleUserName(r, gothUser)
	}

	conn, err := h.db.Acquire(r.Context())
	if err != nil {
		h.logger.Error(
			"failed to acquire DB connection",
			slog.Any("error", err),
		)
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	var pair *tokens.TokenPair

	err = core.WithTransaction(r.Context(), conn, func(tx pgx.Tx) error {
		repo := repository.New(tx)
		deviceSvc := service.NewDeviceService(repo)
		tokenSvc := tokens.NewTokenService(repo, h.cacher, h.auth.config)

		account, err := h.upsertAccount(r, repo, gothUser)
		if err != nil {
			return err
		}

		if err := h.upsertSocialConnection(r, repo, gothUser, account, provider); err != nil {
			return err
		}

		// Parse IP from request
		ip, err := netip.ParseAddr(strings.Split(r.RemoteAddr, ":")[0])
		if err != nil {
			return fmt.Errorf("parse remote addr: %w", err)
		}

		input := service.DeviceRegistrationInput{
			UserID:      account.ID,
			DeviceName:  stateData.DeviceName,
			Platform:    stateData.Platform,
			DeviceToken: stateData.DeviceToken,
			IpAddress:   &ip,
		}

		if h.geoLocator != nil {
			if info, err := h.geoLocator.Lookup(ip); err != nil {
				h.logger.Warn(
					"geo lookup failed",
					slog.String("ip", ip.String()),
					slog.Any("error", err),
				)
			} else {
				input.Country = &info.Country.ISOCode
			}
		}

		device, err := deviceSvc.RegisterDevice(
			r.Context(),
			input,
		)
		if err != nil {
			return fmt.Errorf("register device: %w", err)
		}

		tokenFamily := uuid.New()

		pair, err = tokenSvc.IssueTokenPair(
			r.Context(),
			account.ID,
			device.ID,
			tokenFamily,
		)
		if err != nil {
			return fmt.Errorf("issue token pair: %w", err)
		}

		return nil
	})
	if err != nil {
		h.logger.Error("callback transaction failed", slog.Any("error", err))
		http.Error(w, "authentication failed", http.StatusInternalServerError)
		return
	}

	if stateData.Platform == authPlatformMobileValue {
		h.handleMobileCallback(w, r, pair, stateData)
	} else {
		h.redirectWithTokens(w, r, pair, stateData)
	}
}

func (h *AuthHandler) RefreshTokenHandler(
	w http.ResponseWriter,
	r *http.Request,
) error {
	var req refreshTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		req.RefreshToken == "" {
		return fmt.Errorf(
			"%w: missing or malformed refresh_token",
			core.ErrInvalidInput,
		)
	}

	conn, err := h.db.Acquire(r.Context())
	if err != nil {
		return fmt.Errorf("%w: failed to acquire connection", core.ErrInternal)
	}

	var pair *tokens.TokenPair

	err = core.WithTransaction(r.Context(), conn, func(tx pgx.Tx) error {
		tokenSvc := tokens.NewTokenService(
			repository.New(tx),
			h.cacher,
			h.auth.config,
		)
		var err error
		pair, err = tokenSvc.RotateRefreshToken(r.Context(), req.RefreshToken)
		return err
	})
	if err != nil {
		h.logger.Warn("refresh token rotation failed", slog.Any("error", err))
		return fmt.Errorf("%w: %s", core.ErrUnauthorized, err.Error())
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(tokenResponse{
		AccessToken:      pair.AccessToken,
		RefreshToken:     pair.RawRefreshToken,
		AccessExpiresAt:  pair.AccessExpiresAt,
		RefreshExpiresAt: pair.RefreshExpiresAt,
	})
	return nil
}

func (h *AuthHandler) RevokeTokenHandler(
	w http.ResponseWriter,
	r *http.Request,
) error {
	claims, ok := r.Context().Value(middleware.AuthUserClaims).(*tokens.VerisafeClaims)
	if !ok || claims == nil {
		return fmt.Errorf("%w: missing claims", core.ErrUnauthorized)
	}

	jti, err := claims.JTI()
	if err != nil {
		return fmt.Errorf("%w: invalid jti in token", core.ErrUnauthorized)
	}

	var req revokeTokenRequest
	json.NewDecoder(r.Body).Decode(&req)

	conn, err := h.db.Acquire(r.Context())
	if err != nil {
		return fmt.Errorf("%w: failed to acquire connection", core.ErrInternal)
	}

	remaining := time.Until(claims.RegisteredClaims.ExpiresAt.Time)

	err = core.WithTransaction(r.Context(), conn, func(tx pgx.Tx) error {
		tokenSvc := tokens.NewTokenService(
			repository.New(tx),
			h.cacher,
			h.auth.config,
		)

		if remaining > 0 {
			if err := tokenSvc.RevokeAccessToken(r.Context(), jti, remaining); err != nil {
				return fmt.Errorf("blocklist access token: %w", err)
			}
		}

		if req.RefreshToken != "" {
			if err := tokenSvc.RevokeByRawToken(r.Context(), req.RefreshToken); err != nil {
				// Non-fatal — access token is already blocklisted.
				h.logger.Warn(
					"failed to revoke refresh token family",
					slog.Any("error", err),
				)
			}
		}

		return nil
	})
	if err != nil {
		h.logger.Error("failed to revoke token", slog.Any("error", err))
		return fmt.Errorf("%w: could not revoke access token", core.ErrInternal)
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *AuthHandler) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	provider, err := GetProviderName(r)
	if err != nil {
		h.logger.Warn("missing provider in logout request", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := gothic.Logout(w, r); err != nil {
		h.logger.Error("failed to logout from provider",
			slog.String("provider", provider),
			slog.Any("error", err),
		)
		http.Error(w, "logout failed", http.StatusInternalServerError)
		return
	}

	h.logger.Info("user logged out", slog.String("provider", provider))
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

func (h *AuthHandler) ExchangeAuthCodeHandler(
	w http.ResponseWriter,
	r *http.Request,
) error {
	var req authCodeExchangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		req.Code == "" {
		return fmt.Errorf("%w: missing or malformed code", core.ErrInvalidInput)
	}

	key := authCodePrefix + req.Code
	var resp tokenResponse

	if err := h.cacher.Get(r.Context(), key, &resp); err != nil {
		if errors.Is(err, core.ErrCacheMiss) {
			h.logger.Error("Invalid or expired code", slog.Any("error", err))
			return fmt.Errorf(
				"%w: invalid or expired code",
				core.ErrUnauthorized,
			)
		}

		h.logger.Error("Failed to retrieve auth code", slog.Any("error", err))
		return fmt.Errorf("%w: failed to retrieve auth code", core.ErrInternal)
	}

	// One-time use — delete immediately after retrieval
	if err := h.cacher.Delete(r.Context(), key); err != nil {
		h.logger.Warn(
			"failed to delete auth code after exchange",
			slog.Any("error", err),
		)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
	return nil
}

// --- helpers ---

func (h *AuthHandler) upsertAccount(
	r *http.Request,
	repo *repository.Queries,
	user goth.User,
) (repository.Account, error) {
	account, err := repo.GetAccountByEmail(r.Context(), user.Email)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return repository.Account{}, fmt.Errorf("lookup account: %w", err)
	}

	if errors.Is(err, pgx.ErrNoRows) {
		account, err = repo.CreateAccount(
			r.Context(),
			repository.CreateAccountParams{
				Email: user.Email,
				Name: strings.TrimSpace(
					user.FirstName + " " + user.LastName,
				),
				Type:      repository.AccountTypeHuman,
				AvatarUrl: &user.AvatarURL,
			},
		)
		if err != nil {
			return repository.Account{}, fmt.Errorf("create account: %w", err)
		}

		h.publishEvent(r, func() error {
			return h.eventBus.PublishUserCreated(
				r.Context(),
				account,
				eventbus.GenerateRequestID(),
			)
		}, "publish user created event")
	}

	return account, nil
}

func (h *AuthHandler) upsertSocialConnection(
	r *http.Request,
	repo *repository.Queries,
	user goth.User,
	account repository.Account,
	provider string,
) error {
	_, err := repo.GetSocialByExternalUserID(r.Context(), user.UserID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("lookup social connection: %w", err)
	}

	if errors.Is(err, pgx.ErrNoRows) {
		_, err = repo.CreateSocial(r.Context(), repository.CreateSocialParams{
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
			return fmt.Errorf("create social connection: %w", err)
		}
		return nil
	}

	_, err = repo.UpdateSocial(r.Context(), repository.UpdateSocialParams{
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
	})
	if err != nil {
		return fmt.Errorf("update social connection: %w", err)
	}

	h.publishEvent(r, func() error {
		return h.eventBus.PublishUserUpdated(
			r.Context(),
			account,
			eventbus.GenerateRequestID(),
		)
	}, "publish user updated event")

	return nil
}

func (h *AuthHandler) handleMobileCallback(
	w http.ResponseWriter,
	r *http.Request,
	pair *tokens.TokenPair,
	stateData *StateData,
) {
	code, err := generateOpaqueCode()
	if err != nil {
		h.logger.Error("failed to generate auth code", slog.Any("error", err))
		http.Error(w, "authentication failed", http.StatusInternalServerError)
		return
	}

	if err := h.storeAuthCode(r.Context(), code, pair); err != nil {
		h.logger.Error("failed to store auth code", slog.Any("error", err))
		http.Error(w, "authentication failed", http.StatusInternalServerError)
		return
	}

	// Redirect to deep link — tokens never touch the URL
	// e.g. myapp://auth/callback?code=abc123
	deepLink := fmt.Sprintf("%s?code=%s", stateData.DeepLink, code)
	http.Redirect(w, r, deepLink, http.StatusFound)
}

func generateOpaqueCode() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (h *AuthHandler) redirectWithTokens(
	w http.ResponseWriter,
	r *http.Request,
	pair *tokens.TokenPair,
	stateData *StateData,
) {
	http.SetCookie(w, &http.Cookie{
		Name:     "access_token",
		Value:    pair.AccessToken,
		Path:     "/",
		Expires:  pair.AccessExpiresAt,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    pair.RawRefreshToken,
		Path:     "/",
		Expires:  pair.RefreshExpiresAt,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})

	target := stateData.RedirectURI
	if target == "" {
		target = "/"
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func (h *AuthHandler) publishEvent(
	r *http.Request,
	fn func() error,
	label string,
) {
	if h.eventBus == nil {
		return
	}
	if err := fn(); err != nil {
		h.logger.Error(label, slog.Any("error", err))
	}
}

func patchAppleUserName(r *http.Request, user goth.User) goth.User {
	if r.FormValue("user") == "" || user.FirstName != "" {
		return user
	}

	var appleData appleUserJSON
	if err := json.Unmarshal([]byte(r.FormValue("user")), &appleData); err != nil {
		return user
	}

	if appleData.Name.FirstName != "" || appleData.Name.LastName != "" {
		user.FirstName = appleData.Name.FirstName
		user.LastName = appleData.Name.LastName
		user.Name = strings.TrimSpace(user.FirstName + " " + user.LastName)
	}

	return user
}

func encodeState(s StateData) string {
	raw := fmt.Sprintf(
		"%s|%s|%s|%s|%s",
		s.Platform,
		s.RedirectURI,
		s.DeepLink,
		s.DeviceName,
		s.DeviceToken,
	)
	return base64.URLEncoding.EncodeToString([]byte(raw))
}

func decodeState(r *http.Request) (*StateData, error) {
	state := r.FormValue("state")
	if state == "" {
		return nil, errors.New("missing state parameter")
	}

	b, err := base64.URLEncoding.DecodeString(state)
	if err != nil {
		return nil, errors.New("invalid state encoding")
	}

	parts := strings.SplitN(string(b), "|", 5)
	if len(parts) != 5 {
		return nil, errors.New("malformed state parameter")
	}

	return &StateData{
		Platform:    parts[0],
		RedirectURI: parts[1],
		DeepLink:    parts[2],
		DeviceName:  parts[3],
		DeviceToken: parts[4],
	}, nil
}
