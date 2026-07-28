package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
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
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/providers"
	"github.com/opencrafts-io/verisafe/internal/repository"
	"github.com/opencrafts-io/verisafe/internal/secrets"
	devicesvc "github.com/opencrafts-io/verisafe/internal/service/device"
	grantsvc "github.com/opencrafts-io/verisafe/internal/service/grants"
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

	// grants builds a GrantService bound to the caller's transaction, so a
	// login can mirror provider credentials into oauth_grants. Nil disables
	// the mirroring, which keeps the handler constructible in tests that do
	// not care about it.
	grants func(repository.Querier) grantsvc.GrantService
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

// WithGrantRecording enables mirroring provider credentials into oauth_grants
// on every login.
func (h *AuthHandler) WithGrantRecording(
	registry *providers.Registry,
	sealer *secrets.Sealer,
	exchanger providers.TokenExchanger,
) *AuthHandler {
	h.grants = func(repo repository.Querier) grantsvc.GrantService {
		return grantsvc.NewGrantService(
			repo,
			h.cacher,
			registry,
			sealer,
			exchanger,
			h.auth.config,
			h.logger,
		)
	}
	return h
}

func (h *AuthHandler) RegisterHandlers(router core.Router) {
	router.HandleFunc("GET /auth/{provider}", h.LoginHandler)
	router.HandleFunc("/auth/{provider}/callback", h.CallbackHandler)
	router.Handle(
		"POST /auth/token/exchange",
		core.AppHandler(h.ExchangeAuthCodeHandler),
	)
	router.Handle(
		"POST /auth/token/refresh",
		core.AppHandler(h.RefreshTokenHandler),
	)
	router.Handle(
		"POST /auth/token/revoke",
		middleware.CreateStack(
			middleware.IsAuthenticated(h.auth.config, h.db, h.cacher, h.logger),
		)(core.AppHandler(h.RevokeTokenHandler)),
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
	return h.cacher.Set(
		ctx, authCodePrefix+code,
		tokenResponse{
			AccessToken:      pair.AccessToken,
			RefreshToken:     pair.RawRefreshToken,
			AccessExpiresAt:  pair.AccessExpiresAt,
			RefreshExpiresAt: pair.RefreshExpiresAt,
		},
		authCodeTTL,
	)
}

// LoginHandler godoc
//
// @Summary      Start OAuth2 login
// @Description  Redirects the client to the given OAuth2 provider to begin a login. Omit "platform" (or pass anything other than "web") for the mobile flow. Pass platform=web with a redirect_uri that's in the server-side allowlist for the web flow.
// @Tags         auth
// @Produce      json
// @Param        provider      path   string  true   "OAuth2 provider"  Enums(google, spotify, apple)
// @Param        platform      query  string  false  "Set to 'web' for the web flow; omitted defaults to mobile"
// @Param        redirect_uri  query  string  false  "Required when platform=web; must be in the configured allowlist"
// @Param        deep_link     query  string  false  "Mobile deep link to redirect to after login, e.g. myapp://auth/callback"
// @Param        device_name   query  string  false  "Device name to register on successful login"
// @Param        device_token  query  string  false  "Push notification token to register on successful login"
// @Success      302  "Redirects to the OAuth2 provider"
// @Failure      400  {object}  core.APIError  "Missing provider, or missing/disallowed redirect_uri"
// @Failure      500  {object}  core.APIError  "Failed to initiate login with the provider"
// @Router       /auth/{provider} [get]
func (h *AuthHandler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	provider, err := GetProviderName(r)
	if err != nil {
		h.logger.Warn("missing provider in login request", "error", err)
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	platform := authPlatformMobileValue
	redirectURI := ""

	if r.URL.Query().Get("platform") == "web" {
		platform = authPlatformWebValue
		redirectURI = r.URL.Query().Get("redirect_uri")
		if redirectURI == "" {
			core.WriteError(
				w,
				http.StatusBadRequest,
				"missing redirect_uri for web platform",
			)
			return
		}

		if !isRedirectAllowed(
			redirectURI,
			h.auth.config.JWTConfig.AllowedRedirectURIs,
		) {
			core.WriteError(w, http.StatusBadRequest, "redirect_uri not allowed")
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

	h.logger.Info(
		"initiating OAuth login",
		slog.String("provider", provider),
		slog.String("platform", platform),
	)

	q := r.URL.Query()
	q.Set("state", state)
	r.URL.RawQuery = q.Encode()

	authURL, err := gothic.GetAuthURL(w, r)
	if err != nil {
		h.logger.Error("failed to get auth URL from provider", "error", err)
		core.WriteError(
			w,
			http.StatusInternalServerError,
			"failed to initiate login",
		)
		return
	}

	// goth binds its auth-code options at provider construction and exposes no
	// generic setter, so include_granted_scopes has to be applied to the URL
	// it produced.
	//
	// This matters most once logins request only identity scopes: without it,
	// a returning user who already granted Calendar would complete a new,
	// narrower authorization and could be silently downgraded. With it, the
	// tokens Google issues carry the union of previously granted and newly
	// requested scopes.
	if descriptor, ok := h.auth.registry.Get(provider); ok {
		decorated, err := providers.DecorateAuthURL(descriptor, authURL)
		if err != nil {
			// Never fail a login over a decoration failure — the undecorated
			// URL is still a valid authorization request.
			h.logger.Error(
				"failed to decorate auth URL, continuing undecorated",
				slog.String("provider", provider),
				slog.Any("error", err),
			)
		} else {
			authURL = decorated
		}
	}

	http.Redirect(w, r, authURL, http.StatusFound)
}

// CallbackHandler godoc
//
// @Summary      OAuth2 provider callback
// @Description  Completes an OAuth2 login started by LoginHandler. Not called directly by clients — the provider redirects here (Apple posts via application/x-www-form-urlencoded). On success, redirects to redirect_uri with cookies set (web) or to the deep link with a one-time opaque code (mobile).
// @Tags         auth
// @Produce      json
// @Param        provider  path  string  true  "OAuth2 provider"  Enums(google, spotify, apple)
// @Success      302  "Redirects to redirect_uri (web) or the deep link (mobile)"
// @Failure      400  {object}  core.APIError  "Missing provider or malformed/missing state"
// @Failure      500  {object}  core.APIError  "OAuth flow, database, or token issuance failure"
// @Router       /auth/{provider}/callback [get]
// @Router       /auth/{provider}/callback [post]
func (h *AuthHandler) CallbackHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			h.logger.Error("failed to parse Apple callback form", "error", err)
			core.WriteError(w, http.StatusBadRequest, "invalid request")
			return
		}
	}

	provider, err := GetProviderName(r)
	if err != nil {
		h.logger.Warn("missing provider in callback", "error", err)
		core.WriteError(w, http.StatusBadRequest, "missing provider")
		return
	}

	stateData, err := decodeState(r)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	gothUser, err := gothic.CompleteUserAuth(w, r)
	if err != nil {
		h.logger.Error("OAuth flow failed", slog.Any("error", err))
		core.WriteError(w, http.StatusInternalServerError, "authentication failed")
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
		core.WriteError(w, http.StatusInternalServerError, "database error")
		return
	}

	var pair *tokens.TokenPair

	err = core.WithTransaction(r.Context(), conn, func(tx pgx.Tx) error {
		repo := repository.New(tx)
		deviceSvc := devicesvc.NewDeviceService(repo)
		tokenSvc := tokens.NewTokenService(repo, h.cacher, h.auth.config)

		account, err := h.upsertAccount(r, repo, gothUser)
		if err != nil {
			return err
		}

		if err := h.upsertSocialConnection(
			r,
			repo,
			gothUser,
			account,
			provider,
		); err != nil {
			return err
		}

		// Mirror the provider credentials into oauth_grants, which is what the
		// broker and the incremental flow read. Scopes are marked unverified:
		// goth discards the token response's scope field, so what we have here
		// is what we *asked* for, not what the provider confirmed. The first
		// broker call refreshes and learns the truth.
		if h.grants != nil {
			if err := h.grants(repo).RecordGrant(
				r.Context(),
				grantsvc.RecordGrantInput{
					AccountID:      account.ID,
					Provider:       provider,
					ExternalUserID: gothUser.UserID,
					AccessToken:    gothUser.AccessToken,
					RefreshToken:   gothUser.RefreshToken,
					ExpiresAt:      gothUser.ExpiresAt,
					GrantedScopes:  h.auth.registry.LoginScopesFor(provider),
					ScopesVerified: false,
				},
			); err != nil {
				// Non-fatal: failing a login because the grant mirror failed
				// would trade a working sign-in for a feature nobody has asked
				// for yet at this point in the request.
				h.logger.Error(
					"failed to record oauth grant at login",
					slog.String("provider", provider),
					slog.Any("error", err),
				)
			}
		}

		// Parse IP from request. net.SplitHostPort (not a naive strings.Split
		// on ":") is required here since IPv6 addresses contain multiple
		// colons themselves.
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return fmt.Errorf("split remote addr: %w", err)
		}
		ip, err := netip.ParseAddr(host)
		if err != nil {
			return fmt.Errorf("parse remote addr: %w", err)
		}

		input := devicesvc.DeviceRegistrationInput{
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
		core.WriteError(w, http.StatusInternalServerError, "authentication failed")
		return
	}

	if stateData.Platform == authPlatformMobileValue {
		h.handleMobileCallback(w, r, pair, stateData)
	} else {
		h.redirectWithTokens(w, r, pair, stateData)
	}
}

// RefreshTokenHandler godoc
//
// @Summary      Rotate a refresh token
// @Description  Consumes the given refresh token and issues a fresh access/refresh pair. Reusing an already-rotated, revoked, or expired refresh token revokes its entire token family and returns 401.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        request  body      auth.refreshTokenRequest  true  "Refresh token to rotate"
// @Success      200  {object}  auth.tokenResponse
// @Failure      400  {object}  core.APIError  "Missing or malformed refresh_token"
// @Failure      401  {object}  core.APIError  "Refresh token invalid, expired, or reuse detected"
// @Router       /auth/token/refresh [post]
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

// RevokeTokenHandler godoc
//
// @Summary      Revoke the caller's access token (and optionally a refresh token family)
// @Description  Blocklists the presented access token for its remaining lifetime. If a refresh_token is also supplied, revokes its entire token family too; refresh-family revocation failure is logged but non-fatal.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        request  body  auth.revokeTokenRequest  false  "Optional refresh token to also revoke its family"
// @Success      204  "No Content"
// @Failure      401  {object}  core.APIError  "Missing or invalid claims"
// @Failure      500  {object}  core.APIError  "Failed to revoke token"
// @Security     BearerToken
// @Router       /auth/token/revoke [post]
func (h *AuthHandler) RevokeTokenHandler(
	w http.ResponseWriter,
	r *http.Request,
) error {
	claims, ok := middleware.ClaimsFromContext(r.Context())
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
			if err := tokenSvc.RevokeAccessToken(
				r.Context(),
				jti,
				remaining,
			); err != nil {
				return fmt.Errorf("blocklist access token: %w", err)
			}
		}

		if req.RefreshToken != "" {
			if err := tokenSvc.RevokeByRawToken(
				r.Context(),
				req.RefreshToken,
			); err != nil {
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

// LogoutHandler godoc
//
// @Summary      Log out of the OAuth2 provider session
// @Description  Clears the goth/gothic OAuth2 session for the given provider. This is not the same as token revocation — call RevokeTokenHandler too if the client also wants to invalidate its JWT/refresh token.
// @Tags         auth
// @Produce      json
// @Param        provider  path  string  true  "OAuth2 provider"  Enums(google, spotify, apple)
// @Success      307  "Redirects to /"
// @Failure      400  {object}  core.APIError  "Missing provider"
// @Failure      500  {object}  core.APIError  "Failed to log out from provider"
// @Security     BearerToken
// @Router       /auth/{provider}/logout [get]
func (h *AuthHandler) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	provider, err := GetProviderName(r)
	if err != nil {
		h.logger.Warn("missing provider in logout request", "error", err)
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := gothic.Logout(w, r); err != nil {
		h.logger.Error(
			"failed to logout from provider",
			slog.String("provider", provider),
			slog.Any("error", err),
		)
		core.WriteError(w, http.StatusInternalServerError, "logout failed")
		return
	}

	h.logger.Info("user logged out", slog.String("provider", provider))
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

// ExchangeAuthCodeHandler godoc
//
// @Summary      Exchange a mobile auth code for a token pair
// @Description  Exchanges the one-time opaque code from the deep link (see CallbackHandler) for the access/refresh token pair. The code is single-use with a 60-second TTL and is deleted on first use.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        request  body      auth.authCodeExchangeRequest  true  "Opaque code from the deep link"
// @Success      200  {object}  auth.tokenResponse
// @Failure      400  {object}  core.APIError  "Missing or malformed code"
// @Failure      401  {object}  core.APIError  "Invalid or expired code"
// @Failure      500  {object}  core.APIError  "Failed to retrieve auth code"
// @Router       /auth/token/exchange [post]
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

	expiresAt := providerTokenExpiry(user.ExpiresAt)

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
			ExpiresAt:         expiresAt,
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
		ExpiresAt:         expiresAt,
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

// providerTokenExpiry converts a provider-reported access token expiry into
// the pgtype value the socials queries expect.
//
// Two traps live here. The first is that pgtype.Timestamp zero-values to
// Valid:false, which encodes as SQL NULL — the original code built the value
// without setting Valid, so socials.expires_at was silently NULL on every
// login since the column was introduced.
//
// The second is why this cannot simply set Valid:true unconditionally: goth
// reports a zero time for providers and flows that return no expiry, and
// UpdateSocial does `expires_at = COALESCE($3, expires_at)`. Writing a valid
// zero time would overwrite a previously-good expiry with year 1. Leaving it
// invalid keeps NULL flowing into COALESCE, which preserves the stored value.
//
// The column is TIMESTAMP without a zone, so the value is normalized to UTC
// rather than stored in whatever zone the process happens to run in.
func providerTokenExpiry(expiresAt time.Time) pgtype.Timestamp {
	if expiresAt.IsZero() {
		return pgtype.Timestamp{}
	}
	return pgtype.Timestamp{Time: expiresAt.UTC(), Valid: true}
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
		core.WriteError(w, http.StatusInternalServerError, "authentication failed")
		return
	}

	if err := h.storeAuthCode(r.Context(), code, pair); err != nil {
		h.logger.Error("failed to store auth code", slog.Any("error", err))
		core.WriteError(w, http.StatusInternalServerError, "authentication failed")
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
	if err := json.Unmarshal(
		[]byte(r.FormValue("user")),
		&appleData,
	); err != nil {
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
